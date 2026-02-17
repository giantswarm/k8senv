package k8senv

import "github.com/giantswarm/k8senv/internal/core"

// ReleaseStrategy controls what happens when an Instance is released back to
// the pool. See the individual constant documentation for details on each
// strategy's behavior and trade-offs.
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
)
