//go:build integration

package k8senv_purge_test

import (
	"context"
	"testing"

	"github.com/giantswarm/k8senv/tests/internal/testutil"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// TestReleasePurgeNamespaces verifies that Release() with ReleasePurge
// strategy removes all user-created namespaces so the next consumer gets
// a clean instance.
func TestReleasePurgeNamespaces(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// Acquire an instance and create user namespaces.
	inst, client, release := testutil.AcquireWithGuardedRelease(ctx, t, sharedManager)
	instID := inst.ID()

	userNS := []string{
		testutil.UniqueName("purge-a"),
		testutil.UniqueName("purge-b"),
		testutil.UniqueName("purge-c"),
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

	// Release — purge runs (ReleasePurge strategy), instance returns to pool.
	release()

	// Re-acquire. The pool is LIFO, so we expect the same instance back.
	inst2, client2 := testutil.AcquireWithClient(ctx, t, sharedManager)
	defer func() {
		if err := inst2.Release(); err != nil {
			t.Logf("release error: %v", err)
		}
	}()

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
			t.Errorf("unexpected user namespace %q found after purge", ns.Name)
		}
	}
}

// TestReleasePurgePreservesSystemNamespaces verifies that the purge step
// preserves default, kube-system, kube-public, and kube-node-lease.
func TestReleasePurgePreservesSystemNamespaces(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// Create a user namespace so purge actually runs (not just the fast path).
	inst, client := testutil.AcquireWithClient(ctx, t, sharedManager)
	createNamespace(ctx, t, client, testutil.UniqueName("preserve-test"))

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
			t.Errorf("system namespace %q missing after purge", name)
		}
	}
}

// TestReleasePurgeWithNoUserNamespaces verifies that Release() with
// ReleasePurge succeeds quickly when no user namespaces exist (fast path).
func TestReleasePurgeWithNoUserNamespaces(t *testing.T) {
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

// TestReleasePurgeNamespacedResources verifies that Release() removes
// all user-created resources inside non-system namespaces.
func TestReleasePurgeNamespacedResources(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	inst, client, release := testutil.AcquireWithGuardedRelease(ctx, t, sharedManager)
	instID := inst.ID()

	nsName := testutil.UniqueName("res-purge")
	createNamespace(ctx, t, client, nsName)

	// Create resources in the namespace.
	cm := &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "test-cm", Namespace: nsName},
		Data:       map[string]string{"key": "value"},
	}
	if _, err := client.CoreV1().ConfigMaps(nsName).Create(ctx, cm, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create ConfigMap: %v", err)
	}

	secret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "test-secret", Namespace: nsName},
		StringData: map[string]string{"password": "hunter2"},
	}
	if _, err := client.CoreV1().Secrets(nsName).Create(ctx, secret, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create Secret: %v", err)
	}

	// Release and re-acquire.
	release()

	inst2, client2 := testutil.AcquireWithClient(ctx, t, sharedManager)
	defer func() {
		if err := inst2.Release(); err != nil {
			t.Logf("release error: %v", err)
		}
	}()

	if inst2.ID() == instID {
		t.Log("got same instance back (LIFO)")
	} else {
		t.Log("got different instance (pool concurrency)")
	}

	// Verify namespace is gone.
	nsList, err := client2.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list namespaces: %v", err)
	}
	for _, ns := range nsList.Items {
		if ns.Name == nsName {
			t.Errorf("namespace %q should have been purged", nsName)
		}
	}

	// Verify resources are gone (list across all namespaces).
	cmList, err := client2.CoreV1().ConfigMaps("").List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list ConfigMaps: %v", err)
	}
	for _, item := range cmList.Items {
		if item.Namespace == nsName {
			t.Errorf("ConfigMap %s/%s should have been purged", item.Namespace, item.Name)
		}
	}

	secretList, err := client2.CoreV1().Secrets("").List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list Secrets: %v", err)
	}
	for _, item := range secretList.Items {
		if item.Namespace == nsName {
			t.Errorf("Secret %s/%s should have been purged", item.Namespace, item.Name)
		}
	}
}

// TestReleasePurgeResourcesWithFinalizers verifies that Release() purges
// resources that have finalizers set. SQL deletion bypasses the Kubernetes
// admission chain, so finalizers have no effect.
func TestReleasePurgeResourcesWithFinalizers(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	inst, client, release := testutil.AcquireWithGuardedRelease(ctx, t, sharedManager)
	instID := inst.ID()

	nsName := testutil.UniqueName("finalizer-purge")
	createNamespace(ctx, t, client, nsName)

	// Create a ConfigMap with a finalizer.
	cm := &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "finalized-cm",
			Namespace:  nsName,
			Finalizers: []string{"test.example.com/block-deletion"},
		},
		Data: map[string]string{"key": "value"},
	}
	if _, err := client.CoreV1().ConfigMaps(nsName).Create(ctx, cm, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create ConfigMap with finalizer: %v", err)
	}

	// Release and re-acquire.
	release()

	inst2, client2 := testutil.AcquireWithClient(ctx, t, sharedManager)
	defer func() {
		if err := inst2.Release(); err != nil {
			t.Logf("release error: %v", err)
		}
	}()

	if inst2.ID() == instID {
		t.Log("got same instance back (LIFO)")
	} else {
		t.Log("got different instance (pool concurrency)")
	}

	// Verify the finalized resource is gone.
	cmList, err := client2.CoreV1().ConfigMaps("").List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list ConfigMaps: %v", err)
	}
	for _, item := range cmList.Items {
		if item.Namespace == nsName {
			t.Errorf("ConfigMap %s/%s should have been purged (had finalizer)", item.Namespace, item.Name)
		}
	}
}

// TestReleasePurgePreservesSystemNamespaceResources verifies that resources
// in system namespaces are not deleted during purge.
func TestReleasePurgePreservesSystemNamespaceResources(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	inst, client, release := testutil.AcquireWithGuardedRelease(ctx, t, sharedManager)
	instID := inst.ID()

	// Create a ConfigMap in kube-system.
	cmName := testutil.UniqueName("sys-cm")
	cm := &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: cmName, Namespace: "kube-system"},
		Data:       map[string]string{"key": "preserved"},
	}
	if _, err := client.CoreV1().ConfigMaps("kube-system").Create(ctx, cm, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create ConfigMap in kube-system: %v", err)
	}

	// Also create a user namespace so purge actually runs.
	userNS := testutil.UniqueName("trigger-purge")
	createNamespace(ctx, t, client, userNS)

	release()

	inst2, client2 := testutil.AcquireWithClient(ctx, t, sharedManager)
	defer func() {
		// Clean up the system namespace ConfigMap we created.
		_ = client2.CoreV1().ConfigMaps("kube-system").Delete(ctx, cmName, metav1.DeleteOptions{})
		if err := inst2.Release(); err != nil {
			t.Logf("release error: %v", err)
		}
	}()

	// Each instance is a separate kube-apiserver. If concurrent tests grabbed
	// our instance before we re-acquired, we get a different one where the
	// ConfigMap was never created. Only verify when we got the same instance.
	if inst2.ID() != instID {
		t.Skip("got different instance (pool concurrency); skipping system resource verification")
	}

	// Verify the kube-system ConfigMap still exists.
	got, err := client2.CoreV1().ConfigMaps("kube-system").Get(ctx, cmName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("ConfigMap in kube-system should survive purge: %v", err)
	}
	if got.Data["key"] != "preserved" {
		t.Errorf("ConfigMap data mismatch: got %v", got.Data)
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
