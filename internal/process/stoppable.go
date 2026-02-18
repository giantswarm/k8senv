package process

import (
	"time"
)

// Stoppable represents a process that can be stopped and have its resources closed.
type Stoppable interface {
	Stop(timeout time.Duration) error
	Close()
}

// StopCloseAndNil stops, closes, and nils a Stoppable pointer in a single
// atomic cleanup step. It is safe to call with a nil p or when *p is nil;
// in both cases it returns nil immediately.
//
// StopCloseAndNil uses two type parameters to enforce a pointer-type constraint
// at compile time:
//
//   - P is constrained to both *E and Stoppable, meaning only pointer types
//     that implement Stoppable can be passed. This eliminates the need for
//     reflection-based nil checks because *E is always directly comparable to nil.
//   - E is the underlying element type (inferred by the compiler). Callers never
//     need to specify E explicitly; the compiler derives it from the pointer type.
//
// Close and nil-out always run even when Stop returns an error. This is intentional:
// a failed Stop means the process may be in an unknown state, so we must still close
// file handles (Close) and clear the pointer (nil-out) to prevent resource leaks and
// stale references. The Stop error is still returned to the caller.
//
// Usage:
//
//	var proc *kine.Process // implements Stoppable via pointer receiver
//	// ... start proc ...
//	err := process.StopCloseAndNil(&proc, 10*time.Second)
//
// After the call, proc is nil regardless of whether Stop succeeded.
func StopCloseAndNil[P interface {
	*E
	Stoppable
}, E any](p *P, timeout time.Duration) error {
	if p == nil || *p == nil {
		return nil
	}
	defer func() {
		(*p).Close()
		*p = nil
	}()
	return (*p).Stop(timeout)
}
