# k8senv

[![Go Reference](https://pkg.go.dev/badge/github.com/giantswarm/k8senv.svg)](https://pkg.go.dev/github.com/giantswarm/k8senv)
[![Go Report Card](https://goreportcard.com/badge/github.com/giantswarm/k8senv)](https://goreportcard.com/report/github.com/giantswarm/k8senv)
[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)
![Go Version](https://img.shields.io/badge/Go-1.25-00ADD8.svg)

Lightweight Kubernetes API testing with kube-apiserver and SQLite — no etcd, no cluster, no YAML.

## What it does

k8senv gives your Go tests a real kube-apiserver backed by [kine](https://github.com/k3s-io/kine) (a SQLite-to-etcd shim). Each instance runs two processes — kine and kube-apiserver — with no scheduler or controller-manager, making it fast to start and ideal for API-level testing.

- **Instance pooling** — a concurrent-safe pool (default 4) with acquire/release semantics
- **Namespace isolation** — parallel tests share instances via separate namespaces
- **Lazy startup** — instances start on first acquire, not at init
- **CRD caching** — hash-based caching so CRDs are applied once and reused
- **SQLite storage** — faster startup than etcd, zero external dependencies beyond the two binaries

### How is this different from envtest?

[envtest](https://pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/envtest) starts a single kube-apiserver with etcd per test suite. k8senv replaces etcd with SQLite (via kine) and adds a pooling layer, so multiple instances can run concurrently with fast startup and release-time cleanup. This makes it a better fit for large parallel test suites where you want isolation without the overhead of etcd.

## Getting started

### 1. Install the binaries

```bash
# kine (etcd-compatible SQLite shim)
go install github.com/k3s-io/kine/cmd/kine@latest

# kube-apiserver (Linux amd64 — see Getting Started guide for other platforms)
curl -Lo kube-apiserver https://dl.k8s.io/v1.35.2/bin/linux/amd64/kube-apiserver
chmod +x kube-apiserver
sudo mv kube-apiserver /usr/local/bin/
```

Verify: `which kine && which kube-apiserver`

### 2. Add the dependency

```bash
go get github.com/giantswarm/k8senv
```

### 3. Write a test

Create a shared manager in `TestMain` so all tests share one pool of instances:

```go
//go:build integration

package myapp_test

import (
    "context"
    "fmt"
    "os"
    "testing"

    "github.com/giantswarm/k8senv"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/client-go/kubernetes"
)

var mgr k8senv.Manager

func TestMain(m *testing.M) {
    mgr = k8senv.NewManager()
    if err := mgr.Initialize(context.Background()); err != nil {
        fmt.Fprintf(os.Stderr, "Initialize failed: %v\n", err)
        os.Exit(1)
    }

    code := m.Run()

    if err := mgr.Shutdown(); err != nil {
        fmt.Fprintf(os.Stderr, "Shutdown error: %v\n", err)
    }
    os.Exit(code)
}

func TestListNamespaces(t *testing.T) {
    t.Parallel()
    ctx := context.Background()

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

Expected output:

```
=== RUN   TestListNamespaces
    myapp_test.go:52: Found 4 namespaces
--- PASS: TestListNamespaces (5.12s)
PASS
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

## License

[Apache License 2.0](LICENSE)
