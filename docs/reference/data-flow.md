# Data Flow

This document describes how requests flow through k8senv during test execution and CRD cache creation.

## Test Request Lifecycle

```mermaid
sequenceDiagram
    participant Test
    participant Manager
    participant Pool
    participant Instance
    participant Stack as KubeStack
    participant Kine
    participant APIServer as kube-apiserver

    Test->>Manager: Acquire(ctx)
    Manager->>Pool: Acquire(ctx)
    Note over Pool: Creates new instance<br/>or reuses released one

    Pool-->>Manager: Instance
    Manager->>Instance: Start(ctx) [if not started]

    Instance->>Stack: Start(processCtx, readyCtx)
    Stack->>Kine: Start(ctx)
    Note over Kine: Launches kine binary<br/>with SQLite backend
    Stack->>Kine: WaitReady(ctx, timeout)
    Note over Kine: TCP probe on<br/>kine port

    Stack->>APIServer: Start(ctx)
    Note over APIServer: Generates certs,<br/>token auth file,<br/>launches binary
    Stack->>APIServer: WaitReady(ctx, timeout)
    Note over APIServer: HTTP GET /livez

    Instance-->>Manager: Ready
    Manager-->>Test: Instance

    Test->>Instance: Config()
    Instance-->>Test: *rest.Config

    Note over Test: Create client,<br/>run test logic

    Test->>Instance: Release()
    Note over Instance: Strategy from config

    alt ReleaseRestart (default)
        Instance->>Instance: Stop()
        Instance->>Pool: Return (stopped)
    else ReleaseClean
        Instance->>Instance: cleanNamespaces()
        Instance->>Pool: Return (running)
    else ReleaseNone
        Instance->>Pool: Return (as-is)
    end
```

## Instance Startup Sequence

Each instance starts two coordinated processes:

1. **kine** starts first — provides etcd-compatible API backed by SQLite on a dynamic port
2. **TCP readiness probe** — polls kine's port until it accepts connections
3. **kube-apiserver** starts second — connects to kine as its etcd backend
4. **HTTP health check** — polls `/livez` endpoint until the API server is alive

If either process fails to start (e.g., port conflict), the instance retries with new ports (up to 5 attempts by default).

## CRD Cache Creation

```mermaid
sequenceDiagram
    participant Manager
    participant CRDCache as crdcache
    participant Lock as File Lock
    participant TempStack as Temporary Stack
    participant Kine as Temp kine
    participant APIServer as Temp kube-apiserver

    Manager->>CRDCache: EnsureCache(ctx, cfg)
    CRDCache->>CRDCache: computeDirHash(crdDir)
    Note over CRDCache: SHA256 of all YAML<br/>file contents + names

    CRDCache->>CRDCache: Check cached-{hash}.db
    alt Cache exists
        CRDCache-->>Manager: Result{CachePath, Created: false}
    else Cache miss
        CRDCache->>Lock: Acquire file lock
        CRDCache->>CRDCache: Double-check cache
        CRDCache->>TempStack: Start temporary stack
        TempStack->>Kine: Start
        TempStack->>APIServer: Start

        CRDCache->>CRDCache: Apply CRD YAML files
        Note over CRDCache: Wait for CRDs to<br/>reach Established

        CRDCache->>TempStack: Stop
        CRDCache->>CRDCache: Save kine.db as cached-{hash}.db
        CRDCache->>Lock: Release file lock
        CRDCache-->>Manager: Result{CachePath, Created: true}
    end

    Note over Manager: Instances copy<br/>cached DB on startup
```

## Pool Mechanics

```mermaid
stateDiagram-v2
    [*] --> Created: NewInstance()
    Created --> Started: First Acquire() triggers Start()
    Started --> Busy: Acquire() — token issued
    Busy --> Free: Release() — ReleaseToPool (strategy applied)
    Free --> Busy: Acquire()
    Busy --> Failed: ReleaseFailed() — error during release
    Started --> Failed: Start error
    Failed --> [*]: Removed from pool

    note right of Busy
        Strategy determines cleanup:
        Restart → Stop()
        Clean → cleanNamespaces()
        None → no-op
    end note
```

The pool manages instances with a bounded capacity (default: 4):

- **Acquire**: Returns a previously released instance if available, or creates a new one (up to the pool size limit). Blocks if all instances are in use. Returns a token for double-release detection.
- **Release()**: Behavior depends on the manager's `ReleaseStrategy`:
  - `ReleaseRestart` (default) — Stops instance. Next acquire starts fresh.
  - `ReleaseClean` — Deletes non-system namespaces, keeps running.
  - `ReleaseNone` — No cleanup, returns as-is.
- **Failed**: Instance release or startup failed; instance is removed from the pool.

## Related

- [Architecture Overview](../explanation/architecture-overview.md) - Design principles and component descriptions
- [Directory Structure](directory-structure.md) - File organization and purposes
- [CRD Testing](../how-to/crd-testing.md) - Using the CRD cache in tests
