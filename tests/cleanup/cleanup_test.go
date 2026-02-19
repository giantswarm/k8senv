//go:build integration

package k8senv_cleanup_test

import (
	"context"
	"testing"

	"github.com/giantswarm/k8senv/tests/internal/testutil"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestSystemNamespacesMatchAPIServer verifies that testutil.SystemNamespaces()
// matches exactly the namespaces that kube-apiserver creates on startup.
// This catches drift between the authoritative set in internal/core/cleanup.go
// and what a real kube-apiserver actually creates.
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
				"API server has namespace %q not in SystemNamespaces set — update internal/core/cleanup.go:systemNamespaces",
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
	testutil.ReleaseRemovesUserNamespaces(t, context.Background(), sharedManager, "cleanup")
}

// TestReleasePreservesSystemNamespaces verifies that the cleanup step
// preserves default, kube-system, kube-public, and kube-node-lease.
func TestReleasePreservesSystemNamespaces(t *testing.T) {
	t.Parallel()
	testutil.ReleasePreservesSystemNamespaces(t, context.Background(), sharedManager, "cleanup")
}

// TestReleaseCleanupWithNoUserNamespaces verifies that Release() with
// ReleaseClean succeeds quickly when no user namespaces exist (fast path).
func TestReleaseCleanupWithNoUserNamespaces(t *testing.T) {
	t.Parallel()
	testutil.ReleaseWithNoUserNamespaces(t, context.Background(), sharedManager)
}

// TestReleaseCleanupNamespacedResources verifies that Release() removes
// all user-created resources inside non-system namespaces.
func TestReleaseCleanupNamespacedResources(t *testing.T) {
	t.Parallel()
	testutil.ReleaseRemovesNamespacedResources(t, context.Background(), sharedManager, "cleanup")
}

// TestReleaseCleanupResourcesWithFinalizers verifies that Release() clears
// finalizers and deletes resources that have finalizers set.
func TestReleaseCleanupResourcesWithFinalizers(t *testing.T) {
	t.Parallel()
	testutil.ReleaseRemovesResourcesWithFinalizers(t, context.Background(), sharedManager, "cleanup")
}

// TestReleaseCleanupPreservesSystemNamespaceResources verifies that resources
// in system namespaces are not deleted during cleanup.
func TestReleaseCleanupPreservesSystemNamespaceResources(t *testing.T) {
	t.Parallel()
	testutil.ReleasePreservesSystemNamespaceResources(t, context.Background(), sharedManager, "cleanup")
}
