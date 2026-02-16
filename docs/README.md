# k8senv Documentation

k8senv is a lightweight Kubernetes testing framework that manages kube-apiserver instances backed by [kine](https://github.com/k3s-io/kine) (an etcd-compatible SQLite shim). It provides on-demand instance creation with lazy initialization, allowing parallel tests to share instances while maintaining isolation through namespaces.

## Key Benefits

- **Fast startup**: SQLite-backed storage eliminates etcd complexity
- **Instance pooling**: Reuse kube-apiserver instances across tests
- **Parallel-ready**: Run many tests with fewer instances
- **CRD caching**: Pre-apply CRDs once, share across all instances
- **Minimal footprint**: Runs only kube-apiserver (no scheduler or controller-manager)

## Documentation

### Tutorials

| Document | Description |
|----------|-------------|
| [Getting Started](tutorials/getting-started.md) | Installation, prerequisites, first test |

### How-To Guides

| Document | Description |
|----------|-------------|
| [Parallel Testing](how-to/parallel-testing.md) | Patterns for running tests concurrently |
| [CRD Testing](how-to/crd-testing.md) | Working with CustomResourceDefinitions |

### Reference

| Document | Description |
|----------|-------------|
| [Configuration](reference/configuration.md) | All options with defaults and examples |
| [Troubleshooting](reference/troubleshooting.md) | Common issues and debugging |
| [Directory Structure](reference/directory-structure.md) | File organization and purposes |
| [Data Flow](reference/data-flow.md) | Request lifecycle and CRD caching sequence |

### Explanation

| Document | Description |
|----------|-------------|
| [Architecture Overview](explanation/architecture-overview.md) | Design principles, components, technology choices |

## Quick Example

```go
import "github.com/giantswarm/k8senv"

func TestExample(t *testing.T) {
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
    defer inst.Release(false)

    cfg, err := inst.Config()
    if err != nil {
        t.Fatal(err)
    }

    client, err := kubernetes.NewForConfig(cfg)
    // Use client...
}
```

## Comparison with envtest

| Feature | k8senv | envtest |
|---------|--------|---------|
| Storage backend | SQLite (via kine) | etcd |
| Instance pooling | Built-in | Not available |
| CRD caching | Hash-based automatic | Manual |
| Parallel test support | Native with on-demand instances | Requires manual coordination |
| Components | kube-apiserver only | kube-apiserver + etcd |
