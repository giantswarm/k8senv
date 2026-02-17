# k8senv

Lightweight Kubernetes API testing with kube-apiserver and SQLite — no etcd, no cluster, no YAML.

## What it does

k8senv gives your Go tests a real kube-apiserver backed by [kine](https://github.com/k3s-io/kine) (a SQLite-to-etcd shim). Each instance runs two processes — kine and kube-apiserver — with no scheduler or controller-manager, making it fast to start and ideal for API-level testing.

- **Instance pooling** — a concurrent-safe pool (default 4) with acquire/release semantics
- **Namespace isolation** — parallel tests share instances via separate namespaces
- **Lazy startup** — instances start on first acquire, not at init
- **CRD caching** — hash-based caching so CRDs are applied once and reused
- **SQLite storage** — faster startup than etcd, zero external dependencies beyond the two binaries

## Getting started

### 1. Install the binaries

```bash
# kine (etcd-compatible SQLite shim)
go install github.com/k3s-io/kine/cmd/kine@latest

# kube-apiserver (Linux amd64)
curl -Lo kube-apiserver https://dl.k8s.io/v1.35.0/bin/linux/amd64/kube-apiserver
chmod +x kube-apiserver
sudo mv kube-apiserver /usr/local/bin/
```

Verify: `which kine && which kube-apiserver`

### 2. Add the dependency

```bash
go get github.com/giantswarm/k8senv
```

### 3. Write a test

```go
//go:build integration

package myapp_test

import (
    "context"
    "testing"

    "github.com/giantswarm/k8senv"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/client-go/kubernetes"
)

func TestMyApp(t *testing.T) {
    ctx := context.Background()

    mgr := k8senv.NewManager()
    if err := mgr.Initialize(ctx); err != nil {
        t.Fatal(err)
    }
    defer mgr.Shutdown()

    inst, err := mgr.Acquire(ctx)
    if err != nil {
        t.Fatal(err)
    }
    defer inst.Release()

    cfg, err := inst.Config()
    if err != nil {
        t.Fatal(err)
    }

    client, err := kubernetes.NewForConfig(cfg)
    if err != nil {
        t.Fatal(err)
    }

    namespaces, err := client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
    if err != nil {
        t.Fatal(err)
    }

    t.Logf("Found %d namespaces", len(namespaces.Items))
}
```

### 4. Run it

```bash
go test -tags=integration -v ./...
```

## Documentation

| Guide | Description |
|-------|-------------|
| [Getting Started](docs/tutorials/getting-started.md) | Full tutorial with prerequisites and first test |
| [Configuration](docs/reference/configuration.md) | All manager options with defaults and examples |
| [Parallel Testing](docs/how-to/parallel-testing.md) | Patterns for concurrent test execution |
| [CRD Testing](docs/how-to/crd-testing.md) | Working with CustomResourceDefinitions |
| [Architecture](docs/explanation/architecture-overview.md) | Design principles, components, data flow |
| [Troubleshooting](docs/reference/troubleshooting.md) | Common issues and debugging |
