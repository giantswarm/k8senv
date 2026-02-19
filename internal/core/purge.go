package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	// Register the pure-Go SQLite driver (no CGO required).
	_ "modernc.org/sqlite"
)

// sqliteBusyTimeoutMs is the SQLite busy_timeout pragma value in milliseconds.
// It prevents "database is locked" errors when purge runs concurrently with
// kine's own SQLite operations while keeping test latency acceptable.
// 5 seconds is generous for a local SQLite file; in practice, lock waits
// resolve within a few milliseconds.
const sqliteBusyTimeoutMs = 5000

// purgeHandle holds a persistent SQLite connection and a prepared DELETE
// statement for ReleasePurge operations. It is created eagerly during instance
// startup (after system namespaces are verified) and kept open for the lifetime
// of the instance to amortize connection setup and query compilation across
// many release cycles.
//
// The baseline ID (captured via MAX(id) at startup) anchors the DELETE to only
// rows created after system bootstrap. deleteArgs holds [baselineID, exact
// namespace paths..., LIKE patterns...] — constant for the lifetime of the
// handle.
type purgeHandle struct {
	db         *sql.DB
	deleteStmt *sql.Stmt
	deleteArgs []any
}

// buildPurgeDeleteQuery constructs the DELETE statement that removes all kine
// rows created after the baseline ID while preserving system namespace data.
// The WHERE clause uses `id > ?` (primary key index, O(rows_to_delete)) plus
// exact-match and LIKE filters for the system namespaces.
//
// The filters are built dynamically from systemNamespaces so there is a single
// source of truth — adding a namespace to systemNamespaces automatically
// protects it from purge without requiring a manual SQL update.
//
// Returns the query string and a function that, given a baselineID, produces
// the full argument slice [baselineID, exact paths..., LIKE patterns...].
func buildPurgeDeleteQuery() (query string, makeArgs func(baselineID int64) []any) {
	sysNS := systemNamespaces[:]

	var b strings.Builder
	b.WriteString("DELETE FROM kine WHERE id > ?")

	// Exact matches: protect the namespace object itself.
	for range sysNS {
		b.WriteString(" AND name != ?")
	}

	// LIKE patterns: protect resources within system namespaces.
	// The pattern '%/<ns>/%' matches any key containing /<ns>/ as a path
	// segment, covering both core resources (/registry/configmaps/<ns>/foo)
	// and group resources (/registry/deployments/apps/<ns>/foo).
	for range sysNS {
		b.WriteString(" AND name NOT LIKE ?")
	}

	query = b.String()

	makeArgs = func(baselineID int64) []any {
		args := make([]any, 0, 1+len(sysNS)*2)
		args = append(args, baselineID)
		for _, ns := range sysNS {
			args = append(args, "/registry/namespaces/"+ns)
		}
		for _, ns := range sysNS {
			args = append(args, "%/"+ns+"/%")
		}
		return args
	}

	return query, makeArgs
}

// baselineQueryRetries is the number of attempts for the baseline ID query
// in openPurgeHandle. SQLITE_BUSY is transient — kine may hold a write lock
// briefly after system namespace creation (e.g., WAL checkpoint). Retrying
// with backoff avoids tearing down the entire stack for a lock that clears
// in milliseconds.
const baselineQueryRetries = 3

// baselineQueryBackoff is the initial backoff between baseline query retries.
// Each subsequent attempt doubles the backoff (100ms, 200ms, 400ms).
const baselineQueryBackoff = 100 * time.Millisecond

// openPurgeHandle opens a SQLite connection configured for purge operations,
// captures the baseline ID (MAX(id) at the point where only system data
// exists), and prepares the reusable DELETE statement. The connection uses WAL
// mode (matching kine), a generous busy timeout for concurrent access, and
// relaxed synchronous mode (OFF) since the database is ephemeral test data
// where crash durability is irrelevant.
//
// The busy_timeout pragma is ordered first so it is active before
// journal_mode(WAL), which may itself trigger SQLITE_BUSY if kine holds
// a write lock during connection setup.
func openPurgeHandle(sqlitePath string) (*purgeHandle, error) {
	dsn := fmt.Sprintf(
		"file:%s?_pragma=busy_timeout(%d)&_pragma=journal_mode(WAL)&_pragma=synchronous(OFF)",
		sqlitePath, sqliteBusyTimeoutMs,
	)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %s: %w", sqlitePath, err)
	}

	// Single connection — purge is serialized per-instance so a pool is
	// unnecessary. This also keeps exactly one WAL reader active, reducing
	// contention with kine's own connection.
	db.SetMaxOpenConns(1)

	// Capture the baseline ID: everything at or below this ID is system data
	// that must be preserved. COALESCE handles the (theoretical) empty-table
	// case by returning 0. Retry with backoff for transient SQLITE_BUSY
	// errors that can occur when kine is still settling after namespace
	// creation (e.g., WAL checkpoint in progress).
	var baselineID int64
	var queryErr error
	backoff := baselineQueryBackoff
	for attempt := range baselineQueryRetries {
		queryErr = db.QueryRow("SELECT COALESCE(MAX(id), 0) FROM kine").Scan(&baselineID)
		if queryErr == nil {
			break
		}
		if attempt < baselineQueryRetries-1 {
			time.Sleep(backoff)
			backoff *= 2
		}
	}
	if queryErr != nil {
		db.Close() //nolint:errcheck,gosec // best-effort cleanup on query failure
		return nil, fmt.Errorf("query baseline id: %w", queryErr)
	}

	query, makeArgs := buildPurgeDeleteQuery()
	deleteArgs := makeArgs(baselineID)

	deleteStmt, err := db.Prepare(query)
	if err != nil {
		db.Close() //nolint:errcheck,gosec // best-effort cleanup on prepare failure
		return nil, fmt.Errorf("prepare purge delete: %w", err)
	}

	return &purgeHandle{db: db, deleteStmt: deleteStmt, deleteArgs: deleteArgs}, nil
}

// Close releases the prepared statement and closes the database connection.
func (h *purgeHandle) Close() error {
	return errors.Join(h.deleteStmt.Close(), h.db.Close())
}

// purge deletes all rows created after the baseline ID from kine's database,
// preserving system namespace data. This bypasses the Kubernetes API entirely
// for maximum speed: a single SQL DELETE replaces ~20+ HTTP round trips through
// kube-apiserver → kine → SQLite.
//
// Safety: this is only called between Release and the next Acquire, when no
// API consumers or watchers are active. With --watch-cache=false, kube-apiserver
// reads go directly through kine to SQLite, so subsequent API calls see the
// cleaned state immediately.
func (h *purgeHandle) purge(ctx context.Context, log *slog.Logger) error {
	result, err := h.deleteStmt.ExecContext(ctx, h.deleteArgs...)
	if err != nil {
		return fmt.Errorf("execute purge delete: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("purge rows affected: %w", err)
	}

	if rowsAffected == 0 {
		log.Debug("purge: no rows to delete")
	} else {
		log.Debug("purge: deleted rows", "rows_affected", rowsAffected)
	}

	return nil
}
