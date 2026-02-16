# Parallel Testing

k8senv's pooling system enables efficient parallel test execution. This guide covers patterns for running concurrent tests.

## How Pooling Works

The pool uses a bounded design (default: 4 instances):

1. **Bounded capacity**: Up to `DefaultPoolSize` (4) instances are created; `Acquire()` blocks when all are in use. Use `WithPoolSize(0)` for unlimited.
2. **Lazy start**: Instances start kube-apiserver only when first acquired
3. **Instance reuse**: `Release(false)` returns instances for reuse by subsequent `Acquire()` calls

Parallel tests share instances from the pool. When all instances are in use, `Acquire` blocks until one is released. Released instances are reused to minimize startup overhead.

## Basic Parallel Pattern

Use `t.Parallel()` to mark subtests for concurrent execution:

```go
//go:build integration

package myproject_test

import (
    "context"
    "fmt"
    "testing"

    "github.com/giantswarm/k8senv"
    v1 "k8s.io/api/core/v1"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/client-go/kubernetes"
)

func TestParallel(t *testing.T) {
    ctx := context.Background()

    mgr := k8senv.NewManager(
        k8senv.WithAcquireTimeout(2*time.Minute),
    )
    if err := mgr.Initialize(ctx); err != nil {
        t.Fatalf("Initialize failed: %v", err)
    }
    t.Cleanup(func() { mgr.Shutdown() })

    for i := 0; i < 10; i++ {
        t.Run(fmt.Sprintf("test-%d", i), func(t *testing.T) {
            t.Parallel()

            inst, err := mgr.Acquire(context.Background())
            if err != nil {
                t.Fatalf("Acquire failed: %v", err)
            }
            defer inst.Release(false)

            cfg, err := inst.Config()
            if err != nil {
                t.Fatalf("Config failed: %v", err)
            }

            client, err := kubernetes.NewForConfig(cfg)
            if err != nil {
                t.Fatalf("Client creation failed: %v", err)
            }

            // Test logic here...
            _, err = client.CoreV1().Namespaces().List(context.Background(), metav1.ListOptions{})
            if err != nil {
                t.Fatalf("API call failed: %v", err)
            }
        })
    }
}
```

## Namespace Isolation Pattern

When tests share instances, use unique namespaces for isolation:

```go
func TestWithNamespaceIsolation(t *testing.T) {
    ctx := context.Background()

    mgr := k8senv.NewManager()
    if err := mgr.Initialize(ctx); err != nil {
        t.Fatal(err)
    }
    t.Cleanup(func() { mgr.Shutdown() })

    for i := 0; i < 5; i++ {
        t.Run(fmt.Sprintf("test-%d", i), func(t *testing.T) {
            t.Parallel()

            inst, err := mgr.Acquire(context.Background())
            if err != nil {
                t.Fatal(err)
            }
            defer inst.Release(false)

            cfg, err := inst.Config()
            if err != nil {
                t.Fatal(err)
            }

            client, err := kubernetes.NewForConfig(cfg)
            if err != nil {
                t.Fatal(err)
            }

            // Create unique namespace for this test
            nsName := fmt.Sprintf("test-%d-%d", i, time.Now().UnixNano())
            ns := &v1.Namespace{
                ObjectMeta: metav1.ObjectMeta{Name: nsName},
            }
            if _, err := client.CoreV1().Namespaces().Create(
                context.Background(), ns, metav1.CreateOptions{},
            ); err != nil {
                t.Fatal(err)
            }

            // Clean up namespace when done
            t.Cleanup(func() {
                client.CoreV1().Namespaces().Delete(
                    context.Background(), nsName, metav1.DeleteOptions{},
                )
            })

            // Run test in isolated namespace
            cm := &v1.ConfigMap{
                ObjectMeta: metav1.ObjectMeta{
                    Name:      "my-config",
                    Namespace: nsName,
                },
                Data: map[string]string{"key": "value"},
            }
            if _, err := client.CoreV1().ConfigMaps(nsName).Create(
                context.Background(), cm, metav1.CreateOptions{},
            ); err != nil {
                t.Fatal(err)
            }
        })
    }
}
```

## TestMain Pattern for Shared Manager

For large test suites, initialize the manager once in `TestMain`:

```go
//go:build integration

package myproject_test

import (
    "context"
    "os"
    "testing"
    "time"

    "github.com/giantswarm/k8senv"
)

var testManager k8senv.Manager

func TestMain(m *testing.M) {
    ctx := context.Background()

    testManager = k8senv.NewManager(
        k8senv.WithAcquireTimeout(2*time.Minute),
        k8senv.WithCRDDir("testdata/crds"),
    )

    if err := testManager.Initialize(ctx); err != nil {
        // Skip tests if binaries not available
        if os.Getenv("CI") == "" {
            os.Exit(0) // Local development without binaries
        }
        panic(err)
    }

    code := m.Run()

    testManager.Shutdown()
    os.Exit(code)
}

func TestFeatureA(t *testing.T) {
    t.Parallel()

    inst, err := testManager.Acquire(context.Background())
    if err != nil {
        t.Fatal(err)
    }
    defer inst.Release(false)

    // Test logic...
}

func TestFeatureB(t *testing.T) {
    t.Parallel()

    inst, err := testManager.Acquire(context.Background())
    if err != nil {
        t.Fatal(err)
    }
    defer inst.Release(false)

    // Test logic...
}
```

## Timeout Considerations

The `acquireTimeout` covers both pool wait time and instance startup:

```
┌─────────── acquireTimeout ───────────┐
│                                       │
│   ┌── Pool Wait ──┐ ┌── Startup ──┐ │
│   │ Block if all   │ │ Start kine + │ │
│   │ instances busy │ │ apiserver    │ │
│   │ (0s if idle)   │ │ (5-15s)     │ │
│   └────────────────┘ └─────────────┘ │
└───────────────────────────────────────┘
```

The default of 30 seconds is sufficient for most scenarios. Increase if pool contention or slow startup is expected:

```go
k8senv.WithAcquireTimeout(90*time.Second) // Allow extra startup time
```

## Running Tests

Execute parallel tests with Go's `-parallel` flag:

```bash
# Run with default parallelism (GOMAXPROCS)
go test -tags=integration -v ./...

# Run with specific parallelism
go test -tags=integration -v -parallel=10 ./...

# Run with race detector (recommended)
go test -tags=integration -v -parallel=10 -race ./...
```

## Resource Considerations

Each instance consumes:
- ~50-100MB memory (kine + kube-apiserver)
- 2 file descriptors for processes
- ~10-20MB disk for SQLite database

With the default pool size of 4, at most 4 instances run concurrently. Use `Release(false)` to allow instance reuse, keeping resource usage efficient. Adjust `WithPoolSize()` to match your resource budget.

## Complete Example: 10 Parallel Tests

```go
//go:build integration

package myproject_test

import (
    "context"
    "fmt"
    "sync"
    "testing"
    "time"

    "github.com/giantswarm/k8senv"
    v1 "k8s.io/api/core/v1"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/client-go/kubernetes"
)

func TestTenParallel(t *testing.T) {
    ctx := context.Background()

    mgr := k8senv.NewManager(
        k8senv.WithAcquireTimeout(2*time.Minute),
    )
    if err := mgr.Initialize(ctx); err != nil {
        t.Fatal(err)
    }
    t.Cleanup(func() { mgr.Shutdown() })

    // Track instance reuse
    var (
        usage = make(map[string]int)
        mu    sync.Mutex
    )

    for i := 0; i < 10; i++ {
        t.Run(fmt.Sprintf("worker-%d", i), func(t *testing.T) {
            t.Parallel()

            inst, err := mgr.Acquire(context.Background())
            if err != nil {
                t.Fatal(err)
            }
            defer inst.Release(false)

            // Record instance usage
            mu.Lock()
            usage[inst.ID()]++
            mu.Unlock()

            cfg, err := inst.Config()
            if err != nil {
                t.Fatal(err)
            }

            client, err := kubernetes.NewForConfig(cfg)
            if err != nil {
                t.Fatal(err)
            }

            // Create unique namespace
            nsName := fmt.Sprintf("worker-%d-%d", i, time.Now().UnixNano())
            ns := &v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsName}}
            if _, err := client.CoreV1().Namespaces().Create(
                context.Background(), ns, metav1.CreateOptions{},
            ); err != nil {
                t.Fatal(err)
            }

            // Simulate work
            time.Sleep(100 * time.Millisecond)

            // Cleanup
            client.CoreV1().Namespaces().Delete(
                context.Background(), nsName, metav1.DeleteOptions{},
            )
        })
    }

    // Log reuse statistics after all tests complete
    t.Cleanup(func() {
        mu.Lock()
        defer mu.Unlock()
        for id, count := range usage {
            t.Logf("Instance %s used %d times", id, count)
        }
    })
}
```

## Related

- [Configuration](../reference/configuration.md) - Timeouts and all options
- [CRD Testing](crd-testing.md) - Pre-apply CRDs with caching for parallel tests
- [Troubleshooting](../reference/troubleshooting.md) - Debugging timeout issues
- [Architecture Overview](../explanation/architecture-overview.md) - How pooling works internally
