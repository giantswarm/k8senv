# Getting Started

This guide walks you through installing k8senv and writing your first test.

## Prerequisites

- **Go 1.25+**
- **kine** binary in `$PATH`
- **kube-apiserver** binary in `$PATH`

### Installing kine

```bash
go install github.com/k3s-io/kine/cmd/kine@latest
```

Verify installation:
```bash
kine --version
```

### Installing kube-apiserver

Download the binary for your platform:

**Linux (amd64):**
```bash
curl -LO https://dl.k8s.io/v1.35.0/bin/linux/amd64/kube-apiserver
chmod +x kube-apiserver
sudo mv kube-apiserver /usr/local/bin/
```

**macOS (arm64):**
```bash
curl -LO https://dl.k8s.io/v1.35.0/bin/darwin/arm64/kube-apiserver
chmod +x kube-apiserver
sudo mv kube-apiserver /usr/local/bin/
```

Verify installation:
```bash
kube-apiserver --version
```

## Installation

Add k8senv to your project:

```bash
go get github.com/giantswarm/k8senv
```

## First Test

Create a test file with the `integration` build tag:

```go
//go:build integration

package myproject_test

import (
    "context"
    "testing"

    "github.com/giantswarm/k8senv"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/client-go/kubernetes"
)

func TestKubernetesAPI(t *testing.T) {
    ctx := context.Background()

    // Create manager
    mgr := k8senv.NewManager()
    if err := mgr.Initialize(ctx); err != nil {
        t.Fatalf("Failed to initialize: %v", err)
    }
    defer mgr.Shutdown()

    // Acquire an instance from the pool
    inst, err := mgr.Acquire(ctx)
    if err != nil {
        t.Fatalf("Failed to acquire instance: %v", err)
    }
    defer inst.Release(false) // Keep running for potential reuse

    // Get the REST config
    cfg, err := inst.Config()
    if err != nil {
        t.Fatalf("Failed to get config: %v", err)
    }

    // Create a Kubernetes client
    client, err := kubernetes.NewForConfig(cfg)
    if err != nil {
        t.Fatalf("Failed to create client: %v", err)
    }

    // Use the API
    namespaces, err := client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
    if err != nil {
        t.Fatalf("Failed to list namespaces: %v", err)
    }

    t.Logf("Found %d namespaces", len(namespaces.Items))
}
```

## Running the Test

Run with the integration build tag:

```bash
go test -tags=integration -v ./...
```

If binaries are not installed, tests will fail with an informative error message indicating which binary is missing and how to install it.

## What Happens During a Test

1. **Manager creation**: `NewManager()` configures the pool but doesn't start anything
2. **Initialization**: `Initialize()` prepares directories and optional CRD caching
3. **Acquire**: `Acquire()` gets an instance, starting it lazily if needed
4. **Use**: Your test uses the `*rest.Config` to create clients and interact with the API
5. **Release**: `Release(false)` returns the instance to the pool without stopping it
6. **Shutdown**: `Shutdown()` stops all instances and cleans up

## API-Only Mode

k8senv runs only kube-apiserver without the scheduler or controller-manager. This means:

- Pods remain in `Pending` state (no scheduler to place them)
- Controllers don't reconcile (no controller-manager)
- API operations work normally (create, read, update, delete)

This is ideal for testing:
- CustomResourceDefinitions
- RBAC policies
- Admission webhooks
- API validation
- Namespace isolation

## Next Steps

- [Configuration](../reference/configuration.md) - Customize timeouts and more
- [Parallel Testing](../how-to/parallel-testing.md) - Run multiple tests concurrently
- [CRD Testing](../how-to/crd-testing.md) - Pre-apply CRDs with caching
