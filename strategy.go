package k8senv

import "github.com/giantswarm/k8senv/internal/core"

// ReleaseStrategy controls what happens when an Instance is released back to
// the pool. See the individual constant documentation for details on each
// strategy's behavior and trade-offs.
//
// ReleaseStrategy is a type alias (not a named type) so that the underlying
// [core.ReleaseStrategy] methods are part of the public API:
//
//   - IsValid reports whether the value is a recognized strategy.
//   - String returns the strategy name (implements [fmt.Stringer]).
//
// This is intentional: callers can validate and print strategy values without
// the public package needing to redeclare these methods.
//
// Audit: new methods added to core.ReleaseStrategy automatically become
// part of the public API through this alias.
type ReleaseStrategy = core.ReleaseStrategy

const (
	// ReleaseRestart stops the instance without performing any API-level
	// cleanup. The next Acquire starts a fresh instance — kine copies the
	// SQLite database from the cached template, restoring the database to
	// its pre-test state. This is the default strategy.
	ReleaseRestart = core.ReleaseRestart

	// ReleaseClean cleans all non-system namespaces but keeps the instance
	// running. Faster than ReleaseRestart (no stop/start cycle) but relies
	// on cleanup correctness for isolation.
	ReleaseClean = core.ReleaseClean

	// ReleaseNone performs no cleanup. The instance is returned to the pool
	// as-is. Use only when tests use unique namespaces and never share state.
	ReleaseNone = core.ReleaseNone

	// ReleasePurge cleans non-system data by directly deleting rows from
	// kine's SQLite database, bypassing the Kubernetes API entirely. Both
	// kine and kube-apiserver stay running; the next Acquire reuses the
	// same warm instance with zero startup delay. Fastest cleanup strategy.
	ReleasePurge = core.ReleasePurge
)
