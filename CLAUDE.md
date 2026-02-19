# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Overview

k8senv is a lightweight Kubernetes testing framework powered by kube-apiserver and kine, similar to `envtest` but with on-demand instance creation and SQLite backing for efficient parallel test execution. The library manages kube-apiserver instances backed by kine (an etcd-compatible SQLite shim) with on-demand creation, allowing parallel tests to share instances while maintaining isolation through namespaces.

**Stack**: Go 1.25, Kubernetes client-go v0.35.1, kine (SQLite etcd shim), kube-apiserver
**Structure**: Public API (root) → `internal/core` (manager/pool/instance) → `internal/kubestack` (process stack) → `internal/process` (base abstractions)

For detailed architecture, module guide, and navigation, see [docs/CODEBASE_MAP.md](docs/CODEBASE_MAP.md).

## Build, Test & Development Commands

### Basic Commands
```bash
# Build the library
make build

# Format code
make fmt
```

### Integration Tests
Integration tests require kine and kube-apiserver binaries to be installed. They use the `//go:build integration` tag.

```bash
# Run integration tests
make test-integration

# Run integration tests with race detector
make test-integration RACE=1

# Run integration tests without cache
make test-integration NOCACHE=1

# Run a single integration test by name
make test-integration TEST=TestBasicUsage

# Run all tests
make test
```

**Prerequisites for integration tests:**
- kine binary installed and in `$PATH`
- kube-apiserver binary installed and in `$PATH`
- Install kine: `go install github.com/k3s-io/kine/cmd/kine@latest`
- Install kube-apiserver: Download from https://dl.k8s.io/v1.35.0/bin/linux/amd64/kube-apiserver (or use `make install-tools`)

### Running a Single Test
```bash
# Run a specific test by name
make test-integration TEST=TestBasicUsage

# Combine options
make test-integration RACE=1 NOCACHE=1 TEST=TestBasicUsage
```

### Configuring Log Level (Tests Only)
Tests can be run with configurable verbosity via the `K8SENV_LOG_LEVEL` environment variable.
This does NOT affect the library when used by applications - it only configures logging during test runs.

```bash
# Default (INFO) - minimal output
make test-integration

# Debug mode - verbose output for troubleshooting
make test-integration LOG_LEVEL=DEBUG

# Available levels: DEBUG, INFO, WARN, ERROR
```

**Note:** The library itself inherits logging configuration from the application's `slog.Default()`.
Use `k8senv.SetLogger()` to customize logging in your application.

### Other Useful Commands
```bash
# Run unit tests only (no binaries required)
make test-unit

# Generate coverage report
make coverage
go tool cover -html=coverage.out

# Clean temp directories and build artifacts
make clean

# Install all required tools (kine, kube-apiserver, golangci-lint, pkgsite)
make install-tools

# Serve documentation locally
make docs

# Run integration tests in a loop to find flaky tests
make find-flaky

# Download and tidy dependencies
make deps
```

### Linting

The project uses golangci-lint with 55 enabled linters organized across 11 tiers for comprehensive code quality.

```bash
# Run all enabled linters
make lint

# Run linters with auto-fix (fixes issues where possible)
make lint-fix
```

**Linter tiers (see `.golangci.yml` for full configuration):**

| Tier | Focus | Count | Examples |
|------|-------|-------|----------|
| Core | Original linters | 12 | `staticcheck`, `errcheck`, `gosec`, `revive`, `gocritic` |
| 1 | Critical safety | 5 | `govet`, `errorlint`, `wrapcheck`, `testifylint`, `thelper` |
| 2 | High value | 4 | `unparam`, `exhaustive`, `forbidigo`, `nilerr` |
| 3 | Beneficial | 4 | `contextcheck`, `bodyclose`, `gomoddirectives`, `prealloc` |
| 4 | Core safety | 7 | `copyloopvar`, `nilnil`, `bidichk`, `asciicheck` |
| 5 | Performance | 2 | `perfsprint`, `gocheckcompilerdirectives` |
| 6 | Modern Go | 4 | `modernize`, `intrange`, `exptostd`, `usetesting` |
| 7 | Code quality | 4 | `fatcontext`, `nolintlint`, `sloglint`, `loggercheck` |
| 8 | High value adds | 5 | `containedctx`, `ireturn`, `forcetypeassert`, `errname` |
| 9 | Medium value | 4 | `musttag`, `testableexamples`, `nilnesserr`, `recvcheck` |
| 10 | Stylistic | 4 | `mirror`, `tagalign`, `importas`, `inamedparam` |

**Complexity thresholds:** cyclomatic: 15, cognitive: 20

**Handling linter issues:**
1. Run `make lint` to see all issues
2. Run `make lint-fix` to auto-fix simple issues (formatting, imports)
3. For issues that can't be auto-fixed:
   - Refactor code to address the issue
   - Add `//nolint:<linter>` comment with justification if the issue is a false positive
4. Update `.golangci.yml` if you need to adjust thresholds or add exclusions

**Security exceptions:**
The codebase includes justified `//nolint:gosec` comments for:
- Subprocess execution (G204) - binary paths are from config, not user input
- File operations (G304) - paths are from controlled sources
- TLS verification (G402) - testing framework uses ephemeral self-signed certs
- File permissions (G306, G301) - standard permissions for test environments
- Weak random number (G404) - instance IDs and test PRNG need uniqueness, not cryptographic strength

## Architecture

The library follows a clean layered architecture:
- **Public API** (root package) → `internal/core` (manager/pool/instance) → `internal/kubestack` (process stack) → `internal/process` (base abstractions)
- Each instance runs two coordinated processes: kine (SQLite etcd shim) → kube-apiserver
- Pool caps at 4 instances by default (configurable via WithPoolSize); Acquire blocks when all are in use
- Instances are started when first acquired and returned for reuse on release
- API-only mode: no scheduler or controller-manager

For detailed architecture documentation with Mermaid diagrams, see:
- [Architecture Overview](docs/explanation/architecture-overview.md) - Design principles, components, technology choices
- [Data Flow](docs/reference/data-flow.md) - Request lifecycle, CRD caching sequence, pool state machine
- [Directory Structure](docs/reference/directory-structure.md) - Complete file reference with purposes
- [CODEBASE_MAP.md](docs/CODEBASE_MAP.md) - Auto-generated module guide with navigation

**Import path:** `github.com/giantswarm/k8senv`

## Working with Tests

### Testing Philosophy

**All tests MUST be integration tests that exercise only the public API.**

This means:
- Tests use only the public `Manager` and `Instance` interfaces
- Tests do NOT access internal packages or unexported types
- Tests require kine and kube-apiserver binaries
- Tests verify actual behavior through real Kubernetes API calls

Benefits:
- Internal code can be refactored freely without updating tests
- Tests verify actual behavior, not mocked implementations
- No mock interfaces or test helpers that expose internals

### Tests

Tests are organized across the root package (unit tests) and nine integration test packages under `tests/`. Integration tests share a singleton manager created in `TestMain` and require kine and kube-apiserver binaries. Different test packages use different `ReleaseStrategy` configurations to test specific behaviors.

**Root package (`options_test.go`)** — Unit tests (no binaries required):
- `TestWithAcquireTimeoutPanicsOnInvalid` - Panics on invalid timeout
- `TestWithKineBinaryPanicsOnEmpty` - Panics on empty kine binary path
- `TestWithPoolSizePanicsOnInvalid` - Panics on negative pool size
- `TestWithKubeAPIServerBinaryPanicsOnEmpty` - Panics on empty kube-apiserver binary path
- `TestWithReleaseStrategyPanicsOnInvalid` - Panics on invalid release strategy
- `TestWithEmptyStringOptionsPanic` - Empty string options panic
- `TestWithCleanupTimeoutPanicsOnInvalid` - Panics on invalid cleanup timeout
- `TestWithShutdownDrainTimeoutPanicsOnInvalid` - Panics on invalid shutdown drain timeout
- `TestWithInstanceStartTimeoutPanicsOnInvalid` - Panics on invalid instance start timeout
- `TestWithInstanceStopTimeoutPanicsOnInvalid` - Panics on invalid instance stop timeout
- `TestWithCRDCacheTimeoutPanicsOnInvalid` - Panics on invalid CRD cache timeout
- `TestOptionApplicationDefaults` - Default config values are correct
- `TestOptionApplicationOverrides` - Options override defaults
- `TestOptionApplicationMultipleOptions` - Multiple options compose correctly
- `TestOptionApplicationLastWriteWins` - Last option wins on conflict

**`tests/` (package `k8senv_test`)** — Core integration tests:

**Lifecycle & initialization:**
- `TestInitializeIdempotent` - Safe to call Initialize multiple times
- `TestInitializeConcurrent` - Concurrent Initialize calls are safe

**Pool behavior:**
- `TestPoolAcquireRelease` - Basic acquire/release semantics
- `TestPoolConcurrentAccess` - Concurrent safety with race detector
- `TestParallelAcquisition` - 10 parallel tests acquiring concurrently

**Instance behavior:**
- `TestBasicUsage` - Simple acquire, use, release
- `TestInstanceReuse` - Explicit reuse demonstration
- `TestIDUniqueness` - Instance IDs are unique
- `TestDoubleReleaseReturnsError` - Double release returns ErrDoubleRelease

**API server mode:**
- `TestAPIServerOnlyMode` - API server testing (namespaces, ConfigMaps)
- `TestMultipleInstancesWithAPIOnly` - Multiple kube-apiserver instances simultaneously

**Other:**
- `TestContextCancelDuringAcquire` - Context cancellation during acquire

**`tests/poolsize/` (package `k8senv_poolsize_test`)** — Bounded pool tests (separate package with pool size 2):
- `TestPoolTimeout` - Acquire blocks and times out when bounded pool is exhausted
- `TestPoolReleaseUnblocks` - Releasing an instance unblocks a waiting Acquire
- `TestPoolBoundedInstanceReuse` - Bounded pool reuses instances, never exceeds max

**`tests/cleanup/` (package `k8senv_cleanup_test`)** — Namespace cleanup tests (separate package with `ReleaseClean` strategy):
- `TestSystemNamespacesMatchAPIServer` - Verifies local system namespace set matches API server
- `TestReleaseCleanupNamespaces` - Release() with ReleaseClean removes user namespaces before pool return
- `TestReleasePreservesSystemNamespaces` - System namespaces survive cleanup
- `TestReleaseCleanupWithNoUserNamespaces` - Fast path when no user namespaces exist

**`tests/stress/` (package `k8senv_stress_test`)** — Stress tests (separate package, run sequentially after other tests):
- `TestStress` - Spawns parallel subtests that verify instance cleanliness and create random resources (configurable via `K8SENV_STRESS_SUBTESTS`)

**`tests/purge/` (package `k8senv_purge_test`)** — SQLite purge tests (separate package with `ReleasePurge` strategy):
- `TestReleasePurgeNamespaces` - Release() with ReleasePurge removes user namespaces via direct SQL
- `TestReleasePurgePreservesSystemNamespaces` - System namespaces survive purge
- `TestReleasePurgeWithNoUserNamespaces` - Fast path when no user namespaces exist
- `TestReleasePurgeNamespacedResources` - ConfigMaps/Secrets in user namespaces are deleted
- `TestReleasePurgeResourcesWithFinalizers` - Finalized resources are purged (SQL bypasses finalizers)
- `TestReleasePurgePreservesSystemNamespaceResources` - Resources in kube-system survive

**`tests/stressclean/` (package `k8senv_stressclean_test`)** — Stress tests with `ReleaseClean` strategy (separate package, run sequentially after other tests):
- `TestStressClean` - Stress test verifying ReleaseClean under high concurrency (configurable via `K8SENV_STRESS_SUBTESTS`)

**`tests/stresspurge/` (package `k8senv_stresspurge_test`)** — Stress tests with `ReleasePurge` strategy (separate package, run sequentially after other tests):
- `TestStressPurge` - Stress test verifying ReleasePurge under high concurrency (configurable via `K8SENV_STRESS_SUBTESTS`)

**`tests/restart/` (package `k8senv_restart_test`)** — Restart strategy tests (separate package with default `ReleaseRestart` strategy):
- `TestReleaseRestart` - Release() stops instance, next Acquire starts fresh

**`tests/crd/` (package `k8senv_crd_test`)** — CRD tests (separate package for CRD-enabled singleton):
- `TestCRDDirCaching` - CRD directory caching behavior
- `TestCRDDirWithMultipleCRDs` - Multiple CRD files
- `TestCRDDirWithEstablishedCondition` - Cached CRD is established on acquired instance
- `TestCRDDirWithMultiDocumentYAML` - Multi-document YAML processing
- `TestCRDDirWithYmlExtension` - .yml extension file support

Integration tests fail if kine or kube-apiserver binaries are not available (ensuring missing binaries are never silently ignored in CI).

## Configuration Options

### Manager Options
`NewManager` is a **process-level singleton** — the first call creates the manager with the provided options; subsequent calls return the same instance and log a warning (options are ignored). This ensures a single pool of instances is shared across the entire process.

Applied when creating a manager:
```go
mgr := k8senv.NewManager(
    k8senv.WithPoolSize(2),                                  // Default: 4 (0 = unlimited)
    k8senv.WithReleaseStrategy(k8senv.ReleaseClean),        // Default: ReleaseRestart (also: ReleasePurge, ReleaseNone)
    k8senv.WithKineBinary("/usr/local/bin/kine"),           // Default: "kine"
    k8senv.WithKubeAPIServerBinary("/usr/local/bin/kube-apiserver"), // Default: "kube-apiserver"
    k8senv.WithAcquireTimeout(2*time.Minute),               // Default: 30 seconds
    k8senv.WithCRDDir("testdata/crds"),                     // Optional CRD pre-loading
    k8senv.WithPrepopulateDB("testdata/crds.db"),           // Optional prepopulation
    k8senv.WithBaseDataDir("/tmp/myproject-k8senv"),        // Default: "/tmp/k8senv" - useful for CI isolation
    k8senv.WithCRDCacheTimeout(10*time.Minute),             // Default: 5 minutes
    k8senv.WithInstanceStartTimeout(3*time.Minute),         // Default: 5 minutes
    k8senv.WithInstanceStopTimeout(30*time.Second),         // Default: 10 seconds
    k8senv.WithShutdownDrainTimeout(2*time.Minute),        // Default: 30 seconds
)
```

**Note on acquireTimeout:** This timeout covers both pool wait time (when all instances are in use) and instance startup. Startup typically takes 5-15 seconds. With a bounded pool (default: 4 instances), Acquire may also block waiting for a free instance. Increase this timeout if pool contention is expected.

### Instance Configuration
Instances use internal defaults (start timeout: 5 min, stop timeout: 10s, max retries: 5). Use `WithInstanceStartTimeout` and `WithInstanceStopTimeout` manager options to override the start and stop timeouts.

## Common Patterns

### Basic Usage (Singleton Manager in TestMain)
The recommended pattern creates the singleton manager once in `TestMain` and shares it across all tests:
```go
import "github.com/giantswarm/k8senv"

var sharedManager k8senv.Manager

func TestMain(m *testing.M) {
    mgr := k8senv.NewManager() // Singleton: first call creates, subsequent calls return same instance
    ctx := context.Background()
    if err := mgr.Initialize(ctx); err != nil {
        fmt.Fprintf(os.Stderr, "Initialize failed: %v\n", err)
        os.Exit(1)
    }
    sharedManager = mgr

    code := m.Run()

    if err := mgr.Shutdown(); err != nil {
        fmt.Fprintf(os.Stderr, "Shutdown error: %v\n", err)
    }
    os.Exit(code)
}

func TestExample(t *testing.T) {
    t.Parallel()
    ctx := context.Background()

    inst, err := sharedManager.Acquire(ctx)
    if err != nil {
        t.Fatal(err)
    }
    defer inst.Release() // safe to ignore in defer

    cfg, err := inst.Config()
    client, err := kubernetes.NewForConfig(cfg)
    // Use client...
}
```

### Two-Phase Initialization
Manager creation is split into two phases:
- `NewManager(opts...)` - Pure construction (singleton), no I/O operations
- `Initialize(ctx) error` - Expensive operations (directory creation, CRD cache)

This allows for:
- Error handling instead of panics
- Context-aware initialization with cancellation support
- Idempotent Initialize (safe to call multiple times)

```go
mgr := k8senv.NewManager()
if err := mgr.Initialize(ctx); err != nil {
    // Handle initialization error gracefully
    return err
}
defer mgr.Shutdown()
```

### Parallel Testing
The pool creates up to 4 instances by default. Parallel tests share instances from the pool, with `Acquire` blocking when all are in use. All tests share the singleton manager created in `TestMain`:
```go
func TestParallel(t *testing.T) {
    for i := 0; i < 10; i++ {
        t.Run(fmt.Sprintf("test-%d", i), func(t *testing.T) {
            t.Parallel()
            inst, _ := sharedManager.Acquire(context.Background())
            defer inst.Release()
            // Use unique namespaces for isolation
        })
    }
}
```

Run with: `go test -parallel=10 -tags=integration`

## Important Notes

- **Always** use `defer inst.Release()` in tests to return instances to the pool. The behavior is determined by the Manager's `ReleaseStrategy` (default: `ReleaseRestart`). On error the instance is removed from the pool; safe to ignore in defer.
- **Never** share resources between parallel tests without namespace isolation
- **Configure** the release strategy once in `TestMain` via `WithReleaseStrategy`. Available strategies: `ReleaseRestart` (default, stops instance), `ReleaseClean` (cleans namespaces via API, keeps running), `ReleasePurge` (cleans via direct SQLite deletion, fastest, keeps running), `ReleaseNone` (no cleanup)
- **Check** that kine and kube-apiserver are installed before running integration tests:
  - `which kine` (install: `go install github.com/k3s-io/kine/cmd/kine@latest`)
  - `which kube-apiserver` (download from https://dl.k8s.io/v1.35.0/bin/linux/amd64/kube-apiserver or use `make install-tools`)
- The library logs process output to separate log files in each instance's data directory for debugging:
  - `kine-stdout.log` and `kine-stderr.log`
  - `kube-apiserver-stdout.log` and `kube-apiserver-stderr.log`
- Instance data directories are created under `/tmp/k8senv/` and persist after tests (use `make clean` to remove)

## Troubleshooting

### Integration Tests Fail on Missing Binaries
Tests fail if:
- kine binary not found in `$PATH`
- kube-apiserver binary not found in `$PATH`
- Binaries exist but not working properly (check with `--version`)

Check: `which kine` and `which kube-apiserver`

### Port Conflicts
The library uses dynamic port allocation (two ports per instance: one for kine, one for API server). If you see bind errors, another process may be holding ports.

### Race Detector Warnings
Run tests with `-race` flag to detect concurrency issues. The pool implementation is designed to be race-free.

### Startup Failures
Check logs in `/tmp/k8senv/inst-*/` for errors. Common issues:
- **kine-stderr.log**: SQLite database issues, permission problems
- **kube-apiserver-stderr.log**: Connection to kine failed, certificate generation issues, port already in use


## Issue Management

This project uses **bd** (beads) for issue tracking. Run `bd onboard` to get started.

### Quick Reference

```bash
bd ready              # Find available work
bd show <id>          # View issue details
bd update <id> --status in_progress  # Claim work
bd close <id>         # Complete work
bd sync               # Sync with git
```

### Landing the Plane (Session Completion)

**When ending a work session**, you MUST complete ALL steps below. Work is NOT complete until `git push` succeeds.

**MANDATORY WORKFLOW:**

1. **File issues for remaining work** - Create issues for anything that needs follow-up
2. **Run quality gates** (if code changed) - Tests, linters, builds
3. **Update issue status** - Close finished work, update in-progress items
4. **PUSH TO REMOTE** - This is MANDATORY:
   ```bash
   git pull --rebase
   bd sync
   git push
   git status  # MUST show "up to date with origin"
   ```
5. **Clean up** - Clear stashes, prune remote branches
6. **Verify** - All changes committed AND pushed
7. **Hand off** - Provide context for next session

**CRITICAL RULES:**
- Work is NOT complete until `git push` succeeds
- NEVER stop before pushing - that leaves work stranded locally
- NEVER say "ready to push when you are" - YOU must push
- If push fails, resolve and retry until it succeeds
