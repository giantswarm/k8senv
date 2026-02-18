//go:build integration

package k8senv_crd_test

import (
	"fmt"
	"os"
	"path/filepath"
)

// =============================================================================
// CRD YAML constants -- each file in the shared CRD dir uses a unique CRD name
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
const sampleMultiDoc = sampleCRDThingamajig + "---\n" + sampleConfigMap

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

// setupSharedCRDDir creates a CRD directory under baseDir containing all CRDs
// needed by this package's tests. Returns the path to the CRD directory.
func setupSharedCRDDir(baseDir string) (string, error) {
	crdDir := filepath.Join(baseDir, "crds")
	if err := os.MkdirAll(crdDir, 0o750); err != nil {
		return "", fmt.Errorf("mkdir: %w", err)
	}

	files := []struct {
		name    string
		content string
	}{
		{"widget-crd.yaml", sampleCRDWidget},
		{"gadget-crd.yaml", sampleCRDGadget},
		{"gizmo-crd.yaml", sampleCRDGizmo},
		{"multi.yaml", sampleMultiDoc},
		{"sprocket-crd.yml", sampleCRDSprocket}, // exercises .yml extension
	}

	for _, f := range files {
		if err := os.WriteFile(filepath.Join(crdDir, f.name), []byte(f.content), 0o644); err != nil {
			return "", fmt.Errorf("write %s: %w", f.name, err)
		}
	}

	return crdDir, nil
}
