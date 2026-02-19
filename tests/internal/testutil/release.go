//go:build integration

package testutil

import (
	"context"
	"testing"

	"github.com/giantswarm/k8senv"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ReleaseRemovesUserNamespaces verifies that releasing an instance removes all
// user-created namespaces so the next consumer gets a clean instance. The label
// parameter (e.g. "cleanup", "purge") is used for unique name prefixes and log
// messages.
func ReleaseRemovesUserNamespaces(t *testing.T, ctx context.Context, mgr k8senv.Manager, label string) {
	t.Helper()

	inst, client, release := AcquireWithGuardedRelease(ctx, t, mgr)
	instID := inst.ID()

	userNS := []string{
		UniqueName(label + "-a"),
		UniqueName(label + "-b"),
		UniqueName(label + "-c"),
	}
	for _, name := range userNS {
		CreateNamespace(ctx, t, client, name)
	}

	// Verify namespaces exist before release.
	nsList, err := client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list namespaces: %v", err)
	}
	if len(nsList.Items) < len(userNS)+len(SystemNamespaces()) {
		t.Fatalf("expected at least %d namespaces before release, got %d",
			len(userNS)+len(SystemNamespaces()), len(nsList.Items))
	}

	// Release — strategy runs, instance returns to pool.
	release()

	// Re-acquire. We may get the same instance back or a different one
	// depending on pool scheduling; the assertion below is valid either way
	// because every instance should have only system namespaces after release.
	inst2, client2 := AcquireWithClient(ctx, t, mgr)
	defer func() {
		if err := inst2.Release(); err != nil {
			t.Logf("release error: %v", err)
		}
	}()

	if inst2.ID() == instID {
		t.Log("got same instance back")
	} else {
		t.Log("got different instance")
	}

	nsList2, err := client2.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list namespaces after re-acquire: %v", err)
	}

	sysNS := SystemNamespaces()
	for i := range nsList2.Items {
		if _, ok := sysNS[nsList2.Items[i].Name]; !ok {
			t.Errorf("unexpected user namespace %q found after %s", nsList2.Items[i].Name, label)
		}
	}
}

// ReleasePreservesSystemNamespaces verifies that releasing an instance
// preserves the default, kube-system, kube-public, and kube-node-lease
// namespaces. The label is used for unique name prefixes.
func ReleasePreservesSystemNamespaces(t *testing.T, ctx context.Context, mgr k8senv.Manager, label string) {
	t.Helper()

	// Create a user namespace so the release strategy actually runs (not just the fast path).
	inst, client := AcquireWithClient(ctx, t, mgr)
	CreateNamespace(ctx, t, client, UniqueName("preserve-"+label))

	if err := inst.Release(); err != nil {
		t.Fatalf("Release() failed: %v", err)
	}

	// Re-acquire and verify system namespaces exist.
	inst2, client2 := AcquireWithClient(ctx, t, mgr)
	defer func() {
		if err := inst2.Release(); err != nil {
			t.Logf("release error: %v", err)
		}
	}()

	nsList, err := client2.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list namespaces: %v", err)
	}

	found := make(map[string]struct{}, len(nsList.Items))
	for i := range nsList.Items {
		found[nsList.Items[i].Name] = struct{}{}
	}

	for name := range SystemNamespaces() {
		if _, ok := found[name]; !ok {
			t.Errorf("system namespace %q missing after %s", name, label)
		}
	}
}

// ReleaseWithNoUserNamespaces verifies that releasing an instance succeeds
// quickly when no user namespaces exist (fast path).
func ReleaseWithNoUserNamespaces(t *testing.T, ctx context.Context, mgr k8senv.Manager) {
	t.Helper()

	inst, err := mgr.Acquire(ctx)
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}

	// Release immediately without creating any namespaces.
	if err := inst.Release(); err != nil {
		t.Fatalf("Release() with no user namespaces should succeed: %v", err)
	}
}

// ReleaseRemovesNamespacedResources verifies that releasing an instance removes
// all user-created resources (ConfigMaps, Secrets) inside non-system
// namespaces. The label is used for unique name prefixes.
func ReleaseRemovesNamespacedResources(t *testing.T, ctx context.Context, mgr k8senv.Manager, label string) {
	t.Helper()

	inst, client, release := AcquireWithGuardedRelease(ctx, t, mgr)
	instID := inst.ID()

	nsName := UniqueName("res-" + label)
	CreateNamespace(ctx, t, client, nsName)

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

	inst2, client2 := AcquireWithClient(ctx, t, mgr)
	defer func() {
		if err := inst2.Release(); err != nil {
			t.Logf("release error: %v", err)
		}
	}()

	if inst2.ID() == instID {
		t.Log("got same instance back")
	} else {
		t.Log("got different instance")
	}

	// Verify namespace is gone.
	nsList, err := client2.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list namespaces: %v", err)
	}
	for i := range nsList.Items {
		if nsList.Items[i].Name == nsName {
			t.Errorf("namespace %q should have been removed by %s", nsName, label)
		}
	}

	// Verify resources are gone (list across all namespaces).
	cmList, err := client2.CoreV1().ConfigMaps("").List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list ConfigMaps: %v", err)
	}
	for i := range cmList.Items {
		if cmList.Items[i].Namespace == nsName {
			t.Errorf("ConfigMap %s/%s should have been removed by %s",
				cmList.Items[i].Namespace, cmList.Items[i].Name, label)
		}
	}

	secretList, err := client2.CoreV1().Secrets("").List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list Secrets: %v", err)
	}
	for i := range secretList.Items {
		if secretList.Items[i].Namespace == nsName {
			t.Errorf(
				"Secret %s/%s should have been removed by %s",
				secretList.Items[i].Namespace,
				secretList.Items[i].Name,
				label,
			)
		}
	}
}

// ReleaseRemovesResourcesWithFinalizers verifies that releasing an instance
// removes resources that have finalizers set. The label is used for unique name
// prefixes.
func ReleaseRemovesResourcesWithFinalizers(t *testing.T, ctx context.Context, mgr k8senv.Manager, label string) {
	t.Helper()

	inst, client, release := AcquireWithGuardedRelease(ctx, t, mgr)
	instID := inst.ID()

	nsName := UniqueName("finalizer-" + label)
	CreateNamespace(ctx, t, client, nsName)

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

	inst2, client2 := AcquireWithClient(ctx, t, mgr)
	defer func() {
		if err := inst2.Release(); err != nil {
			t.Logf("release error: %v", err)
		}
	}()

	if inst2.ID() == instID {
		t.Log("got same instance back")
	} else {
		t.Log("got different instance")
	}

	// Verify the finalized resource is gone.
	cmList, err := client2.CoreV1().ConfigMaps("").List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list ConfigMaps: %v", err)
	}
	for i := range cmList.Items {
		if cmList.Items[i].Namespace == nsName {
			t.Errorf(
				"ConfigMap %s/%s should have been removed by %s (had finalizer)",
				cmList.Items[i].Namespace,
				cmList.Items[i].Name,
				label,
			)
		}
	}
}

// ReleasePreservesSystemNamespaceResources verifies that resources in system
// namespaces are not deleted during release. The label is used for unique name
// prefixes.
func ReleasePreservesSystemNamespaceResources(t *testing.T, ctx context.Context, mgr k8senv.Manager, label string) {
	t.Helper()

	inst, client, release := AcquireWithGuardedRelease(ctx, t, mgr)
	instID := inst.ID()

	// Create a ConfigMap in kube-system.
	cmName := UniqueName("sys-cm")
	cm := &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: cmName, Namespace: "kube-system"},
		Data:       map[string]string{"key": "preserved"},
	}
	if _, err := client.CoreV1().ConfigMaps("kube-system").Create(ctx, cm, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create ConfigMap in kube-system: %v", err)
	}

	// Also create a user namespace so the release strategy actually runs.
	userNS := UniqueName("trigger-" + label)
	CreateNamespace(ctx, t, client, userNS)

	release()

	inst2, client2 := AcquireWithClient(ctx, t, mgr)
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
		t.Fatalf("ConfigMap in kube-system should survive %s: %v", label, err)
	}
	if got.Data["key"] != "preserved" {
		t.Errorf("ConfigMap data mismatch: got %v", got.Data)
	}
}
