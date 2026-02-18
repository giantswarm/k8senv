//go:build integration

package k8senv_crd_test

import (
	"context"
	"testing"

	"github.com/giantswarm/k8senv/tests/internal/testutil"
	v1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicclient "k8s.io/client-go/dynamic"
)

// TestCRDDirCaching verifies that CRDs from a directory are applied and cached.
func TestCRDDirCaching(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	inst, err := sharedManager.Acquire(ctx)
	if err != nil {
		t.Fatalf("Failed to acquire instance: %v", err)
	}
	defer func() {
		if err := inst.Release(); err != nil {
			t.Logf("release error: %v", err)
		}
	}()

	verifyCRDExists(ctx, t, inst, "widgets.example.com")
}

// TestCRDDirWithMultipleCRDs verifies that multiple CRDs are all applied.
func TestCRDDirWithMultipleCRDs(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	inst, err := sharedManager.Acquire(ctx)
	if err != nil {
		t.Fatalf("Failed to acquire instance: %v", err)
	}
	defer func() {
		if err := inst.Release(); err != nil {
			t.Logf("release error: %v", err)
		}
	}()

	verifyCRDExists(ctx, t, inst, "gadgets.example.com")
	verifyCRDExists(ctx, t, inst, "gizmos.example.com")
}

// TestCRDDirWithEstablishedCondition verifies that a cached CRD is established on an acquired instance.
func TestCRDDirWithEstablishedCondition(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	inst, client := testutil.AcquireWithClient(ctx, t, sharedManager)
	defer func() {
		if err := inst.Release(); err != nil {
			t.Logf("release error: %v", err)
		}
	}()

	// Create a test namespace
	nsName := testutil.UniqueName("test-widgets")
	ns := &v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsName}}
	if _, err := client.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{}); err != nil {
		t.Fatalf("Failed to create namespace: %v", err)
	}

	// Verify the CRD API is available
	cfg, err := inst.Config()
	if err != nil {
		t.Fatalf("Failed to get config: %v", err)
	}

	extClient, err := apiextensionsclient.NewForConfig(cfg)
	if err != nil {
		t.Fatalf("Failed to create apiextensions client: %v", err)
	}

	crd, err := extClient.ApiextensionsV1().
		CustomResourceDefinitions().
		Get(ctx, "widgets.example.com", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("CRD not found: %v", err)
	}

	// Verify CRD has Established condition (should already be set in the cached DB)
	established := false
	for _, cond := range crd.Status.Conditions {
		if cond.Type == apiextensionsv1.Established && cond.Status == apiextensionsv1.ConditionTrue {
			established = true
			break
		}
	}
	if !established {
		t.Errorf("CRD not yet established (conditions: %v)", crd.Status.Conditions)
	}
}

// TestCRDDirWithMultiDocumentYAML exercises multi-document YAML processing.
func TestCRDDirWithMultiDocumentYAML(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	inst, client := testutil.AcquireWithClient(ctx, t, sharedManager)
	defer func() {
		if err := inst.Release(); err != nil {
			t.Logf("release error: %v", err)
		}
	}()

	// Verify the multi-doc CRD was applied
	verifyCRDExists(ctx, t, inst, "thingamajigs.example.com")

	// Verify the ConfigMap was applied in the default namespace
	cm, err := client.CoreV1().ConfigMaps("default").Get(ctx, "test-config", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("ConfigMap not found: %v", err)
	}
	if cm.Data["key"] != "value" {
		t.Errorf("ConfigMap data mismatch: got %v", cm.Data)
	}

	t.Log("Multi-document YAML with CRD and ConfigMap applied successfully")
}

// TestCRDDirWithYmlExtension exercises the .yml extension match in walkYAMLFiles.
func TestCRDDirWithYmlExtension(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	inst, err := sharedManager.Acquire(ctx)
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}
	defer func() {
		if err := inst.Release(); err != nil {
			t.Logf("release error: %v", err)
		}
	}()

	// The sprocket CRD comes from a .yml file
	verifyCRDExists(ctx, t, inst, "sprockets.example.com")

	t.Log("CRD from .yml extension file applied successfully")
}

// TestReleaseCleanupCRDResources verifies that Release() removes CRD
// instances in non-system namespaces. This exercises the dynamic-client
// resource cleanup path for custom resource types that are not known at
// compile time.
func TestReleaseCleanupCRDResources(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	inst, client := testutil.AcquireWithClient(ctx, t, sharedManager)
	instID := inst.ID()
	released := false
	defer func() {
		if !released {
			inst.Release() //nolint:errcheck,gosec // safety net on test failure
		}
	}()

	// Create a namespace for the CRD instance.
	nsName := testutil.UniqueName("crd-cleanup")
	ns := &v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsName}}
	if _, createErr := client.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{}); createErr != nil {
		t.Fatalf("create namespace: %v", createErr)
	}

	// Create a Widget CR using the dynamic client.
	cfg, err := inst.Config()
	if err != nil {
		t.Fatalf("get config: %v", err)
	}
	dynClient, err := dynamicclient.NewForConfig(cfg)
	if err != nil {
		t.Fatalf("create dynamic client: %v", err)
	}

	widgetGVR := schema.GroupVersionResource{
		Group:    "example.com",
		Version:  "v1",
		Resource: "widgets",
	}
	widget := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "example.com/v1",
			"kind":       "Widget",
			"metadata": map[string]any{
				"name":      "test-widget",
				"namespace": nsName,
			},
			"spec": map[string]any{
				"size": "large",
			},
		},
	}
	if _, createErr := dynClient.Resource(widgetGVR).
		Namespace(nsName).
		Create(ctx, widget, metav1.CreateOptions{}); createErr != nil {
		t.Fatalf("create Widget CR: %v", createErr)
	}

	// Release and re-acquire.
	if err := inst.Release(); err != nil {
		t.Fatalf("Release() failed: %v", err)
	}
	released = true

	inst2, _ := testutil.AcquireWithClient(ctx, t, sharedManager)
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

	// Re-create dynamic client for the (possibly different) instance.
	cfg2, err := inst2.Config()
	if err != nil {
		t.Fatalf("get config after re-acquire: %v", err)
	}
	dynClient2, err := dynamicclient.NewForConfig(cfg2)
	if err != nil {
		t.Fatalf("create dynamic client after re-acquire: %v", err)
	}

	// Verify the Widget CR is gone.
	widgetList, err := dynClient2.Resource(widgetGVR).Namespace("").List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list Widgets: %v", err)
	}
	for _, item := range widgetList.Items {
		if item.GetNamespace() == nsName {
			t.Errorf("Widget %s/%s should have been cleaned up", item.GetNamespace(), item.GetName())
		}
	}
}
