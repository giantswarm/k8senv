package core

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/giantswarm/k8senv/internal/netutil"
)

// errFromFactory is a sentinel used to make failFactory identifiable.
//
//nolint:gochecknoglobals // package-level test sentinel; mirrors the pattern used by ErrPoolClosed
var errFromFactory = errors.New("factory failure")

// noopFactory returns an InstanceFactory that produces valid unstarted instances
// without launching any processes. The factory is suitable for pool construction
// and acquire/release tests that do not exercise startup or cleanup paths.
func noopFactory(t *testing.T) InstanceFactory {
	t.Helper()
	return func(_ int) (*Instance, error) {
		ports := netutil.NewPortRegistry(nil)
		return NewInstance(NewInstanceParams{
			ID:       "noop-inst",
			DataDir:  t.TempDir(),
			Releaser: &fakeReleaser{},
			Ports:    ports,
			Config:   validInstanceConfig(),
		}), nil
	}
}

// failFactory returns an InstanceFactory that always returns errFromFactory.
func failFactory() InstanceFactory {
	return func(_ int) (*Instance, error) {
		return nil, errFromFactory
	}
}

// TestNewPoolPanics verifies that NewPool panics on invalid arguments.
func TestNewPoolPanics(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		factory InstanceFactory
		maxSize int
		wantMsg string
	}{
		"nil factory": {
			factory: nil,
			maxSize: 0,
			wantMsg: "k8senv: NewPool factory must not be nil",
		},
		"negative maxSize": {
			factory: func(_ int) (*Instance, error) {
				return nil, errors.New("never called")
			},
			maxSize: -1,
			wantMsg: "k8senv: NewPool maxSize must not be negative, got -1",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			requirePanicContains(t, func() {
				NewPool(tc.factory, tc.maxSize)
			}, tc.wantMsg)
		})
	}
}

// TestNewPoolUnboundedDoesNotPanic verifies that NewPool succeeds for the
// unbounded case (maxSize == 0).
func TestNewPoolUnboundedDoesNotPanic(t *testing.T) {
	t.Parallel()

	factory := func(_ int) (*Instance, error) {
		return nil, errors.New("never called")
	}
	pool := NewPool(factory, 0)
	if pool == nil {
		t.Fatal("NewPool returned nil for valid args")
	}
}

// TestPoolAcquireCanceledContext verifies that Acquire returns the context error
// immediately when the context is already canceled.
func TestPoolAcquireCanceledContext(t *testing.T) {
	t.Parallel()

	pool := NewPool(noopFactory(t), 0)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before calling Acquire

	_, _, err := pool.Acquire(ctx)
	if err == nil {
		t.Fatal("Acquire with canceled context should return error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Acquire error = %v, want wrapping context.Canceled", err)
	}
}

// TestPoolAcquireClosedPoolReturnsErrPoolClosed verifies that Acquire returns
// ErrPoolClosed immediately after Close has been called on an unbounded pool.
func TestPoolAcquireClosedPoolReturnsErrPoolClosed(t *testing.T) {
	t.Parallel()

	pool := NewPool(noopFactory(t), 0)
	pool.Close()

	_, _, err := pool.Acquire(context.Background())
	if !errors.Is(err, ErrPoolClosed) {
		t.Errorf("Acquire on closed pool error = %v, want ErrPoolClosed", err)
	}
}

// TestPoolAcquireClosedBoundedPoolReturnsErrPoolClosed verifies that a bounded
// pool that is closed before any acquisition returns ErrPoolClosed immediately.
func TestPoolAcquireClosedBoundedPoolReturnsErrPoolClosed(t *testing.T) {
	t.Parallel()

	// Bounded pool with size 1.
	pool := NewPool(noopFactory(t), 1)
	pool.Close()

	_, _, err := pool.Acquire(context.Background())
	if !errors.Is(err, ErrPoolClosed) {
		t.Errorf("Acquire on closed bounded pool error = %v, want ErrPoolClosed", err)
	}
}

// TestPoolAcquireBoundedBlocksAndUnblocksOnClose verifies that a bounded pool
// blocks Acquire when all instances are in use, and Close unblocks waiting callers.
func TestPoolAcquireBoundedBlocksAndUnblocksOnClose(t *testing.T) {
	t.Parallel()

	pool := NewPool(noopFactory(t), 1)

	// Acquire the one available slot.
	inst, _, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatalf("first Acquire failed: %v", err)
	}
	_ = inst // retain reference to keep slot occupied

	// Launch a goroutine that will block on the second Acquire.
	errCh := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _, acquireErr := pool.Acquire(ctx)
		errCh <- acquireErr
	}()

	// Close the pool while the goroutine is blocked. This should unblock it.
	pool.Close()

	// The blocking Acquire should now return ErrPoolClosed, proving that Close
	// unblocked the waiter (not merely a context timeout racing ahead).
	select {
	case err := <-errCh:
		if !errors.Is(err, ErrPoolClosed) {
			t.Errorf("blocked Acquire error = %v, want ErrPoolClosed", err)
		}
	case <-time.After(3 * time.Second):
		t.Error("blocked Acquire did not unblock within 3s after Close")
	}
}

// TestPoolAcquireFactoryFailure verifies that Acquire propagates factory errors.
func TestPoolAcquireFactoryFailure(t *testing.T) {
	t.Parallel()

	pool := NewPool(failFactory(), 0)

	_, _, err := pool.Acquire(context.Background())
	if err == nil {
		t.Fatal("Acquire with failing factory should return error, got nil")
	}
	if !errors.Is(err, errFromFactory) {
		t.Errorf("Acquire error = %v, want to wrap errFromFactory", err)
	}
}

// TestPoolReleasePanicsOnDoubleRelease verifies that Release panics when given
// a stale token (double-release scenario).
func TestPoolReleasePanicsOnDoubleRelease(t *testing.T) {
	t.Parallel()

	pool := NewPool(noopFactory(t), 0)

	inst, token, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}

	// First Release: must succeed.
	pool.Release(inst, token)

	// Second Release with the same token: must panic.
	requirePanicContains(t, func() {
		pool.Release(inst, token)
	}, "double-release")
}

// TestPoolCloseIsIdempotent verifies that calling Close multiple times does
// not panic.
func TestPoolCloseIsIdempotent(t *testing.T) {
	t.Parallel()

	pool := NewPool(noopFactory(t), 1)

	// Calling Close twice must not panic.
	pool.Close()
	pool.Close()
}

// TestPoolInstancesReturnsCreatedInstances verifies that Instances returns all
// instances created by the pool, even after they have been released.
func TestPoolInstancesReturnsCreatedInstances(t *testing.T) {
	t.Parallel()

	pool := NewPool(noopFactory(t), 0)

	if n := len(pool.Instances()); n != 0 {
		t.Fatalf("Instances() before any Acquire = %d, want 0", n)
	}

	inst, token, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}

	if n := len(pool.Instances()); n != 1 {
		t.Fatalf("Instances() after one Acquire = %d, want 1", n)
	}

	pool.Release(inst, token)

	// After release, instance should still be tracked in all.
	if n := len(pool.Instances()); n != 1 {
		t.Fatalf("Instances() after Release = %d, want 1 (still tracked in all)", n)
	}
}
