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

// purgeHandle holds a persistent SQLite connection and a prepared statement for
// ReleasePurge operations. It is created lazily on first purge and kept open
// for the lifetime of the instance to amortize connection setup and query
// compilation across many release cycles.
//
// findArgs holds the fixed bind parameters for findStmt (the system namespace
// paths used in the NOT IN clause). They are constant for the lifetime of the
// handle and stored here so callers do not need to reconstruct them on every
// invocation.
type purgeHandle struct {
	db       *sql.DB
	findStmt *sql.Stmt
	findArgs []any
}

// buildFindUserNamespacesQuery constructs the SQL query that discovers
// non-deleted, non-system namespace names. The NOT IN clause is built
// dynamically from the systemNamespaces slice so there is a single source of
// truth — adding a namespace to systemNamespaces automatically excludes it
// from purge without requiring a manual SQL update.
//
// Kine is append-only: the row with the highest id for a given name is the
// current state. We filter system namespaces server-side to avoid client-side
// allocation per row. The query returns just the namespace name (suffix after
// the /registry/namespaces/ prefix, which is 21 characters).
//
// The second return value holds the query arguments (one per system namespace).
func buildFindUserNamespacesQuery() (query string, args []any) {
	placeholders := make([]string, len(systemNamespaces))
	args = make([]any, len(systemNamespaces))
	for i, ns := range systemNamespaces {
		placeholders[i] = "?"
		args[i] = "/registry/namespaces/" + ns
	}
	query = fmt.Sprintf(`
	SELECT SUBSTR(k.name, 22) FROM kine k
	INNER JOIN (
		SELECT name, MAX(id) AS max_id FROM kine
		WHERE name LIKE '/registry/namespaces/%%'
		AND name NOT LIKE '/registry/namespaces/%%/%%'
		GROUP BY name
	) latest ON k.name = latest.name AND k.id = latest.max_id
	WHERE k.deleted = 0
	AND k.name NOT IN (%s)
`, strings.Join(placeholders, ", "))
	return query, args
}

// openPurgeHandle opens a SQLite connection configured for purge operations and
// prepares the reusable find query. The connection uses WAL mode (matching kine),
// a generous busy timeout for concurrent access, and relaxed synchronous mode
// (NORMAL) since the database is ephemeral test data where crash durability is
// irrelevant.
func openPurgeHandle(sqlitePath string) (*purgeHandle, error) {
	dsn := fmt.Sprintf(
		"file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(30000)&_pragma=synchronous(NORMAL)",
		sqlitePath,
	)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %s: %w", sqlitePath, err)
	}

	// Single connection — purge is serialized per-instance so a pool is
	// unnecessary. This also keeps exactly one WAL reader active, reducing
	// contention with kine's own connection.
	db.SetMaxOpenConns(1)

	query, findArgs := buildFindUserNamespacesQuery()
	findStmt, err := db.Prepare(query)
	if err != nil {
		db.Close() //nolint:errcheck,gosec // best-effort cleanup on prepare failure
		return nil, fmt.Errorf("prepare find-namespaces: %w", err)
	}

	return &purgeHandle{db: db, findStmt: findStmt, findArgs: findArgs}, nil
}

// Close releases the prepared statement and closes the database connection.
func (h *purgeHandle) Close() error {
	h.findStmt.Close() //nolint:errcheck,gosec // best-effort cleanup
	return h.db.Close()
}

// purge deletes all rows associated with non-system namespaces from kine's
// database. This bypasses the Kubernetes API entirely for maximum speed: a few
// SQL DELETEs replace ~20+ HTTP round trips through kube-apiserver → kine → SQLite.
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
func (h *purgeHandle) purge(ctx context.Context, log *slog.Logger) error {
	// Discover user namespaces using the prepared statement.
	userNamespaces, err := h.findUserNamespaces(ctx)
	if err != nil {
		return err
	}

	if len(userNamespaces) == 0 {
		log.Debug("purge: no user namespaces found, skipping")
		return nil
	}

	log.Debug("purge: deleting user namespace data", "namespaces", len(userNamespaces))

	if err := h.deleteNamespaceData(ctx, userNamespaces); err != nil {
		return err
	}

	log.Debug("purge: cleanup complete", "namespaces_purged", len(userNamespaces))
	return nil
}

// findUserNamespaces returns the names of non-system namespaces present in the
// kine database using the prepared query. The system namespace paths are bound
// via h.findArgs, which were computed once at handle creation time.
func (h *purgeHandle) findUserNamespaces(ctx context.Context) ([]string, error) {
	rows, err := h.findStmt.QueryContext(ctx, h.findArgs...)
	if err != nil {
		return nil, fmt.Errorf("query user namespaces: %w", err)
	}
	defer rows.Close() //nolint:errcheck // rows.Err() below catches read errors; Close error is redundant

	var namespaces []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("scan namespace row: %w", err)
		}
		if name != "" {
			namespaces = append(namespaces, name)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate namespace rows: %w", err)
	}

	return namespaces, nil
}

// escapeLIKE escapes SQL LIKE wildcard characters (%, _) and the escape
// character itself (\) in s so that s is matched literally in a LIKE pattern
// used with ESCAPE '\'.
func escapeLIKE(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
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
func (h *purgeHandle) deleteNamespaceData(ctx context.Context, namespaces []string) error {
	tx, err := h.db.BeginTx(ctx, nil)
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
		b.WriteString("name = ? OR name LIKE ? ESCAPE '\\'")
		args = append(args, "/registry/namespaces/"+ns, "%/"+escapeLIKE(ns)+"/%")
	}

	if _, err := tx.ExecContext(ctx, b.String(), args...); err != nil {
		return fmt.Errorf("delete namespace data: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit purge transaction: %w", err)
	}
	return nil
}
