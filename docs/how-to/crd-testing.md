# CRD Testing

k8senv supports CustomResourceDefinitions (CRDs) with automatic caching. This guide covers how to pre-apply CRDs and use them in tests.

## Overview

CRDs can be pre-applied during manager initialization using `WithCRDDir`. The system:

1. Reads all YAML files from the specified directory
2. Computes a hash of the combined content
3. Checks for an existing cached database with that hash
4. If no cache exists, spins up a temporary instance, applies CRDs, and saves the database
5. All instances start with a copy of this cached database

This means CRDs are applied once per unique configuration, not once per test.

## Using WithCRDDir

The recommended approach is to place CRD YAML files in a directory:

```
testdata/
└── crds/
    ├── widgets.example.com.yaml
    ├── gadgets.example.com.yaml
    └── gizmos.example.com.yaml
```

Then configure the manager:

```go
mgr := k8senv.NewManager(
    k8senv.WithCRDDir("testdata/crds"),
)
```

## CRD File Structure

Each YAML file should contain a complete CRD definition:

```yaml
# testdata/crds/widgets.example.com.yaml
apiVersion: apiextensions.k8s.io/v1
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
              color:
                type: string
```

Multiple CRDs can be in separate files or combined in a single file using `---` separators.

## Hash-Based Cache Invalidation

The cache key is computed from:

- All YAML file contents in the directory
- File names (to detect renames)

When you modify any CRD file:

1. The hash changes
2. A new cache is created on next `Initialize()`
3. Old caches remain (manual cleanup if needed)

Cache files are stored in the base data directory:
```
/tmp/k8senv/cached-abc123def.db
```

## Complete Example

```go
//go:build integration

package myproject_test

import (
    "context"
    "testing"
    "time"

    "github.com/giantswarm/k8senv"
    apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/client-go/kubernetes"
)

func TestWithCRDs(t *testing.T) {
    ctx := context.Background()

    mgr := k8senv.NewManager(
        k8senv.WithCRDDir("testdata/crds"),
        k8senv.WithAcquireTimeout(3*time.Minute), // Cache creation can take time
    )
    if err := mgr.Initialize(ctx); err != nil {
        t.Fatalf("Initialize failed: %v", err)
    }
    defer mgr.Shutdown()

    inst, err := mgr.Acquire(ctx)
    if err != nil {
        t.Fatalf("Acquire failed: %v", err)
    }
    defer inst.Release()

    cfg, err := inst.Config()
    if err != nil {
        t.Fatalf("Config failed: %v", err)
    }

    // Create apiextensions client to verify CRDs
    extClient, err := apiextensionsclient.NewForConfig(cfg)
    if err != nil {
        t.Fatalf("Failed to create apiextensions client: %v", err)
    }

    // List all CRDs
    crds, err := extClient.ApiextensionsV1().CustomResourceDefinitions().List(
        ctx, metav1.ListOptions{},
    )
    if err != nil {
        t.Fatalf("Failed to list CRDs: %v", err)
    }

    t.Logf("Found %d CRDs", len(crds.Items))
    for _, crd := range crds.Items {
        t.Logf("  - %s", crd.Name)
    }

    // Verify specific CRD exists
    crd, err := extClient.ApiextensionsV1().CustomResourceDefinitions().Get(
        ctx, "widgets.example.com", metav1.GetOptions{},
    )
    if err != nil {
        t.Fatalf("Widget CRD not found: %v", err)
    }
    t.Logf("Widget CRD is ready: %s", crd.Name)
}
```

## Using CRDs in Tests

After CRDs are applied, use a dynamic client or generated client to work with custom resources:

```go
import (
    "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
    "k8s.io/apimachinery/pkg/runtime/schema"
    "k8s.io/client-go/dynamic"
)

func TestCreateCustomResource(t *testing.T) {
    // ... setup manager and acquire instance ...

    cfg, err := inst.Config()
    if err != nil {
        t.Fatal(err)
    }

    // Create dynamic client
    dynClient, err := dynamic.NewForConfig(cfg)
    if err != nil {
        t.Fatal(err)
    }

    // Define the GVR for your custom resource
    widgetGVR := schema.GroupVersionResource{
        Group:    "example.com",
        Version:  "v1",
        Resource: "widgets",
    }

    // Create a custom resource
    widget := &unstructured.Unstructured{
        Object: map[string]interface{}{
            "apiVersion": "example.com/v1",
            "kind":       "Widget",
            "metadata": map[string]interface{}{
                "name":      "my-widget",
                "namespace": "default",
            },
            "spec": map[string]interface{}{
                "size":  "large",
                "color": "blue",
            },
        },
    }

    created, err := dynClient.Resource(widgetGVR).Namespace("default").Create(
        context.Background(), widget, metav1.CreateOptions{},
    )
    if err != nil {
        t.Fatalf("Failed to create widget: %v", err)
    }
    t.Logf("Created widget: %s", created.GetName())
}
```

## Cache Behavior

### First Run (Cache Miss)

1. `Initialize()` computes hash of CRD directory
2. No matching cache found
3. Temporary kine + kube-apiserver started
4. CRDs applied and wait for Established condition
5. Database saved as `cached-<hash>.db`
6. Temporary processes stopped

This adds significant time to first initialization.

### Subsequent Runs (Cache Hit)

1. `Initialize()` computes hash of CRD directory
2. Matching cache found: `cached-<hash>.db`
3. Initialization completes immediately
4. Instances copy from cached database on startup

### Cache Location

Caches are stored in the base data directory:

```
/tmp/k8senv/
├── cached-abc123.db      # CRD cache (persistent)
├── cached-def456.db      # Another CRD cache
├── inst-xyz789/          # Instance data (ephemeral)
│   ├── kine.db
│   ├── kine-stdout.log
│   └── ...
```

### Manual Cache Cleanup

Remove old caches:

```bash
rm /tmp/k8senv/cached-*.db
```

Or use `make clean` if available in your project.

## Timeout Considerations

Cache creation requires starting a full kube-apiserver, applying CRDs, and waiting for them to become established. Set a longer acquire timeout:

```go
k8senv.WithAcquireTimeout(3*time.Minute)
```

After the cache is created, subsequent runs are much faster.

## Related

- [Configuration](../reference/configuration.md) - All manager and instance options
- [Parallel Testing](parallel-testing.md) - Running CRD tests concurrently
- [Data Flow](../reference/data-flow.md) - CRD cache creation sequence diagram
- [Troubleshooting](../reference/troubleshooting.md) - CRD cache debugging
