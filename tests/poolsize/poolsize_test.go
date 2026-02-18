//go:build integration

package k8senv_poolsize_test

import (
	"context"
	"errors"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
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
		t.Fatalf("Failed to acquire first instance: %v", err)
	}
	defer func() {
		if relErr := inst1.Release(); relErr != nil {
			t.Logf("release error: %v", relErr)
		}
	}()

	inst2, err := sharedManager.Acquire(ctx)
	if err != nil {
		t.Fatalf("Failed to acquire second instance: %v", err)
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
		t.Errorf("Expected DeadlineExceeded, got %v", err)
	}

	t.Logf("Acquire correctly timed out on exhausted pool: %v", err)
}

// TestPoolReleaseUnblocks verifies that releasing an instance unblocks a
// waiting Acquire call on a bounded pool.
func TestPoolReleaseUnblocks(t *testing.T) {
	ctx := context.Background()

	// Acquire both instances.
	inst1, err := sharedManager.Acquire(ctx)
	if err != nil {
		t.Fatalf("Failed to acquire first instance: %v", err)
	}

	inst2, err := sharedManager.Acquire(ctx)
	if err != nil {
		t.Fatalf("Failed to acquire second instance: %v", err)
	}

	// Track whether inst2 was released on the happy path. The cleanup
	// guard ensures release on any failure path without risking a
	// double-release panic on the happy path.
	inst2Released := false
	t.Cleanup(func() {
		if !inst2Released {
			if relErr := inst2.Release(); relErr != nil {
				t.Logf("release error: %v", relErr)
			}
		}
	})

	// Release one instance in a goroutine. The readyCh gate ensures the
	// goroutine does not call Release before the test is ready, but there is
	// no strict ordering between close(readyCh) and the Acquire call below —
	// either may execute first. The test works because the 30-second timeout
	// is long enough for the release to happen regardless of scheduling order.
	readyCh := make(chan struct{})
	releaseCh := make(chan error, 1)
	go func() {
		<-readyCh
		releaseCh <- inst1.Release()
	}()

	// Ungate the goroutine and call the blocking Acquire.
	close(readyCh)

	acquireCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	inst3, err := sharedManager.Acquire(acquireCtx)
	if err != nil {
		t.Fatalf("Expected Acquire to succeed after release, got: %v", err)
	}
	defer func() {
		if relErr := inst3.Release(); relErr != nil {
			t.Logf("release error: %v", relErr)
		}
	}()

	// Check the goroutine's release result now that Acquire has returned
	// (the goroutine must have completed for Acquire to unblock).
	if relErr := <-releaseCh; relErr != nil {
		t.Logf("release error from goroutine: %v", relErr)
	}

	t.Log("Acquire unblocked after release")

	// Clean up inst2.
	if relErr := inst2.Release(); relErr != nil {
		t.Logf("release error: %v", relErr)
	}
	inst2Released = true
}

// TestPoolBoundedInstanceReuse verifies that a bounded pool reuses instances
// and never creates more than the configured maximum.
func TestPoolBoundedInstanceReuse(t *testing.T) {
	ctx := context.Background()

	seen := make(map[string]int)

	// Acquire and release 6 times sequentially on a pool of size 2.
	// At most 2 unique instance IDs should appear.
	for i := range 6 {
		inst, acqErr := sharedManager.Acquire(ctx)
		if acqErr != nil {
			t.Fatalf("Acquire %d failed: %v", i, acqErr)
		}

		// Track whether the instance was released on the happy path.
		// The cleanup guard ensures release on any failure path without
		// risking a double-release panic on the happy path.
		released := false
		t.Cleanup(func() {
			if !released {
				if relErr := inst.Release(); relErr != nil {
					t.Logf("release error: %v", relErr)
				}
			}
		})

		// Verify the instance works.
		cfg, cfgErr := inst.Config()
		if cfgErr != nil {
			t.Fatalf("Config %d failed: %v", i, cfgErr)
		}
		client, clientErr := kubernetes.NewForConfig(cfg)
		if clientErr != nil {
			t.Fatalf("NewForConfig %d failed: %v", i, clientErr)
		}
		if _, listErr := client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{}); listErr != nil {
			t.Fatalf("List namespaces %d failed: %v", i, listErr)
		}

		seen[inst.ID()]++
		if relErr := inst.Release(); relErr != nil {
			t.Logf("release error: %v", relErr)
		}
		released = true
	}

	if len(seen) > 2 {
		t.Errorf("Expected at most 2 unique instances, got %d: %v", len(seen), seen)
	}

	t.Logf("Instance usage across 6 sequential acquires: %v", seen)
}
