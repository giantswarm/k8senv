//go:build integration

package k8senv_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/giantswarm/k8senv"
	"github.com/giantswarm/k8senv/tests/internal/testutil"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// TestBasicUsage shows a simple example of using k8senv.
func TestBasicUsage(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	inst, client := testutil.AcquireWithClient(ctx, t, sharedManager)
	defer func() {
		if err := inst.Release(); err != nil {
			t.Logf("release error: %v", err)
		}
	}()

	_, err := client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("failed to list namespaces: %v", err)
	}
}

// TestInstanceReuse explicitly tests that released instances can be acquired again.
// With a shared pool, the same instance may or may not be returned (other parallel
// tests may claim it first), so we verify the second acquire works, not that it
// returns the identical instance.
func TestInstanceReuse(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// First acquisition
	inst1, client := testutil.AcquireWithClient(ctx, t, sharedManager)

	_, err := client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("failed to list namespaces: %v", err)
	}

	// Release instance — behavior determined by manager's release strategy
	if err = inst1.Release(); err != nil {
		t.Errorf("release error: %v", err)
	}

	// Second acquisition — may get same or different instance from pool
	inst2, client2 := testutil.AcquireWithClient(ctx, t, sharedManager)
	defer func() {
		if err := inst2.Release(); err != nil {
			t.Logf("release error: %v", err)
		}
	}()

	// Verify acquired instance works
	_, err = client2.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("failed to list namespaces from second instance: %v", err)
	}
}

// TestIDUniqueness verifies that each acquired instance has a unique, non-empty ID.
func TestIDUniqueness(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// Acquire all instances
	ids := make(map[string]struct{})
	for i := range 3 {
		inst, err := sharedManager.Acquire(ctx)
		if err != nil {
			t.Fatalf("failed to acquire instance %d: %v", i, err)
		}
		t.Cleanup(func() {
			if err := inst.Release(); err != nil {
				t.Logf("release error: %v", err)
			}
		})

		id := inst.ID()

		// Check empty first — an empty ID would produce a confusing duplicate error.
		if id == "" {
			t.Error("Instance ID should not be empty")
		}
		if _, exists := ids[id]; exists {
			t.Errorf("duplicate ID: %s", id)
		}
		ids[id] = struct{}{}
	}
}

// TestDoubleReleaseReturnsError verifies that releasing an instance twice returns ErrDoubleRelease.
func TestDoubleReleaseReturnsError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	inst, err := sharedManager.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire failed: %v", err)
	}

	// First release should succeed
	if err = inst.Release(); err != nil {
		t.Fatalf("first release should not error: %v", err)
	}

	// Second release should return ErrDoubleRelease
	err = inst.Release()
	if err == nil {
		t.Fatal("Expected error on double-release but got nil")
	}
	if !errors.Is(err, k8senv.ErrDoubleRelease) {
		t.Errorf("expected ErrDoubleRelease, got %v", err)
	}
}

// TestAPIServerOnlyMode verifies that instances can run with scheduler and
// controller-manager disabled, and that API server operations still work.
func TestAPIServerOnlyMode(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	inst, client := testutil.AcquireWithClient(ctx, t, sharedManager)
	defer func() {
		if err := inst.Release(); err != nil {
			t.Logf("release error: %v", err)
		}
	}()

	nsName := testutil.UniqueName("test-api-only")

	// Create the namespace at the parent test level so all subtests can depend
	// on it. If this fails, t.Fatal stops the entire test and no subtest runs
	// with a missing namespace.
	ns := &v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: nsName,
		},
	}
	_, err := client.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("failed to create namespace: %v", err)
	}
	t.Cleanup(func() {
		// Use a fresh context with timeout because the test's ctx may be canceled
		// by the time cleanup runs, and we must not block indefinitely.
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = client.CoreV1().Namespaces().Delete(cleanupCtx, nsName, metav1.DeleteOptions{})
	})

	// Subtests are intentionally sequential (no t.Parallel) because they share
	// the namespace and resources created above. Running them in parallel would
	// introduce races on the shared ConfigMap and namespace state.
	t.Run("NamespaceOperations", func(t *testing.T) {
		nsList, err := client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
		if err != nil {
			t.Fatalf("failed to list namespaces: %v", err)
		}
		if !slices.ContainsFunc(nsList.Items, func(item v1.Namespace) bool {
			return item.Name == nsName
		}) {
			t.Fatalf("expected namespace %s in list, but not found", nsName)
		}
	})

	t.Run("ConfigMapOperations", func(t *testing.T) {
		cm := &v1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-config",
				Namespace: nsName,
			},
			Data: map[string]string{
				"key": "value",
			},
		}
		_, err := client.CoreV1().ConfigMaps(nsName).Create(ctx, cm, metav1.CreateOptions{})
		if err != nil {
			t.Fatalf("failed to create ConfigMap: %v", err)
		}
	})

	t.Run("PodRemainingPending", func(t *testing.T) {
		pod := &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-pod",
				Namespace: nsName,
			},
			Spec: v1.PodSpec{
				Containers: []v1.Container{
					{
						Name:  "test",
						Image: "nginx",
					},
				},
			},
		}
		_, err := client.CoreV1().Pods(nsName).Create(ctx, pod, metav1.CreateOptions{})
		if err != nil {
			t.Fatalf("failed to create Pod: %v", err)
		}

		retrievedPod, err := client.CoreV1().Pods(nsName).Get(ctx, "test-pod", metav1.GetOptions{})
		if err != nil {
			t.Fatalf("failed to get Pod: %v", err)
		}
		if retrievedPod.Status.Phase != v1.PodPending && retrievedPod.Status.Phase != "" {
			t.Errorf("expected pod phase Pending or empty without scheduler, got %s", retrievedPod.Status.Phase)
		}
	})
}

// TestMultipleInstancesWithAPIOnly verifies that multiple instances can run
// simultaneously when API-only mode is enabled (fixing port conflict issues).
func TestMultipleInstancesWithAPIOnly(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// Acquire first instance
	inst1, err := sharedManager.Acquire(ctx)
	if err != nil {
		t.Fatalf("failed to acquire first instance: %v", err)
	}
	defer func() {
		if err := inst1.Release(); err != nil {
			t.Logf("release error: %v", err)
		}
	}()

	// Acquire second instance - this would fail without API-only mode
	inst2, err := sharedManager.Acquire(ctx)
	if err != nil {
		t.Fatalf("failed to acquire second instance (port conflict?): %v", err)
	}
	defer func() {
		if err := inst2.Release(); err != nil {
			t.Logf("release error: %v", err)
		}
	}()

	// Verify both instances are different
	if inst1.ID() == inst2.ID() {
		t.Error("Expected different instances, got the same")
	}

	// Verify both instances work
	for i, inst := range []k8senv.Instance{inst1, inst2} {
		cfg, err := inst.Config()
		if err != nil {
			t.Fatalf("failed to get config from instance %d: %v", i+1, err)
		}

		client, err := kubernetes.NewForConfig(cfg)
		if err != nil {
			t.Fatalf("failed to create client for instance %d: %v", i+1, err)
		}

		// Test API operations
		_, err = client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
		if err != nil {
			t.Fatalf("failed to list namespaces from instance %d: %v", i+1, err)
		}
	}
}
