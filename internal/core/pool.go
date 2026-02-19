package core

import (
	"context"
	"fmt"
	"sync"

	"github.com/giantswarm/k8senv/internal/sentinel"
)

// ErrPoolClosed is returned when Acquire is called on a closed pool
// (e.g., during shutdown).
const ErrPoolClosed = sentinel.Error("pool is closed")

// Pool manages a collection of Instance objects with on-demand creation and
// optional size bounding. When Acquire finds no free instance, it creates one
// via the factory — up to maxSize instances when bounded (maxSize > 0).
// When all instances in a bounded pool are in use, Acquire blocks until one
// is released or the context is canceled.
//
// It is safe for concurrent use by multiple goroutines.
type Pool struct {
	// mu protects free, all, nextIdx, and closed from concurrent access.
	mu sync.Mutex

	// free is a LIFO stack of instances available for acquisition.
	// Acquire pops from the end; Release pushes to the end.
	free []*Instance

	// all holds every Instance ever created by this Pool, including
	// instances that have failed or been stopped. Used by Shutdown
	// to ensure all instances are cleaned up.
	all []*Instance

	// nextIdx is a monotonically increasing index passed to the factory
	// for generating unique instance IDs.
	nextIdx int

	// closed is set by Close to prevent further acquisitions. Once set,
	// Acquire returns ErrPoolClosed and Release stops instances instead
	// of returning them to the free stack.
	closed bool

	// factory creates an Instance for the given pool index.
	factory InstanceFactory

	// maxSize is the maximum number of instances the pool will create.
	// A positive value caps the pool. 0 means unlimited (on-demand
	// creation without bound).
	maxSize int

	// sem is a buffered channel used as a counting semaphore to bound the
	// number of concurrently acquired instances. Pre-filled with maxSize
	// tokens; Acquire takes a token and Release/ReleaseFailed returns one.
	// nil when maxSize == 0 (unbounded pool).
	sem chan struct{}

	// closeCh is closed when the pool is closed, unblocking any Acquire
	// calls waiting on the semaphore. nil when maxSize == 0.
	closeCh chan struct{}

	// closeOnce ensures closeCh is closed exactly once.
	closeOnce sync.Once
}

// InstanceFactory creates an Instance for the given pool index. The factory
// encapsulates all instance construction details (ID generation, directory
// layout, releaser wiring, configuration), keeping Pool decoupled from
// instance creation concerns.
type InstanceFactory func(index int) (*Instance, error)

// NewPool creates a Pool that creates instances on demand using the given factory.
// maxSize bounds the pool: 0 means unlimited, >0 caps the number of instances.
// Panics if factory is nil or maxSize < 0.
func NewPool(factory InstanceFactory, maxSize int) *Pool {
	if factory == nil {
		panic("k8senv: NewPool factory must not be nil")
	}
	if maxSize < 0 {
		panic(fmt.Sprintf("k8senv: NewPool maxSize must not be negative, got %d", maxSize))
	}

	p := &Pool{
		factory: factory,
		maxSize: maxSize,
	}

	if maxSize > 0 {
		p.free = make([]*Instance, 0, maxSize)
		p.all = make([]*Instance, 0, maxSize)
		p.sem = make(chan struct{}, maxSize)
		for range maxSize {
			p.sem <- struct{}{}
		}
		p.closeCh = make(chan struct{})
	}

	return p
}

// Instances returns a copy of the slice of all instances ever created by this Pool.
func (p *Pool) Instances() []*Instance {
	p.mu.Lock()
	defer p.mu.Unlock()
	cp := make([]*Instance, len(p.all))
	copy(cp, p.all)
	return cp
}

// Acquire returns a free Instance or creates a new one on demand. Returns
// ErrPoolClosed if the pool has been closed (e.g., during shutdown).
//
// When the pool is bounded (maxSize > 0) and all instances are in use,
// Acquire blocks until an instance is released, the pool is closed, or
// the context is canceled.
//
// The context is checked before proceeding; if already canceled, the
// context error is returned immediately.
func (p *Pool) Acquire(ctx context.Context) (*Instance, uint64, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, fmt.Errorf("context done while waiting for instance: %w", err)
	}

	// If bounded, acquire a semaphore slot first. This blocks when all
	// maxSize instances are in use.
	if p.sem != nil {
		select {
		case <-p.sem:
			// Got a slot — proceed.
		case <-p.closeCh:
			return nil, 0, ErrPoolClosed
		case <-ctx.Done():
			return nil, 0, fmt.Errorf("context done while waiting for instance: %w", ctx.Err())
		}
	}

	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		p.returnSlot()
		return nil, 0, ErrPoolClosed
	}

	// LIFO: pop from end of free stack if available.
	if n := len(p.free); n > 0 {
		inst := p.free[n-1]
		p.free = p.free[:n-1]
		p.mu.Unlock()
		token := inst.markAcquired()
		return inst, token, nil
	}

	// No free instance — capture index and create outside the lock.
	// Note: nextIdx is incremented unconditionally before the factory call.
	// If the factory fails, this index is "consumed" and skipped, creating
	// a gap in the index sequence (e.g., 0, 1, 3 if index 2 failed). This
	// is harmless — indices are used only for unique ID generation, not as
	// array offsets — and avoids the complexity of rollback or reuse.
	idx := p.nextIdx
	p.nextIdx++
	p.mu.Unlock()

	inst, err := p.factory(idx)
	if err != nil {
		p.returnSlot()
		return nil, 0, fmt.Errorf("creating instance: %w", err)
	}

	// Re-lock to register the instance and recheck closed.
	p.mu.Lock()
	p.all = append(p.all, inst) // Always track — Stop is idempotent
	if p.closed {
		p.mu.Unlock()
		p.returnSlot()
		// Pool closed while we were creating the instance. Clean up.
		stopCtx, stopCancel := context.WithTimeout(context.Background(), inst.cfg.StopTimeout)
		defer stopCancel()
		if stopErr := inst.Stop( //nolint:contextcheck // cleanup must use background context; caller's context is unrelated
			stopCtx,
		); stopErr != nil {
			Logger().Warn("failed to stop instance created after pool close",
				"id", inst.ID(), "error", stopErr)
		}
		return nil, 0, ErrPoolClosed
	}
	p.mu.Unlock()

	token := inst.markAcquired()
	return inst, token, nil
}

// Release puts an Instance back into the free stack.
// The token must match the generation value returned by Acquire; if the token
// is stale (instance was re-acquired), Release panics (double-release).
//
// If the pool has been closed (e.g., during shutdown), the instance is stopped
// instead of being returned to the free stack.
func (p *Pool) Release(i *Instance, token uint64) {
	if !i.tryRelease(token) {
		panic("k8senv: double-release of instance " + i.ID())
	}

	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		stopCtx, stopCancel := context.WithTimeout(context.Background(), i.cfg.StopTimeout)
		defer stopCancel()
		if err := i.Stop(stopCtx); err != nil {
			Logger().Warn("failed to stop released instance after pool close",
				"id", i.ID(), "error", err)
		}
		p.returnSlot()
		return
	}
	p.free = append(p.free, i)
	p.mu.Unlock()

	p.returnSlot()
}

// ReleaseFailed marks an Instance as permanently failed. The instance is
// stopped but remains in the all slice for Shutdown cleanup.
// The token must match the generation value returned by Acquire; if the token
// is stale (instance was re-acquired), ReleaseFailed panics (double-release).
func (p *Pool) ReleaseFailed(i *Instance, token uint64) {
	if !i.tryRelease(token) {
		panic("k8senv: double-release of instance " + i.ID())
	}

	// Attempt cleanup of partially started processes.
	// Stop is idempotent: it atomically nils i.stack and i.cancel on entry,
	// so a second call returns nil immediately.
	// Called without holding the lock because it performs I/O.
	ctx, cancel := context.WithTimeout(context.Background(), i.cfg.StopTimeout)
	defer cancel()
	if err := i.Stop(ctx); err != nil {
		Logger().Warn("failed to stop instance during cleanup", "id", i.ID(), "error", err)
	}

	p.returnSlot()
}

// Close marks the pool as closed. Subsequent Acquire calls return ErrPoolClosed
// and Release calls stop instances instead of returning them to the free stack.
// Safe to call multiple times (idempotent).
func (p *Pool) Close() {
	p.mu.Lock()
	p.closed = true
	p.free = nil
	p.mu.Unlock()

	// Close closeCh to unblock any Acquire calls waiting on the semaphore.
	if p.closeCh != nil {
		p.closeOnce.Do(func() { close(p.closeCh) })
	}
}

// returnSlot returns a semaphore slot, unblocking a waiting Acquire call.
// No-op when the pool is unbounded (sem is nil).
//
// Uses a non-blocking send: after Close(), the semaphore channel may already
// be at capacity (no Acquire will drain it), so a blocking send would hang.
func (p *Pool) returnSlot() {
	if p.sem == nil {
		return
	}
	select {
	case p.sem <- struct{}{}:
	default:
		// Semaphore is full. After Close() this is expected because no
		// Acquire will drain tokens. During normal operation it indicates
		// a bug: more releases than acquires.
		select {
		case <-p.closeCh:
			Logger().Debug("returnSlot: semaphore full after pool close, token dropped (expected)")
		default:
			panic(fmt.Sprintf("k8senv: returnSlot: semaphore full during normal operation (maxSize=%d)", p.maxSize))
		}
	}
}
