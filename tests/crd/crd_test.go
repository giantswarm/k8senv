//go:build integration

package k8senv_crd_test

import (
	"context"
	"testing"

	v1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// =============================================================================
// CRD YAML constants — each file in the shared CRD dir uses a unique CRD name
// to avoid "already exists" conflicts when applied by a single manager.
// =============================================================================

// sampleCRDWidget is the Widget CRD (widgets.example.com), loaded from widget-crd.yaml.
const sampleCRDWidget = `apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: widgets.example.com
spec:
  group: example.com
  names:
    kind: Widget
    listKind: WidgetList
    plural: widgets
    singular: widget
  scope: Namespaced
  versions:
  - name: v1
    served: true
    storage: true
    schema:
      openAPIV3Schema:
        type: object
        properties:
          spec:
            type: object
            properties:
              size:
                type: string
`

// sampleCRDGadget is the Gadget CRD (gadgets.example.com), loaded from gadget-crd.yaml.
const sampleCRDGadget = `apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: gadgets.example.com
spec:
  group: example.com
  names:
    kind: Gadget
    listKind: GadgetList
    plural: gadgets
    singular: gadget
  scope: Namespaced
  versions:
  - name: v1
    served: true
    storage: true
    schema:
      openAPIV3Schema:
        type: object
`

// sampleCRDGizmo is the Gizmo CRD (gizmos.example.com), loaded from gizmo-crd.yaml.
const sampleCRDGizmo = `apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: gizmos.example.com
spec:
  group: example.com
  names:
    kind: Gizmo
    listKind: GizmoList
    plural: gizmos
    singular: gizmo
  scope: Cluster
  versions:
  - name: v1alpha1
    served: true
    storage: true
    schema:
      openAPIV3Schema:
        type: object
`

// sampleCRDThingamajig is the Thingamajig CRD (thingamajigs.example.com),
// used in multi-document YAML (multi.yaml) alongside a ConfigMap.
const sampleCRDThingamajig = `apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: thingamajigs.example.com
spec:
  group: example.com
  names:
    kind: Thingamajig
    listKind: ThingamajigList
    plural: thingamajigs
    singular: thingamajig
  scope: Namespaced
  versions:
  - name: v1
    served: true
    storage: true
    schema:
      openAPIV3Schema:
        type: object
`

// sampleConfigMap is a ConfigMap for the multi-document YAML test.
const sampleConfigMap = `apiVersion: v1
kind: ConfigMap
metadata:
  name: test-config
data:
  key: value
`

// sampleMultiDoc is a multi-document YAML with a unique CRD + ConfigMap.
var sampleMultiDoc = sampleCRDThingamajig + "---\n" + sampleConfigMap

// sampleCRDSprocket is the Sprocket CRD (sprockets.example.com),
// loaded from sprocket-crd.yml to exercise .yml extension matching.
const sampleCRDSprocket = `apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: sprockets.example.com
spec:
  group: example.com
  names:
    kind: Sprocket
    listKind: SprocketList
    plural: sprockets
    singular: sprocket
  scope: Namespaced
  versions:
  - name: v1
    served: true
    storage: true
    schema:
      openAPIV3Schema:
        type: object
`

// =============================================================================
// CRD Tests
// =============================================================================

// TestCRDDirCaching verifies that CRDs from a directory are applied and cached.
func TestCRDDirCaching(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	inst, err := sharedManager.Acquire(ctx)
	if err != nil {
		t.Fatalf("Failed to acquire instance: %v", err)
	}
	defer func() {
		if err := inst.Release(false); err != nil {
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
		if err := inst.Release(false); err != nil {
			t.Logf("release error: %v", err)
		}
	}()

	verifyCRDExists(ctx, t, inst, "gadgets.example.com")
	verifyCRDExists(ctx, t, inst, "gizmos.example.com")
}

// TestCRDDirWithCRAndInstance verifies that CRs can be created using CRDs from cache.
func TestCRDDirWithCRAndInstance(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	inst, client := acquireWithClient(ctx, t, sharedManager)
	defer func() {
		if err := inst.Release(false); err != nil {
			t.Logf("release error: %v", err)
		}
	}()

	// Create a test namespace
	nsName := uniqueNS("test-widgets")
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

	inst, client := acquireWithClient(ctx, t, sharedManager)
	defer func() {
		if err := inst.Release(false); err != nil {
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
		if err := inst.Release(false); err != nil {
			t.Logf("release error: %v", err)
		}
	}()

	// The sprocket CRD comes from a .yml file
	verifyCRDExists(ctx, t, inst, "sprockets.example.com")

	t.Log("CRD from .yml extension file applied successfully")
}
