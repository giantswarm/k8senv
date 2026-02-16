package k8senv

import (
	"context"

	"k8s.io/client-go/rest"
)

// Manager coordinates a pool of kube-apiserver instances for testing.
//
// Callers must follow this lifecycle ordering:
//
//	NewManager → Initialize → Acquire/Release (repeatable) → Shutdown
//
// Initialize must be called before Acquire. Shutdown is safe to call at any
// point, including before Initialize. See each method's documentation for
// detailed error conditions.
type Manager interface {
	// Initialize performs expensive initialization operations.
	// Must be called before Acquire. Returns error instead of panicking.
	// Safe to call multiple times: after a successful initialization,
	// subsequent calls return nil immediately. If initialization fails,
	// subsequent calls retry instead of returning a cached error permanently.
	Initialize(ctx context.Context) error

	// Acquire gets an instance from the pool, creating one on demand if none
	// are free. Implements lazy start: the instance's processes are started
	// on first acquisition.
	//
	// When a pool size limit is configured (WithPoolSize), Acquire blocks if
	// all instances are in use and unblocks when one is released.
	//
	// The acquireTimeout (configured via WithAcquireTimeout) covers both the
	// time spent waiting for a free instance and instance startup time.
	// Instance startup typically takes 5-15 seconds.
	//
	// Returns ErrNotInitialized if Initialize has not been called.
	// Returns ErrShuttingDown if the manager is shutting down.
	Acquire(ctx context.Context) (Instance, error)

	// Shutdown stops all instances and cleans up.
	// Safe to call even if Initialize was never called.
	// Returns an error if any instance fails to stop.
	Shutdown() error
}

// Instance represents an acquired kube-apiserver + kine test environment.
// It exposes only the methods needed by test consumers. Lifecycle management
// (Start, Stop, state queries) is handled internally by the Manager and pool.
type Instance interface {
	// Config returns *rest.Config for connecting to this instance's kube-apiserver.
	// It must be called while the instance is acquired (between Acquire and Release).
	// Returns ErrInstanceReleased if called after Release.
	//
	// Callers must not call Config concurrently with Release on the same instance.
	// If Config and Release race on the same instance, the behavior is undefined:
	// Config may return a valid config, ErrInstanceReleased, or a stale config.
	Config() (*rest.Config, error)

	// Release returns the instance to the pool. If clean is true, the instance
	// is stopped first; otherwise it remains running for reuse by the next Acquire.
	//
	// Before returning the instance to the pool, Release deletes all non-system
	// namespaces to prevent test state leakage between consumers. This cleanup
	// is necessary because k8senv runs in API-only mode without a
	// kube-controller-manager to process namespace finalizers.
	//
	// Error semantics by mode:
	//   - Release(false) returns nil on success. Namespace cleanup runs before
	//     the instance is returned to the pool. If cleanup fails, the instance
	//     is marked as permanently failed and removed from the pool.
	//     Using defer inst.Release(false) is safe.
	//   - Release(true) returns an error if namespace cleanup or stopping fails.
	//     On error the instance is marked as permanently failed and removed from
	//     the pool, so the caller does not need to retry or release again. The
	//     error is informational: no corrective action is required because the
	//     instance is already removed from circulation.
	Release(clean bool) error

	// ID returns a unique identifier for this instance.
	ID() string
}
