package k8senv

import "github.com/giantswarm/k8senv/internal/core"

// Sentinel errors for error inspection with errors.Is.
//
// These use the sentinel.Error const pattern instead of errors.New vars.
// sentinel.Error is a string type implementing error, allowing errors to be
// declared as const. This prevents accidental reassignment and enables
// compile-time immutability, while remaining compatible with errors.Is
// through Go's default == comparison on comparable types.
const (
	// ErrShuttingDown is returned by Acquire when the manager is shutting down.
	ErrShuttingDown = core.ErrShuttingDown

	// ErrNotInitialized is returned by Acquire when Initialize has not been called.
	ErrNotInitialized = core.ErrNotInitialized

	// ErrPoolClosed is returned when Acquire is called on a pool that has
	// been closed during shutdown.
	ErrPoolClosed = core.ErrPoolClosed

	// ErrInstanceReleased is returned by Instance.Config when called after Release.
	// After release, the instance may be re-acquired by another consumer, making
	// any previously obtained configuration stale.
	ErrInstanceReleased = core.ErrInstanceReleased

	// ErrNotStarted is returned by Instance.Config when the instance's processes
	// have not been launched yet.
	ErrNotStarted = core.ErrNotStarted

	// ErrDoubleRelease is returned by Instance.Release when called more than once
	// on the same acquisition. After the first Release returns the instance to the
	// pool, subsequent calls return this error instead of performing any action.
	ErrDoubleRelease = core.ErrDoubleRelease

	// ErrNoYAMLFiles is returned by Initialize when the CRD directory contains no YAML files.
	ErrNoYAMLFiles = core.ErrNoYAMLFiles

	// ErrMissingKind is returned by Initialize when a YAML document in the CRD
	// directory lacks a 'kind' field.
	ErrMissingKind = core.ErrMissingKind
)
