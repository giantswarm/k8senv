//go:build integration

package k8senv_test

import (
	"context"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// systemNamespaces is the set of namespaces created by kube-apiserver that
// must survive cleanup.
//
// KEEP IN SYNC with internal/core/cleanup.go:systemNamespaces.
// TestSystemNamespacesMatchAPIServer verifies this set at runtime against the
// namespaces that kube-apiserver actually creates on startup.
var systemNamespaces = map[string]struct{}{
	"default":         {},
	"kube-system":     {},
	"kube-public":     {},
	"kube-node-lease": {},
}

// TestSystemNamespacesMatchAPIServer verifies that the local systemNamespaces
// set matches exactly the namespaces that kube-apiserver creates on startup.
// This catches drift between the test-local copy and the authoritative set in
// internal/core/cleanup.go (which the test package cannot import by design).
func TestSystemNamespacesMatchAPIServer(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// Acquire a fresh instance and list namespaces before creating any user
	// resources. The only namespaces present should be the ones kube-apiserver
	// creates automatically on startup.
	inst, client := acquireWithClient(ctx, t, sharedManager)
	defer func() {
		if err := inst.Release(false); err != nil {
			t.Logf("release error: %v", err)
		}
	}()

	nsList, err := client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list namespaces: %v", err)
	}

	// Build a set of namespace names the API server actually created.
	actual := make(map[string]struct{}, len(nsList.Items))
	for _, ns := range nsList.Items {
		actual[ns.Name] = struct{}{}
	}

	// Verify every expected system namespace exists on the API server.
	for name := range systemNamespaces {
		if _, ok := actual[name]; !ok {
			t.Errorf("expected system namespace %q not found on API server", name)
		}
	}

	// Verify the API server has no extra namespaces beyond the expected set.
	// (If kube-apiserver adds a new default namespace in a future version,
	// this catches the drift so the local set can be updated.)
	for name := range actual {
		if _, ok := systemNamespaces[name]; !ok {
			t.Errorf(
				"API server has namespace %q not in local systemNamespaces set — update the set in cleanup_test.go and internal/core/cleanup.go",
				name,
			)
		}
	}
}

// TestReleaseCleanupNamespaces verifies that Release(false) removes all
// user-created namespaces so the next consumer gets a clean instance.
func TestReleaseCleanupNamespaces(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// Acquire an instance and create user namespaces.
	inst, client := acquireWithClient(ctx, t, sharedManager)
	instID := inst.ID()
	released := false
	defer func() {
		if !released {
			inst.Release(false) //nolint:errcheck,gosec // safety net on test failure
		}
	}()

	userNS := []string{
		uniqueNS("cleanup-a"),
		uniqueNS("cleanup-b"),
		uniqueNS("cleanup-c"),
	}
	for _, name := range userNS {
		createNamespace(ctx, t, client, name)
	}

	// Verify namespaces exist before release.
	nsList, err := client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list namespaces: %v", err)
	}
	if len(nsList.Items) < len(userNS)+len(systemNamespaces) {
		t.Fatalf("expected at least %d namespaces before release, got %d",
			len(userNS)+len(systemNamespaces), len(nsList.Items))
	}

	// Release without stopping — cleanup runs, instance returns to pool.
	if err := inst.Release(false); err != nil {
		t.Fatalf("Release(false) should succeed: %v", err)
	}
	released = true

	// Re-acquire. The pool is LIFO, so we expect the same instance back
	// (no other test should be using this specific instance since we just
	// released it and immediately re-acquire).
	inst2, client2 := acquireWithClient(ctx, t, sharedManager)
	defer func() {
		if err := inst2.Release(false); err != nil {
			t.Logf("release error: %v", err)
		}
	}()

	// If we got the same instance back, verify namespaces are clean.
	// If we got a different one, the test is still valid — the new instance
	// should have only system namespaces.
	if inst2.ID() == instID {
		t.Log("got same instance back (LIFO)")
	} else {
		t.Log("got different instance (pool concurrency)")
	}

	nsList2, err := client2.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list namespaces after re-acquire: %v", err)
	}

	for _, ns := range nsList2.Items {
		if _, ok := systemNamespaces[ns.Name]; !ok {
			t.Errorf("unexpected user namespace %q found after cleanup", ns.Name)
		}
	}
}

// TestReleasePreservesSystemNamespaces verifies that the cleanup step
// preserves default, kube-system, kube-public, and kube-node-lease.
func TestReleasePreservesSystemNamespaces(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// Create a user namespace so cleanup actually runs (not just the fast path).
	inst, client := acquireWithClient(ctx, t, sharedManager)
	createNamespace(ctx, t, client, uniqueNS("preserve-test"))

	if err := inst.Release(false); err != nil {
		t.Fatalf("Release(false) failed: %v", err)
	}

	// Re-acquire and verify system namespaces exist.
	inst2, client2 := acquireWithClient(ctx, t, sharedManager)
	defer func() {
		if err := inst2.Release(false); err != nil {
			t.Logf("release error: %v", err)
		}
	}()

	nsList, err := client2.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list namespaces: %v", err)
	}

	found := make(map[string]bool)
	for _, ns := range nsList.Items {
		found[ns.Name] = true
	}

	for name := range systemNamespaces {
		if !found[name] {
			t.Errorf("system namespace %q missing after cleanup", name)
		}
	}
}

// TestReleaseCleanupWithNoUserNamespaces verifies that Release(false) succeeds
// quickly when no user namespaces exist (fast path).
func TestReleaseCleanupWithNoUserNamespaces(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	inst, err := sharedManager.Acquire(ctx)
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}

	// Release immediately without creating any namespaces.
	if err := inst.Release(false); err != nil {
		t.Fatalf("Release(false) with no user namespaces should succeed: %v", err)
	}
}

// createNamespace is a test helper that creates a namespace and fails the test on error.
func createNamespace(ctx context.Context, t *testing.T, client kubernetes.Interface, name string) {
	t.Helper()

	ns := &v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: name},
	}
	if _, err := client.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create namespace %s: %v", name, err)
	}
}
