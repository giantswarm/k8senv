//go:build integration

package k8senv_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/giantswarm/k8senv"
	"github.com/giantswarm/k8senv/tests/internal/testutil"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// =============================================================================
// Instance Behavior Tests
// =============================================================================

// TestBasicUsage shows a simple example of using k8senv.
func TestBasicUsage(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	startTime := time.Now()

	inst, client := testutil.AcquireWithClient(ctx, t, sharedManager)
	defer func() {
		if err := inst.Release(); err != nil {
			t.Logf("release error: %v", err)
		}
	}()

	namespaces, err := client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("Failed to list namespaces: %v", err)
	}

	t.Logf(
		"kube-apiserver instance running with %d namespaces (total test time: %v)",
		len(namespaces.Items),
		time.Since(startTime),
	)
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
		t.Fatalf("Failed to list namespaces: %v", err)
	}

	// Release instance — behavior determined by manager's release strategy
	if err = inst1.Release(); err != nil {
		t.Logf("release error: %v", err)
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
		t.Fatalf("Failed to list namespaces from second instance: %v", err)
	}

	t.Logf("Successfully acquired instances: first=%s, second=%s", inst1.ID(), inst2.ID())
}

func TestIDUniqueness(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// Acquire all instances
	ids := make(map[string]struct{})
	for i := range 3 {
		inst, err := sharedManager.Acquire(ctx)
		if err != nil {
			t.Fatalf("Failed to acquire instance %d: %v", i, err)
		}
		t.Cleanup(func() {
			if err := inst.Release(); err != nil {
				t.Logf("release error: %v", err)
			}
		})

		id := inst.ID()
		if _, exists := ids[id]; exists {
			t.Errorf("Duplicate ID: %s", id)
		}
		ids[id] = struct{}{}

		// IDs should be non-empty and unique (format: inst-N-XXXXXXXX)
		if id == "" {
			t.Error("Instance ID should not be empty")
		}
	}
}

func TestDoubleReleasePanics(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	inst, err := sharedManager.Acquire(ctx)
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}

	// First release should succeed
	if err = inst.Release(); err != nil {
		t.Fatalf("First release should not error: %v", err)
	}

	// Second release should panic
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("Expected panic on double-release but didn't get one")
		}
		msg, ok := r.(string)
		if !ok {
			t.Fatalf("Expected string panic, got %T: %v", r, r)
		}
		expectedPrefix := "k8senv: double-release of instance "
		if !strings.HasPrefix(msg, expectedPrefix) {
			t.Errorf("Panic message should start with %q, got %q", expectedPrefix, msg)
		}
	}()

	_ = inst.Release() // error return unreachable due to panic
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

	t.Run("NamespaceOperations", func(t *testing.T) {
		ns := &v1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: nsName,
			},
		}
		_, err := client.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
		if err != nil {
			t.Fatalf("Failed to create namespace: %v", err)
		}

		nsList, err := client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
		if err != nil {
			t.Fatalf("Failed to list namespaces: %v", err)
		}
		t.Logf("Listed %d namespaces", len(nsList.Items))
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
			t.Fatalf("Failed to create ConfigMap: %v", err)
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
			t.Fatalf("Failed to create Pod: %v", err)
		}

		retrievedPod, err := client.CoreV1().Pods(nsName).Get(ctx, "test-pod", metav1.GetOptions{})
		if err != nil {
			t.Fatalf("Failed to get Pod: %v", err)
		}
		if retrievedPod.Status.Phase != v1.PodPending && retrievedPod.Status.Phase != "" {
			t.Errorf("Expected pod phase Pending or empty without scheduler, got %s", retrievedPod.Status.Phase)
		}
	})

	// Cleanup
	if err := client.CoreV1().Namespaces().Delete(ctx, nsName, metav1.DeleteOptions{}); err != nil {
		t.Logf("Warning: Failed to delete namespace %s: %v", nsName, err)
	}
}

// TestMultipleInstancesWithAPIOnly verifies that multiple instances can run
// simultaneously when API-only mode is enabled (fixing port conflict issues).
func TestMultipleInstancesWithAPIOnly(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// Acquire first instance
	inst1, err := sharedManager.Acquire(ctx)
	if err != nil {
		t.Fatalf("Failed to acquire first instance: %v", err)
	}
	defer func() {
		if err := inst1.Release(); err != nil {
			t.Logf("release error: %v", err)
		}
	}()
	t.Logf("Acquired first instance: %s", inst1.ID())

	// Acquire second instance - this would fail without API-only mode
	inst2, err := sharedManager.Acquire(ctx)
	if err != nil {
		t.Fatalf("Failed to acquire second instance (port conflict?): %v", err)
	}
	defer func() {
		if err := inst2.Release(); err != nil {
			t.Logf("release error: %v", err)
		}
	}()
	t.Logf("Acquired second instance: %s", inst2.ID())

	// Verify both instances are different
	if inst1.ID() == inst2.ID() {
		t.Error("Expected different instances, got the same")
	}

	// Verify both instances work
	for i, inst := range []k8senv.Instance{inst1, inst2} {
		cfg, err := inst.Config()
		if err != nil {
			t.Fatalf("Failed to get config from instance %d: %v", i+1, err)
		}

		client, err := kubernetes.NewForConfig(cfg)
		if err != nil {
			t.Fatalf("Failed to create client for instance %d: %v", i+1, err)
		}

		// Test API operations
		nsList, err := client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
		if err != nil {
			t.Fatalf("Failed to list namespaces from instance %d: %v", i+1, err)
		}
		t.Logf("Instance %d: Listed %d namespaces successfully", i+1, len(nsList.Items))
	}
}
