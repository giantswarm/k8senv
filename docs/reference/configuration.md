# Configuration

k8senv uses functional options for configuration. This guide covers all available options with their defaults and usage examples.

## Manager Options

Manager options are passed to `NewManager()`:

```go
mgr := k8senv.NewManager(
    k8senv.WithAcquireTimeout(60*time.Second),
    // ... more options
)
```

### Reference Table

| Option | Default | Description |
|--------|---------|-------------|
| `WithPoolSize(size)` | 4 | Max instances in pool; 0 = unlimited. Acquire blocks when all in use |
| `WithReleaseStrategy(s)` | `ReleaseRestart` | Strategy for `Release()`: `ReleaseRestart`, `ReleaseClean`, `ReleasePurge`, or `ReleaseNone` |
| `WithCleanupTimeout(d)` | 30s | Timeout for namespace cleanup during release (used with `ReleaseClean` and `ReleasePurge`) |
| `WithAcquireTimeout(d)` | 30s | Timeout for instance startup during Acquire |
| `WithKineBinary(path)` | `"kine"` | Path to kine binary |
| `WithKubeAPIServerBinary(path)` | `"kube-apiserver"` | Path to kube-apiserver binary |
| `WithBaseDataDir(dir)` | `"/tmp/k8senv"` | Base directory for instance data |
| `WithCRDDir(dir)` | (none) | Directory with CRD YAML files to pre-apply |
| `WithPrepopulateDB(path)` | (none) | SQLite database file to copy for each instance |
| `WithCRDCacheTimeout(d)` | 5m | Timeout for CRD cache creation (spin up temp stack, apply CRDs, copy DB) |
| `WithInstanceStartTimeout(d)` | 5m | Max time for kine + kube-apiserver to start and become ready |
| `WithInstanceStopTimeout(d)` | 10s | Max time per-process for graceful shutdown |
| `WithShutdownDrainTimeout(d)` | 30s | Max time `Shutdown()` waits for in-flight releases to complete |

### Option Details

#### WithReleaseStrategy

Configures the behavior of `Instance.Release()` for all instances managed by this manager:

- **`ReleaseRestart`** (default) — Stops the instance on release. Next `Acquire()` starts a fresh instance with the DB restored from template. Provides full isolation between tests.
- **`ReleaseClean`** — Deletes non-system namespaces via the Kubernetes API but keeps the instance running. Faster reuse at the cost of shared state in system namespaces.
- **`ReleasePurge`** — Deletes non-system namespaces via direct SQLite queries, bypassing the Kubernetes API entirely. Fastest cleanup strategy; keeps the instance running. Bypasses finalizers.
- **`ReleaseNone`** — No cleanup. Returns the instance as-is. Tests must manage their own isolation.

```go
k8senv.WithReleaseStrategy(k8senv.ReleaseClean)   // Keep running, clean namespaces via API
k8senv.WithReleaseStrategy(k8senv.ReleasePurge)    // Keep running, clean namespaces via SQL
k8senv.WithReleaseStrategy(k8senv.ReleaseNone)     // No cleanup
```

Panics if strategy is invalid.

#### WithCleanupTimeout

Sets the timeout for namespace cleanup during release. Used when the release strategy is `ReleaseClean` or `ReleasePurge`.

```go
k8senv.WithCleanupTimeout(60*time.Second)
```

Panics if duration <= 0.

#### WithPoolSize

Sets the maximum number of instances the pool will create. A positive value caps the pool; `Acquire()` blocks when all instances are in use. A value of 0 means unlimited: instances are created on demand without an upper bound.

```go
k8senv.WithPoolSize(8)  // Allow up to 8 concurrent instances
k8senv.WithPoolSize(0)  // Unlimited instances
```

Panics if size < 0.

#### WithAcquireTimeout

Sets the timeout for `Acquire()`, covering instance startup time.

```go
k8senv.WithAcquireTimeout(90*time.Second)
```

Instance startup typically takes 5-15 seconds. The default of 30 seconds is sufficient for most scenarios.

Panics if duration ≤ 0.

#### WithKineBinary / WithKubeAPIServerBinary

Specify explicit paths to binaries instead of relying on `$PATH`.

```go
k8senv.WithKineBinary("/usr/local/bin/kine"),
k8senv.WithKubeAPIServerBinary("/opt/k8s/kube-apiserver"),
```

#### WithBaseDataDir

Sets the base directory for instance data (databases, logs, certificates).

```go
k8senv.WithBaseDataDir("/tmp/myproject-k8senv")
```

Useful in CI environments where multiple projects may use k8senv simultaneously.

#### WithCRDDir

Directory containing CRD YAML files to pre-apply. See [CRD Testing](../how-to/crd-testing.md) for details.

```go
k8senv.WithCRDDir("testdata/crds")
```

#### WithPrepopulateDB

Path to a SQLite database file to copy as the starting point for each instance.

```go
k8senv.WithPrepopulateDB("testdata/base.db")
```

#### WithCRDCacheTimeout

Sets the overall timeout for CRD cache creation, including spinning up a temporary kine + kube-apiserver, applying CRDs, and copying the resulting database.

```go
k8senv.WithCRDCacheTimeout(10*time.Minute)
```

Panics if duration <= 0.

#### WithInstanceStartTimeout

Sets the maximum time for an instance's kine + kube-apiserver processes to start and become ready.

```go
k8senv.WithInstanceStartTimeout(3*time.Minute)
```

Panics if duration <= 0.

#### WithInstanceStopTimeout

Sets the maximum time per-process for graceful shutdown. Since kube-apiserver and kine are stopped sequentially, the worst-case total stop time is 2x this value.

```go
k8senv.WithInstanceStopTimeout(30*time.Second)
```

Panics if duration <= 0.

#### WithShutdownDrainTimeout

Sets the maximum time `Shutdown()` waits for in-flight `Release()` calls to complete before forcibly closing the pool. This prevents shutdown from hanging if a release is stuck.

```go
k8senv.WithShutdownDrainTimeout(2*time.Minute)
```

Panics if duration <= 0.

## Instance Internals

Instances use internal defaults that are not configurable through the public API:

| Setting | Default | Description |
|---------|---------|-------------|
| Start retries | 5 | Retry attempts on port conflicts |

Start and stop timeouts are configurable via `WithInstanceStartTimeout` and `WithInstanceStopTimeout` manager options (see above).

## Logging Configuration

### SetLogger

Replace the package-level logger to integrate with your application's logging:

```go
import (
    "log/slog"
    "github.com/giantswarm/k8senv"
)

func init() {
    myLogger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
        Level: slog.LevelDebug,
    }))
    k8senv.SetLogger(myLogger.With("component", "k8senv"))
}
```

Call `SetLogger` before creating any Manager instances. Pass `nil` to reset to default.

### K8SENV_LOG_LEVEL (Tests Only)

During test runs, control verbosity with the `K8SENV_LOG_LEVEL` environment variable:

```bash
# Available levels: DEBUG, INFO, WARN, ERROR
K8SENV_LOG_LEVEL=DEBUG go test -tags=integration -v ./...
```

This environment variable only affects logging during tests, not when the library is used by applications.

## Full Configuration Example

```go
package myproject_test

import (
    "context"
    "log/slog"
    "os"
    "testing"
    "time"

    "github.com/giantswarm/k8senv"
)

func TestMain(m *testing.M) {
    // Configure logging
    logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
        Level: slog.LevelDebug,
    }))
    k8senv.SetLogger(logger.With("component", "k8senv"))

    os.Exit(m.Run())
}

func TestWithFullConfig(t *testing.T) {
    ctx := context.Background()

    mgr := k8senv.NewManager(
        // Pool configuration
        k8senv.WithPoolSize(4),                                    // Default: 4 (0 = unlimited)
        k8senv.WithReleaseStrategy(k8senv.ReleaseClean),          // Default: ReleaseRestart

        // Timeout configuration
        k8senv.WithAcquireTimeout(90*time.Second),
        k8senv.WithCleanupTimeout(60*time.Second),                // Default: 30s (ReleaseClean only)
        k8senv.WithInstanceStartTimeout(3*time.Minute),
        k8senv.WithInstanceStopTimeout(30*time.Second),
        k8senv.WithShutdownDrainTimeout(2*time.Minute),         // Default: 30s

        // Binary paths (optional if in $PATH)
        k8senv.WithKineBinary("/usr/local/bin/kine"),
        k8senv.WithKubeAPIServerBinary("/usr/local/bin/kube-apiserver"),

        // Data directory (useful for CI isolation)
        k8senv.WithBaseDataDir("/tmp/myproject-k8senv"),

        // CRD pre-loading
        k8senv.WithCRDDir("testdata/crds"),
        k8senv.WithCRDCacheTimeout(10*time.Minute),
    )

    if err := mgr.Initialize(ctx); err != nil {
        t.Fatalf("Failed to initialize: %v", err)
    }
    defer mgr.Shutdown()

    inst, err := mgr.Acquire(ctx)
    if err != nil {
        t.Fatalf("Failed to acquire: %v", err)
    }
    defer inst.Release()

    // Use instance...
}
```
