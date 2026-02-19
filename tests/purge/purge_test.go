//go:build integration

package k8senv_purge_test

import (
	"context"
	"testing"

	"github.com/giantswarm/k8senv/tests/internal/testutil"
)

// TestReleasePurgeNamespaces verifies that Release() with ReleasePurge
// strategy removes all user-created namespaces so the next consumer gets
// a clean instance.
func TestReleasePurgeNamespaces(t *testing.T) {
	t.Parallel()
	testutil.ReleaseRemovesUserNamespaces(t, context.Background(), sharedManager, "purge")
}

// TestReleasePurgePreservesSystemNamespaces verifies that the purge step
// preserves default, kube-system, kube-public, and kube-node-lease.
func TestReleasePurgePreservesSystemNamespaces(t *testing.T) {
	t.Parallel()
	testutil.ReleasePreservesSystemNamespaces(t, context.Background(), sharedManager, "purge")
}

// TestReleasePurgeWithNoUserNamespaces verifies that Release() with
// ReleasePurge succeeds quickly when no user namespaces exist (fast path).
func TestReleasePurgeWithNoUserNamespaces(t *testing.T) {
	t.Parallel()
	testutil.ReleaseWithNoUserNamespaces(t, context.Background(), sharedManager)
}

// TestReleasePurgeNamespacedResources verifies that Release() removes
// all user-created resources inside non-system namespaces.
func TestReleasePurgeNamespacedResources(t *testing.T) {
	t.Parallel()
	testutil.ReleaseRemovesNamespacedResources(t, context.Background(), sharedManager, "purge")
}

// TestReleasePurgeResourcesWithFinalizers verifies that Release() purges
// resources that have finalizers set. SQL deletion bypasses the Kubernetes
// admission chain, so finalizers have no effect.
func TestReleasePurgeResourcesWithFinalizers(t *testing.T) {
	t.Parallel()
	testutil.ReleaseRemovesResourcesWithFinalizers(t, context.Background(), sharedManager, "purge")
}

// TestReleasePurgePreservesSystemNamespaceResources verifies that resources
// in system namespaces are not deleted during purge.
func TestReleasePurgePreservesSystemNamespaceResources(t *testing.T) {
	t.Parallel()
	testutil.ReleasePreservesSystemNamespaceResources(t, context.Background(), sharedManager, "purge")
}
