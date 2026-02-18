//go:build integration

package k8senv_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/giantswarm/k8senv/tests/internal/testutil"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// =============================================================================
// Pool Behavior Tests
// =============================================================================

func TestPoolAcquireRelease(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// Acquire an instance
	inst, err := sharedManager.Acquire(ctx)
	if err != nil {
		t.Fatalf("Failed to acquire instance: %v", err)
	}

	// Verify instance is usable
	cfg, err := inst.Config()
	if err != nil {
		t.Fatalf("Failed to get config: %v", err)
	}

	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	_, err = client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("Failed to list namespaces: %v", err)
	}

	// Release it back
	if err = inst.Release(); err != nil {
		t.Logf("release error: %v", err)
	}

	// Verify the instance can be re-acquired after release
	inst2, err := sharedManager.Acquire(ctx)
	if err != nil {
		t.Fatalf("Failed to re-acquire after release: %v", err)
	}
	if err = inst2.Release(); err != nil {
		t.Logf("release error: %v", err)
	}
}

func TestPoolConcurrentAccess(t *testing.T) {
	t.Parallel()

	// Concurrent acquire/release
	var wg sync.WaitGroup
	errCh := make(chan error, 10)

	for range 10 {
		wg.Go(func() {
			acquireCtx := context.Background()
			inst, err := sharedManager.Acquire(acquireCtx)
			if err != nil {
				errCh <- fmt.Errorf("failed to acquire: %w", err)
				return
			}
			if relErr := inst.Release(); relErr != nil {
				errCh <- fmt.Errorf("release failed: %w", relErr)
				return
			}
			errCh <- nil
		})
	}

	wg.Wait()
	close(errCh)

	// Check errors from the main test goroutine (safe)
	for err := range errCh {
		if err != nil {
			t.Error(err)
		}
	}
}

// TestParallelAcquisition proves multiple tests can run in parallel,
// acquiring and reusing instances from the pool.
func TestParallelAcquisition(t *testing.T) {
	t.Parallel()

	// Track which instances were used
	instanceUsage := make(map[string]int)
	var mu sync.Mutex

	// Register cleanup to verify instance reuse after all parallel tests complete
	// (t.Run subtests without t.Parallel() run before parallel subtests continue)
	t.Cleanup(func() {
		mu.Lock()
		defer mu.Unlock()

		t.Logf("Instance usage statistics:")
		totalUses := 0
		for id, count := range instanceUsage {
			t.Logf("  Instance %s: used %d times", id, count)
			totalUses += count
		}

		if len(instanceUsage) == 0 {
			t.Error("Expected at least one instance to be used")
		}

		if totalUses != 10 {
			t.Errorf("Expected 10 total acquisitions, got %d", totalUses)
		}
	})

	// Run 10 parallel tests
	for i := range 10 {
		t.Run(fmt.Sprintf("test-%d", i), func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()

			// Acquire instance
			inst, err := sharedManager.Acquire(ctx)
			if err != nil {
				t.Fatalf("Failed to acquire instance: %v", err)
			}
			defer func() {
				if err := inst.Release(); err != nil {
					t.Logf("release error: %v", err)
				}
			}() // Keep instance running for reuse

			// Track instance usage
			mu.Lock()
			instanceUsage[inst.ID()]++
			mu.Unlock()

			t.Logf("Test %d acquired instance %s", i, inst.ID())

			// Get config and create client
			cfg, err := inst.Config()
			if err != nil {
				t.Fatalf("Failed to get config: %v", err)
			}

			client, err := kubernetes.NewForConfig(cfg)
			if err != nil {
				t.Fatalf("Failed to create client: %v", err)
			}

			// Verify we can interact with the API
			namespaces, err := client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
			if err != nil {
				t.Fatalf("Failed to list namespaces: %v", err)
			}

			t.Logf("Test %d: Successfully listed %d namespaces", i, len(namespaces.Items))

			// Create a unique namespace for this test
			nsName := testutil.UniqueName(fmt.Sprintf("test-%d", i))
			ns := &v1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: nsName,
				},
			}
			_, err = client.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
			if err != nil {
				t.Fatalf("Failed to create namespace: %v", err)
			}

			t.Logf("Test %d: Created namespace %s", i, nsName)

			// Cleanup namespace
			err = client.CoreV1().Namespaces().Delete(ctx, nsName, metav1.DeleteOptions{})
			if err != nil {
				t.Logf("Warning: Failed to delete namespace %s: %v", nsName, err)
			}
		})
	}
}
