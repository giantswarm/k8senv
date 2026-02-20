//go:build integration

package k8senv_poolsize_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/giantswarm/k8senv"
	"github.com/giantswarm/k8senv/tests/internal/testutil"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Pool-size tests are NOT marked as t.Parallel() because they share a bounded
// pool of 2 instances and need exclusive access to exercise exhaustion/contention.

// TestPoolTimeout verifies that Acquire blocks when the bounded pool is
// exhausted and returns a deadline error when the context times out.
func TestPoolTimeout(t *testing.T) {
	ctx := context.Background()

	// Acquire both instances (pool size = 2).
	inst1, err := sharedManager.Acquire(ctx)
	if err != nil {
		t.Fatalf("failed to acquire first instance: %v", err)
	}
	defer func() {
		if relErr := inst1.Release(); relErr != nil {
			t.Logf("release error: %v", relErr)
		}
	}()

	inst2, err := sharedManager.Acquire(ctx)
	if err != nil {
		t.Fatalf("failed to acquire second instance: %v", err)
	}
	defer func() {
		if relErr := inst2.Release(); relErr != nil {
			t.Logf("release error: %v", relErr)
		}
	}()

	// Pool is now exhausted. A third Acquire with a short timeout should fail.
	shortCtx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()

	_, err = sharedManager.Acquire(shortCtx)
	if err == nil {
		t.Fatal("Expected timeout error when pool exhausted")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected DeadlineExceeded, got %v", err)
	}
}

// TestPoolReleaseUnblocks verifies that releasing an instance unblocks a
// waiting Acquire call on a bounded pool.
func TestPoolReleaseUnblocks(t *testing.T) {
	ctx := context.Background()

	// Acquire both instances to exhaust the pool (size = 2).
	inst1, err := sharedManager.Acquire(ctx)
	if err != nil {
		t.Fatalf("failed to acquire first instance: %v", err)
	}

	inst2, err := sharedManager.Acquire(ctx)
	if err != nil {
		t.Fatalf("failed to acquire second instance: %v", err)
	}

	// Guard inst2 release with sync.Once so the t.Cleanup safety net and
	// the explicit release below are both safe to call without risking a
	// double-release panic.
	var inst2Once sync.Once
	releaseInst2 := func() {
		inst2Once.Do(func() {
			if relErr := inst2.Release(); relErr != nil {
				t.Logf("release error: %v", relErr)
			}
		})
	}
	t.Cleanup(releaseInst2)

	acquireCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// The goroutine attempts Acquire on the exhausted pool. It signals on
	// readyCh just before calling Acquire so the main goroutine knows the
	// goroutine is about to call Acquire. The main goroutine waits for that
	// signal before calling Release. This reduces (but does not eliminate) the
	// scheduling race: the goroutine may not yet be blocked in the pool's wait
	// queue when Release fires. The test still passes because the released slot
	// remains available for the subsequent Acquire call.
	readyCh := make(chan struct{})
	acquireCh := make(chan error, 1)
	var inst3 k8senv.Instance

	go func() {
		close(readyCh) // signal: about to call Acquire
		acquired, acquireErr := sharedManager.Acquire(acquireCtx)
		if acquireErr == nil {
			inst3 = acquired
		}
		acquireCh <- acquireErr
	}()

	// Wait until the goroutine has signaled it is about to block on Acquire,
	// then release inst1 to free a pool slot.
	<-readyCh
	if relErr := inst1.Release(); relErr != nil {
		t.Logf("release error: %v", relErr)
	}

	// Wait for the goroutine's Acquire to complete.
	if acquireErr := <-acquireCh; acquireErr != nil {
		t.Fatalf("expected Acquire to succeed after release, got: %v", acquireErr)
	}

	defer func() {
		if inst3 != nil {
			if relErr := inst3.Release(); relErr != nil {
				t.Logf("release error: %v", relErr)
			}
		}
	}()

	// Clean up inst2.
	releaseInst2()
}

// TestPoolBoundedInstanceReuse verifies that a bounded pool reuses instances
// and never creates more than the configured maximum.
func TestPoolBoundedInstanceReuse(t *testing.T) {
	ctx := context.Background()

	seen := make(map[string]int)

	// Acquire and release 6 times sequentially on a pool of size 2.
	// At most 2 unique instance IDs should appear.
	for i := range 6 {
		inst, client, release := testutil.AcquireWithGuardedRelease(ctx, t, sharedManager)

		// Verify the instance works.
		if _, listErr := client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{}); listErr != nil {
			t.Fatalf("list namespaces %d failed: %v", i, listErr)
		}

		seen[inst.ID()]++
		release()
	}

	if len(seen) > 2 {
		t.Errorf("expected at most 2 unique instances, got %d: %v", len(seen), seen)
	}
}
