//go:build integration

package k8senv_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/giantswarm/k8senv/tests/internal/testutil"
	"golang.org/x/sync/errgroup"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// =============================================================================
// Pool Behavior Tests
// =============================================================================

// TestPoolAcquireRelease tests that an instance can be acquired, used, released, and re-acquired.
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

// TestPoolConcurrentAccess verifies that concurrent acquire and release operations are safe under the race detector.
func TestPoolConcurrentAccess(t *testing.T) {
	t.Parallel()

	// Concurrent acquire/release
	var g errgroup.Group
	for range 10 {
		g.Go(func() error {
			inst, err := sharedManager.Acquire(context.Background())
			if err != nil {
				return fmt.Errorf("failed to acquire: %w", err)
			}
			// Release errors are safe to ignore; the instance is
			// removed from the pool but the test is not affected.
			_ = inst.Release()
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		t.Fatal(err)
	}
}

// TestParallelAcquisition proves multiple tests can run in parallel,
// acquiring and reusing instances from the pool.
func TestParallelAcquisition(t *testing.T) {
	t.Parallel()

	// Track which instances were used
	instanceUsage := make(map[string]int)
	var mu sync.Mutex

	// Register cleanup to verify instance reuse after all parallel tests complete.
	// Go guarantees parent t.Cleanup runs after all subtests (including parallel) finish.
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
			}()

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
			t.Cleanup(func() {
				// Use context.Background because the test's ctx may be canceled by the time cleanup runs.
				_ = client.CoreV1().Namespaces().Delete(context.Background(), nsName, metav1.DeleteOptions{})
			})

			t.Logf("Test %d: Created namespace %s", i, nsName)
		})
	}
}
