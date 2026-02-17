//go:build integration

package k8senv_cleanup_test

import (
	"context"
	"testing"

	"github.com/giantswarm/k8senv/tests/internal/testutil"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// TestSystemNamespacesMatchAPIServer verifies that testutil.SystemNamespaces()
// matches exactly the namespaces that kube-apiserver creates on startup.
// This catches drift between the shared test set and the authoritative set in
// internal/core/cleanup.go (which the test package cannot import by design).
func TestSystemNamespacesMatchAPIServer(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// Acquire a fresh instance and list namespaces before creating any user
	// resources. The only namespaces present should be the ones kube-apiserver
	// creates automatically on startup.
	inst, client := testutil.AcquireWithClient(ctx, t, sharedManager)
	defer func() {
		if err := inst.Release(); err != nil {
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
	sysNS := testutil.SystemNamespaces()
	for name := range sysNS {
		if _, ok := actual[name]; !ok {
			t.Errorf("expected system namespace %q not found on API server", name)
		}
	}

	// Verify the API server has no extra namespaces beyond the expected set.
	// (If kube-apiserver adds a new default namespace in a future version,
	// this catches the drift so the local set can be updated.)
	for name := range actual {
		if _, ok := sysNS[name]; !ok {
			t.Errorf(
				"API server has namespace %q not in SystemNamespaces set — update testutil.systemNamespaces and internal/core/cleanup.go",
				name,
			)
		}
	}
}

// TestReleaseCleanupNamespaces verifies that Release() with ReleaseClean
// strategy removes all user-created namespaces so the next consumer gets
// a clean instance.
func TestReleaseCleanupNamespaces(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// Acquire an instance and create user namespaces.
	inst, client := testutil.AcquireWithClient(ctx, t, sharedManager)
	instID := inst.ID()
	released := false
	defer func() {
		if !released {
			inst.Release() //nolint:errcheck,gosec // safety net on test failure
		}
	}()

	userNS := []string{
		testutil.UniqueNS("cleanup-a"),
		testutil.UniqueNS("cleanup-b"),
		testutil.UniqueNS("cleanup-c"),
	}
	for _, name := range userNS {
		createNamespace(ctx, t, client, name)
	}

	// Verify namespaces exist before release.
	nsList, err := client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list namespaces: %v", err)
	}
	if len(nsList.Items) < len(userNS)+len(testutil.SystemNamespaces()) {
		t.Fatalf("expected at least %d namespaces before release, got %d",
			len(userNS)+len(testutil.SystemNamespaces()), len(nsList.Items))
	}

	// Release — cleanup runs (ReleaseClean strategy), instance returns to pool.
	if err := inst.Release(); err != nil {
		t.Fatalf("Release() should succeed: %v", err)
	}
	released = true

	// Re-acquire. The pool is LIFO, so we expect the same instance back
	// (no other test should be using this specific instance since we just
	// released it and immediately re-acquire).
	inst2, client2 := testutil.AcquireWithClient(ctx, t, sharedManager)
	defer func() {
		if err := inst2.Release(); err != nil {
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

	sysNS2 := testutil.SystemNamespaces()
	for _, ns := range nsList2.Items {
		if _, ok := sysNS2[ns.Name]; !ok {
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
	inst, client := testutil.AcquireWithClient(ctx, t, sharedManager)
	createNamespace(ctx, t, client, testutil.UniqueNS("preserve-test"))

	if err := inst.Release(); err != nil {
		t.Fatalf("Release() failed: %v", err)
	}

	// Re-acquire and verify system namespaces exist.
	inst2, client2 := testutil.AcquireWithClient(ctx, t, sharedManager)
	defer func() {
		if err := inst2.Release(); err != nil {
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

	for name := range testutil.SystemNamespaces() {
		if !found[name] {
			t.Errorf("system namespace %q missing after cleanup", name)
		}
	}
}

// TestReleaseCleanupWithNoUserNamespaces verifies that Release() with
// ReleaseClean succeeds quickly when no user namespaces exist (fast path).
func TestReleaseCleanupWithNoUserNamespaces(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	inst, err := sharedManager.Acquire(ctx)
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}

	// Release immediately without creating any namespaces.
	if err := inst.Release(); err != nil {
		t.Fatalf("Release() with no user namespaces should succeed: %v", err)
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
