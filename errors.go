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

	// ErrInstanceReleased is returned by Instance.Config when called after Release.
	// After release, the instance may be re-acquired by another consumer, making
	// any previously obtained configuration stale.
	ErrInstanceReleased = core.ErrInstanceReleased

	// ErrDoubleRelease is returned by Instance.Release when called more than once
	// on the same acquisition. After the first Release returns the instance to the
	// pool, subsequent calls return this error instead of performing any action.
	ErrDoubleRelease = core.ErrDoubleRelease
)
