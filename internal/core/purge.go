package core

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"

	// Register the pure-Go SQLite driver (no CGO required).
	_ "modernc.org/sqlite"
)

// purgeViaSQL opens a direct SQLite connection to kine's database and deletes
// all rows associated with non-system namespaces. This bypasses the Kubernetes
// API entirely for maximum speed: a few SQL DELETEs replace ~20+ HTTP round
// trips through kube-apiserver → kine → SQLite.
//
// Safety: this is only called between Release and the next Acquire, when no
// API consumers or watchers are active. With --watch-cache=false, kube-apiserver
// reads go directly through kine to SQLite, so subsequent API calls see the
// cleaned state immediately.
//
// The function preserves:
//   - System namespaces (default, kube-system, kube-public, kube-node-lease)
//   - Resources within system namespaces
//   - Cluster-scoped resources (CRDs, APIServices, ClusterRoles, etc.)
//   - Internal kine bookkeeping keys (compact_rev_key, gap-*)
func purgeViaSQL(ctx context.Context, sqlitePath string, log *slog.Logger) error {
	// Open with WAL mode (matching kine's own mode), a generous busy
	// timeout to handle concurrent access from kine's connection, and
	// relaxed synchronous mode. NORMAL is safe here because the database
	// is ephemeral test data — crash durability is irrelevant — and it
	// reduces fsync calls during transaction commit.
	dsn := fmt.Sprintf(
		"file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(30000)&_pragma=synchronous(NORMAL)",
		sqlitePath,
	)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return fmt.Errorf("open sqlite %s: %w", sqlitePath, err)
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil {
			log.Warn("purge: close sqlite", "error", closeErr)
		}
	}()

	// Single connection — we want a short-lived session, not a pool.
	db.SetMaxOpenConns(1)

	// Discover user namespaces by querying the kine table directly.
	// Kine stores namespace objects at key path "/registry/namespaces/<name>".
	// We look at the latest (highest id) row per name and check deleted=0.
	userNamespaces, err := findUserNamespaces(ctx, db)
	if err != nil {
		return err
	}

	if len(userNamespaces) == 0 {
		log.Debug("purge: no user namespaces found, skipping")
		return nil
	}

	log.Debug("purge: deleting user namespace data", "namespaces", len(userNamespaces))

	if err := deleteNamespaceData(ctx, db, userNamespaces); err != nil {
		return err
	}

	log.Debug("purge: cleanup complete", "namespaces_purged", len(userNamespaces))
	return nil
}

// findUserNamespaces returns the names of non-system namespaces present in the
// kine database. It queries for the current (non-deleted, highest revision)
// namespace objects and filters out system namespaces using isSystemNamespace.
func findUserNamespaces(ctx context.Context, db *sql.DB) ([]string, error) {
	// Find current (non-deleted) namespace objects. Kine's storage is
	// append-only: the row with the highest id for a given name is the
	// current state. We use a subquery to find the max id per name, then
	// filter for non-deleted entries. System namespaces are filtered
	// client-side via isSystemNamespace to keep the SQL simple and avoid
	// duplicating the system namespace list.
	const query = `
		SELECT k.name FROM kine k
		INNER JOIN (
			SELECT name, MAX(id) AS max_id FROM kine
			WHERE name LIKE '/registry/namespaces/%'
			AND name NOT LIKE '/registry/namespaces/%/%'
			GROUP BY name
		) latest ON k.name = latest.name AND k.id = latest.max_id
		WHERE k.deleted = 0
	`

	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query user namespaces: %w", err)
	}
	defer rows.Close() //nolint:errcheck // rows.Err() below catches read errors; Close error is redundant

	const nsPrefix = "/registry/namespaces/"
	var namespaces []string
	for rows.Next() {
		var keyPath string
		if err := rows.Scan(&keyPath); err != nil {
			return nil, fmt.Errorf("scan namespace row: %w", err)
		}
		name := strings.TrimPrefix(keyPath, nsPrefix)
		if name != "" && !isSystemNamespace(name) {
			namespaces = append(namespaces, name)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate namespace rows: %w", err)
	}

	return namespaces, nil
}

// deleteNamespaceData removes all kine rows associated with the given
// namespaces in a single statement. For each namespace it deletes:
//   - The namespace object itself: name = '/registry/namespaces/<ns>'
//   - All resources in the namespace: name LIKE '%/<ns>/%'
//
// All namespace patterns are combined into one DELETE with OR clauses so
// that SQLite scans the table once instead of once per namespace. The
// leading-wildcard LIKE patterns prevent index usage, making this O(rows)
// rather than O(N * rows) where N is the namespace count.
//
// All historical revisions and deletion markers are removed, not just the
// current state. This is safe because the instance is idle (no watchers or
// API consumers) and kine will correctly handle the gaps in revision history.
func deleteNamespaceData(ctx context.Context, db *sql.DB, namespaces []string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin purge transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is a no-op

	// Build a single DELETE: ... WHERE (name = ? OR name LIKE ?) OR (name = ? OR name LIKE ?) ...
	// Two parameters per namespace: exact match for the namespace object,
	// LIKE pattern for all resources in the namespace.
	var b strings.Builder
	b.WriteString("DELETE FROM kine WHERE ")
	args := make([]any, 0, len(namespaces)*2)

	for idx, ns := range namespaces {
		if idx > 0 {
			b.WriteString(" OR ")
		}
		// Pattern matches any key with /<ns>/ as a path segment, catching
		// both core resources (/registry/configmaps/<ns>/foo) and group
		// resources (/registry/deployments/apps/<ns>/foo).
		b.WriteString("name = ? OR name LIKE ?")
		args = append(args, "/registry/namespaces/"+ns, "%/"+ns+"/%")
	}

	if _, err := tx.ExecContext(ctx, b.String(), args...); err != nil {
		return fmt.Errorf("delete namespace data: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit purge transaction: %w", err)
	}
	return nil
}
