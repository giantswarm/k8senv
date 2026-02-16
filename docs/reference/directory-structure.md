# Directory Structure

## Project Layout

```mermaid
flowchart TD
    Root["k8senv/"] --> Public["Public API\n(root package)"]
    Root --> Tests["tests/\n+ crd/\n+ poolsize/"]
    Root --> Internal["internal/"]
    Root --> Docs["docs/"]
    Root --> CRDs["crds/"]

    Public --> doc["doc.go"]
    Public --> ifaces["interfaces.go"]
    Public --> factory["k8senv.go"]
    Public --> opts["options.go"]
    Public --> errs["errors.go"]
    Public --> cfg["config.go"]
    Public --> log["log.go"]

    Internal --> Core["core/"]
    Internal --> KubeStack["kubestack/"]
    Internal --> APIServerPkg["apiserver/"]
    Internal --> KinePkg["kine/"]
    Internal --> Process["process/"]
    Internal --> CRDCachePkg["crdcache/"]
    Internal --> NetUtil["netutil/"]
    Internal --> FileUtil["fileutil/"]
    Internal --> Sentinel["sentinel/"]
```

## File Reference

### Public API (root package)

| File | Purpose |
|------|---------|
| `doc.go` | Package documentation with usage examples |
| `interfaces.go` | `Manager` and `Instance` interfaces |
| `k8senv.go` | `NewManager()` factory function, adapter wrappers |
| `options.go` | Functional options: `WithCRDDir`, `WithAcquireTimeout`, etc. |
| `errors.go` | Sentinel error re-exports from internal/core |
| `config.go` | Unexported `managerConfig` struct + conversion to `core.ManagerConfig` |
| `log.go` | `SetLogger()` public logging API |

### internal/core/ — Orchestration

| File | Purpose |
|------|---------|
| `manager.go` | Manager implementation: pool lifecycle, two-phase init, CRD cache setup |
| `pool.go` | Bounded instance pool (default 4), instance tracking |
| `instance.go` | Instance lifecycle: start, stop, release, port conflict retry |
| `config.go` | `ManagerConfig` and `InstanceConfig` structs |
| `log.go` | Package-level slog logger |

### internal/kubestack/ — Process Stack

| File | Purpose |
|------|---------|
| `stack.go` | Coordinates kine + kube-apiserver startup/shutdown sequence |

### internal/apiserver/ — kube-apiserver Process

| File | Purpose |
|------|---------|
| `process.go` | Certificate generation, token auth, kube-apiserver binary management |

### internal/kine/ — kine Process

| File | Purpose |
|------|---------|
| `process.go` | SQLite backend configuration, DB prepopulation, TCP readiness |

### internal/process/ — Base Abstractions

| File | Purpose |
|------|---------|
| `base.go` | `BaseProcess`: embeddable process lifecycle (setup, start, stop) |
| `process.go` | `LogFiles` management, port conflict detection |
| `stoppable.go` | `Stoppable` interface, cleanup helpers |
| `wait.go` | Polling-based readiness checks with configurable intervals |

### internal/crdcache/ — CRD Cache

| File | Purpose |
|------|---------|
| `cache.go` | `EnsureCache`: double-checked locking, cache creation |
| `apply.go` | Dynamic YAML resource application to kube-apiserver |
| `hash.go` | Deterministic SHA256 directory hashing |
| `lock.go` | File-based locking (gofrs/flock) |
| `walk.go` | Recursive YAML file discovery |

### internal/netutil/ — Network Utilities

| File | Purpose |
|------|---------|
| `port.go` | `GetFreePort()`, `GetTwoFreePorts()` for dynamic port allocation |

### internal/fileutil/ — File Utilities

| File | Purpose |
|------|---------|
| `copy.go` | `CopyFile` with chmod and fsync |
| `dir.go` | `EnsureDir`, `EnsureDirForFile` |

### internal/sentinel/ — Sentinel Error Type

| File | Purpose |
|------|---------|
| `sentinel.go` | `const`-compatible error type for sentinel errors |

### tests/ — Integration Tests (package `k8senv_test`)

| File | Purpose |
|------|---------|
| `main_test.go` | `TestMain` with shared singleton manager setup |
| `lifecycle_test.go` | Initialize idempotency and concurrency tests |
| `pool_test.go` | Pool acquire/release semantics and concurrent access |
| `instance_test.go` | Instance usage, reuse, ID uniqueness, port retry, API server mode |
| `cleanup_test.go` | Namespace cleanup on release, system namespace preservation |
| `options_test.go` | Option validation (panics on invalid input) |
| `coverage_test.go` | Coverage expansion: logger, context cancel, usable instance |
| `stress_test.go` | High-concurrency stress test |
| `helpers_test.go` | Shared test helper functions |

### tests/poolsize/ — Pool Size Tests (package `k8senv_poolsize_test`)

| File | Purpose |
|------|---------|
| `main_test.go` | `TestMain` with custom pool size singleton |
| `poolsize_test.go` | Pool timeout, release unblocking, bounded instance reuse |

### tests/crd/ — CRD Tests (package `k8senv_crd_test`)

| File | Purpose |
|------|---------|
| `main_test.go` | `TestMain` with CRD-enabled singleton |
| `crd_test.go` | CRD caching, multi-CRD, multi-doc YAML, .yml extension |
| `helpers_test.go` | CRD test helper functions |

### Runtime Data Directory

Instance data is created at runtime under the base data directory (default `/tmp/k8senv/`):

```
/tmp/k8senv/
├── cached-abc123.db              # CRD cache (persistent across runs)
├── inst-a1b2c3d4/               # Instance data directory
│   ├── kine.db                  # SQLite database
│   ├── kine-stdout.log          # kine stdout
│   ├── kine-stderr.log          # kine stderr
│   ├── kube-apiserver-stdout.log
│   ├── kube-apiserver-stderr.log
│   ├── apiserver.crt            # Self-signed TLS certificate
│   ├── apiserver.key            # TLS private key
│   └── token-auth               # Token authentication file
```

## Related

- [Architecture Overview](../explanation/architecture-overview.md) - Layer architecture and component descriptions
- [Data Flow](data-flow.md) - How requests flow through the system
- [CODEBASE_MAP.md](../CODEBASE_MAP.md) - Auto-generated detailed module guide
