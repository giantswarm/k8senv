package core

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/giantswarm/k8senv/internal/fileutil"
	"github.com/giantswarm/k8senv/internal/kubestack"
	"github.com/giantswarm/k8senv/internal/netutil"
	"github.com/giantswarm/k8senv/internal/sentinel"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// DefaultMaxStartRetries is the default number of startup retries for transient startup failures
// (port conflicts, namespace readiness timeouts).
const DefaultMaxStartRetries = 5

// ErrInstanceReleased is returned by Config when called on an instance that has
// been released back to the pool. After Release, the instance may be re-acquired
// by another consumer or stopped, making any previously obtained configuration stale.
const ErrInstanceReleased = sentinel.Error("instance has been released")

// ErrNotStarted is returned by Config when called on an instance whose
// processes have not been launched yet. This typically indicates a programming
// error where Config is called before the instance has been started by the pool.
const ErrNotStarted = sentinel.Error("instance not started")

// InstanceReleaser handles returning an instance to the pool or marking it
// as failed. It breaks the dependency from Instance back to Manager/Pool,
// allowing Instance to release itself without knowing the concrete types.
//
// Implementations must be safe for concurrent use. In particular, ReleaseToPool
// may be called concurrently with Shutdown, and the implementation must ensure
// that every instance is cleaned up exactly once regardless of call ordering.
type InstanceReleaser interface {
	// ReleaseToPool returns the instance to the pool for reuse.
	// The token is the generation value returned by markAcquired during
	// the corresponding Acquire. It is threaded through to the pool's
	// Release method, which uses it to detect stale (double) releases.
	// Returns true if the instance was returned to the pool, false if the
	// manager was shutting down and the instance was stopped instead.
	//
	// Safe for concurrent use with Shutdown. The implementation brackets
	// the state check and pool.Release with an inflight counter, preventing
	// Shutdown from proceeding while any release is in progress.
	ReleaseToPool(i *Instance, token uint64) bool

	// ReleaseFailed marks the instance as permanently failed and removes it
	// from the pool. The token is the generation value from markAcquired.
	// The instance is stopped and never returned to the free channel.
	ReleaseFailed(i *Instance, token uint64)
}

// Instance represents a single kube-apiserver + kine test environment.
// It holds both consumer-facing methods (Config, Release, ID) exposed through
// the public k8senv.Instance interface, and lifecycle methods (Start, Stop,
// IsStarted, IsBusy, Err) used internally by Manager and Pool.
//
// Synchronization strategy:
//   - busy, started, lastErr use atomics for lock-free reads (the common path).
//   - stack and cancel are only accessed under startMu (in doStart and Stop),
//     so no additional lock is needed. started.Store(true) after setting
//     stack/cancel under startMu provides happens-before via the Go memory model.
type Instance struct {
	cfg InstanceConfig

	id         string
	dataDir    string
	sqlitePath string
	kubeconfig string

	// releaser is the Pool/Manager callback for release.
	// Set once at construction, read-only thereafter.
	releaser InstanceReleaser
	// ports is the shared port registry for cross-instance coordination.
	ports *netutil.PortRegistry

	// gen is a monotonic generation counter: odd = acquired, even = free (0, 2, 4, ...).
	gen atomic.Uint64
	// started is set by doStart, cleared by Stop.
	started atomic.Bool
	// lastErr is set during warm-up or start failure.
	lastErr atomic.Pointer[error]
	// cleanupClient is a cached client for namespace cleanup.
	// Set on first Release, cleared by Stop.
	cleanupClient atomic.Pointer[kubernetes.Clientset]
	// cachedConfig is a cached rest.Config.
	// Set on first Config() call, cleared by Stop.
	cachedConfig atomic.Pointer[rest.Config]
	// discoveryClient is a cached discovery client for resource enumeration.
	// Set on first cleanNamespacedResources call, cleared by Stop.
	discoveryClient atomic.Pointer[discovery.DiscoveryClient]
	// dynamicClient is a cached dynamic client for resource deletion.
	// Set on first cleanNamespacedResources call, cleared by Stop.
	dynamicClient atomic.Pointer[dynamic.DynamicClient]
	// cachedGVRs is the discovered set of deletable namespaced resource types.
	// Cached across Release calls since CRDs don't change after initialization.
	// Cleared by Stop (API server may have different resources after restart).
	cachedGVRs atomic.Pointer[[]schema.GroupVersionResource]

	// startMu serializes Start/Stop to prevent duplicate process launches.
	startMu sync.Mutex
	// cancel is the process context cancel function. Protected by startMu only.
	cancel context.CancelFunc
	// stack is the kine + kube-apiserver process stack. Protected by startMu only.
	stack *kubestack.Stack

	// log is the instance-scoped logger.
	log *slog.Logger
}

// IsStarted reports whether the instance's processes have been launched.
func (i *Instance) IsStarted() bool {
	return i.started.Load()
}

// IsBusy reports whether the instance is currently acquired by a consumer.
// An odd generation value means acquired; even (including 0) means free.
func (i *Instance) IsBusy() bool {
	return i.gen.Load()%2 == 1
}

// markAcquired increments the generation counter and returns the new value
// as a release token. The counter is monotonically increasing: odd values
// (1, 3, 5, ...) indicate acquired, even values (0, 2, 4, ...) indicate free.
// The token must be passed to tryRelease to complete the release. This prevents
// ABA double-release races: each acquisition produces a unique odd token, so a
// stale token from a prior acquisition can never match the current generation.
func (i *Instance) markAcquired() uint64 {
	return i.gen.Add(1)
}

// tryRelease atomically advances the generation counter from the provided
// token (odd/acquired) to token+1 (even/free). Returns true if the release
// succeeded, false if the token is stale (the instance was re-acquired by
// another goroutine). Because the counter never resets to 0, each token is
// globally unique, eliminating the ABA race where a stale token from a prior
// acquisition could match the current generation.
func (i *Instance) tryRelease(token uint64) bool {
	return i.gen.CompareAndSwap(token, token+1)
}

// isCurrentToken reports whether the given token matches the current generation.
// This is a non-consuming check used to reject stale releases before performing
// irreversible side effects (e.g., namespace cleanup). The actual release is
// still performed via tryRelease (CAS) after side effects complete.
func (i *Instance) isCurrentToken(token uint64) bool {
	return i.gen.Load() == token
}

// Err returns the last error that occurred on this instance.
func (i *Instance) Err() error {
	if p := i.lastErr.Load(); p != nil {
		return *p
	}
	return nil
}

// ID returns the instance's unique identifier.
func (i *Instance) ID() string {
	return i.id
}

// setErr records the last error on this instance.
func (i *Instance) setErr(e error) {
	i.lastErr.Store(&e)
}

// NewInstanceParams holds the parameters for creating a new Instance.
// All fields are required.
type NewInstanceParams struct {
	ID       string
	DataDir  string
	Releaser InstanceReleaser
	Ports    *netutil.PortRegistry
	Config   InstanceConfig
}

// NewInstance creates a new Instance from the given parameters.
// Callers must fully populate params, including params.Config.
// Panics if ID or DataDir is empty, if Releaser or Ports is nil, or if
// Config fails validation (see InstanceConfig.Validate).
// These are programmer errors that should be caught at initialization time.
func NewInstance(params NewInstanceParams) *Instance {
	if params.ID == "" {
		panic("k8senv: instance id must not be empty")
	}
	if params.DataDir == "" {
		panic("k8senv: instance data dir must not be empty")
	}
	if params.Releaser == nil {
		panic("k8senv: instance releaser must not be nil")
	}
	if params.Ports == nil {
		panic("k8senv: instance port registry must not be nil")
	}
	if err := params.Config.Validate(); err != nil {
		panic(fmt.Sprintf("k8senv: invalid instance config: %v", err))
	}
	return &Instance{
		cfg:        params.Config,
		id:         params.ID,
		dataDir:    params.DataDir,
		sqlitePath: filepath.Join(params.DataDir, "db", "state.db"),
		kubeconfig: filepath.Join(params.DataDir, "kubeconfig.yaml"),
		releaser:   params.Releaser,
		ports:      params.Ports,
		log:        Logger().With("id", params.ID),
	}
}

// Start launches kine and kube-apiserver.
// Safe for concurrent calls: startMu serializes callers so only one
// actually launches processes; subsequent callers see started==true.
// Retry logic for transient failures (e.g., port conflicts) is handled
// by [kubestack.StartWithRetry] inside doStart.
func (i *Instance) Start(ctx context.Context) error {
	i.startMu.Lock()
	defer i.startMu.Unlock()

	if i.IsStarted() {
		return nil // Already started
	}

	return i.doStart(ctx)
}

// doStart performs a single startup attempt. On success it sets stack and
// cancel under startMu, then publishes started=true via an atomic store.
// The atomic store provides a happens-before edge: any goroutine that
// observes started==true is guaranteed to see the stack/cancel writes
// that preceded the store (Go memory model §sync/atomic).
func (i *Instance) doStart(ctx context.Context) error {
	startTime := time.Now()
	i.log.Debug("starting instance", "time", startTime.Format("15:04:05.000"))

	// Setup data directory
	i.log.Debug("setting up directories")
	if err := fileutil.EnsureDir(i.dataDir); err != nil {
		return fmt.Errorf("mkdir data dir: %w", err)
	}

	var lastNSErr error
	for attempt := 1; attempt <= i.cfg.MaxStartRetries; attempt++ {
		// Create process context just before starting processes.
		// Using Background so processes survive beyond the Acquire() call.
		// The passed ctx is only used for startup timeouts (readiness checks).
		processCtx, cancel := context.WithCancel(context.Background())

		// Create and start stack with retry logic for transient port conflicts.
		// Each retry creates a fresh kubestack with new port allocations.
		stack, err := kubestack.StartWithRetry(processCtx, ctx, kubestack.Config{
			DataDir:               i.dataDir,
			SQLitePath:            i.sqlitePath,
			KubeconfigPath:        i.kubeconfig,
			KineBinary:            i.cfg.KineBinary,
			APIServerBinary:       i.cfg.KubeAPIServerBinary,
			CachedDBPath:          i.cfg.CachedDBPath,
			KineReadyTimeout:      i.cfg.StartTimeout,
			APIServerReadyTimeout: i.cfg.StartTimeout,
			PortRegistry:          i.ports,
			Logger:                i.log,
		}, i.cfg.MaxStartRetries, i.cfg.StopTimeout)
		if err != nil {
			cancel()
			// StartWithRetry already exhausted its internal retries for port
			// conflicts — no point retrying at this level.
			return fmt.Errorf("start kubestack: %w", err)
		}

		// Wait for all system namespaces to exist before marking started.
		// /livez returns 200 before the namespace controller creates them;
		// this closes that gap (~10-50ms) so consumers never see missing namespaces.
		if err := i.waitForSystemNamespaces(ctx); err != nil {
			cancel()
			if stopErr := stack.Stop(i.cfg.StopTimeout); stopErr != nil {
				i.log.Warn("cleanup stack after namespace wait failure", "error", stopErr)
			}
			// Clear stale state from the failed attempt — the old apiserver
			// ports are gone, so cached configs and clients are invalid.
			i.cleanupClient.Store(nil)
			i.cachedConfig.Store(nil)
			i.discoveryClient.Store(nil)
			i.dynamicClient.Store(nil)
			i.cachedGVRs.Store(nil)

			lastNSErr = err
			if attempt < i.cfg.MaxStartRetries {
				i.log.Warn("system namespace timeout, retrying instance start",
					"attempt", attempt,
					"max_retries", i.cfg.MaxStartRetries,
					"error", err,
				)
				continue
			}
			// Final attempt exhausted — fall through to return error.
			break
		}

		// Install process handles under startMu (already held by caller),
		// then publish started=true. The atomic store creates a happens-before
		// edge so any reader that sees started==true also sees stack/cancel.
		i.cancel = cancel
		i.stack = stack
		i.started.Store(true)

		if attempt > 1 {
			i.log.Info("instance started after namespace retry", "attempt", attempt)
		}
		i.log.Debug("instance started successfully", "total_elapsed", time.Since(startTime))
		return nil
	}

	return fmt.Errorf("wait for system namespaces: %w", lastNSErr)
}

// getOrBuildRestConfig returns a copy of the cached rest.Config, or builds one
// from the kubeconfig file and caches it for future calls. The returned copy is
// safe for callers to mutate (e.g., setting QPS) without affecting the cache.
func (i *Instance) getOrBuildRestConfig() (*rest.Config, error) {
	if cfg := i.cachedConfig.Load(); cfg != nil {
		return rest.CopyConfig(cfg), nil
	}
	cfg, err := clientcmd.BuildConfigFromFlags("", i.kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("build config from kubeconfig: %w", err)
	}
	i.cachedConfig.CompareAndSwap(nil, cfg)
	return rest.CopyConfig(i.cachedConfig.Load()), nil
}

// Config returns *rest.Config for connecting to this instance's kube-apiserver.
// It must be called while the instance is acquired (between Acquire and Release).
//
// Returns ErrInstanceReleased if the instance has been released back to the pool.
// Returns ErrNotStarted if the instance has not been started yet.
//
// TOCTOU note: there is a deliberate time-of-check-time-of-use window between
// the busy/started checks and the subsequent kubeconfig read. Between those
// two steps, a concurrent goroutine could theoretically call Release or Stop,
// making the state snapshot stale. This is acceptable because the Instance
// contract requires callers to hold the instance via Acquire for the entire
// duration of use. A correctly written caller never races Config against
// Release on the same instance. The busy/started checks therefore serve as
// defensive guards against programmer error (e.g., calling Config after
// Release), not as concurrency-safe guarantees.
func (i *Instance) Config() (*rest.Config, error) {
	if i.gen.Load()%2 == 0 {
		return nil, ErrInstanceReleased
	}
	if !i.started.Load() {
		return nil, ErrNotStarted
	}
	return i.getOrBuildRestConfig()
}

// Stop shuts down both kube-apiserver and kine. The provided context allows
// callers to bound the stop duration or cancel it early. If the context has a
// deadline, the effective timeout is the minimum of the context's remaining
// time and the configured StopTimeout. If the context has no deadline, the
// configured StopTimeout is used as a fallback.
//
// Safe for concurrent calls with Start: startMu serializes them so Stop
// cannot run while Start is launching processes (and vice versa).
func (i *Instance) Stop(ctx context.Context) error {
	// Fail fast if the caller has already canceled the context, to avoid
	// acquiring startMu and doing unnecessary work.
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("stop instance: %w", err)
	}

	i.startMu.Lock()
	defer i.startMu.Unlock()

	// Clear state under startMu, then publish started=false.
	stack := i.stack
	cancel := i.cancel
	i.stack = nil
	i.cancel = nil
	i.cleanupClient.Store(nil)
	i.cachedConfig.Store(nil)
	i.discoveryClient.Store(nil)
	i.dynamicClient.Store(nil)
	i.cachedGVRs.Store(nil)
	i.started.Store(false)

	if cancel != nil {
		cancel()
	}

	if stack == nil {
		return nil
	}

	timeout := i.effectiveStopTimeout(ctx)
	if err := stack.Stop(timeout); err != nil {
		return fmt.Errorf("stop kubestack: %w", err)
	}
	return nil
}

// effectiveStopTimeout returns the stop timeout to use, choosing the smaller
// of the context's remaining time and the configured StopTimeout. If the
// context has no deadline, the configured StopTimeout is used.
func (i *Instance) effectiveStopTimeout(ctx context.Context) time.Duration {
	timeout := i.cfg.StopTimeout
	if deadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(deadline); remaining < timeout {
			timeout = remaining
		}
	}
	// Ensure a non-negative timeout; a zero or negative value would cause
	// immediate expiry in the underlying stop sequence.
	if timeout <= 0 {
		timeout = time.Millisecond
	}
	return timeout
}

// Release marks the Instance as free and returns it to the pool.
//
// The behavior depends on the ReleaseStrategy configured on the Manager:
//
//   - ReleaseRestart: stops the instance. The next Acquire starts a fresh
//     instance with the database restored from the cached template. No
//     API-level cleanup is needed.
//   - ReleaseClean: deletes all namespaced resources in non-system namespaces,
//     then deletes the namespaces themselves before returning the running
//     instance to the pool. Resource deletion precedes namespace deletion
//     because k8senv runs in API-only mode (no kube-controller-manager),
//     so namespace deletion does not cascade-delete contained resources.
//     Faster than ReleaseRestart but relies on cleanup correctness.
//   - ReleaseNone: returns the instance to the pool immediately with no
//     cleanup. Use only when tests use unique namespaces.
//
// Error semantics:
//   - ReleaseNone always returns nil (no cleanup to fail).
//   - ReleaseClean returns nil on success. If namespace cleanup fails, the
//     instance is marked as permanently failed via ReleaseFailed and the
//     error is returned. Using defer inst.Release() is safe.
//   - ReleaseRestart returns nil on success. If Stop fails, the instance
//     is marked as permanently failed via ReleaseFailed. The error is
//     informational: no corrective action is required.
//
// The shutdown check and pool release are performed atomically via
// the InstanceReleaser to prevent a TOCTOU race. If the manager is shutting
// down, the instance is stopped instead of being returned to the pool.
func (i *Instance) Release(token uint64) error {
	if i.releaser == nil {
		panic("k8senv: Release called on instance with nil releaser")
	}

	// Validate the token before performing any side effects. A stale token
	// means this release is from a prior acquisition — the instance has
	// already been released and re-acquired by another goroutine. Running
	// cleanup (namespace deletion) with a stale token would corrupt the
	// current holder's state. Panic immediately, matching the double-release
	// panic contract from Pool.Release/tryRelease.
	//
	// Token validity window: there is a gap between this isCurrentToken
	// check and the eventual ReleaseToPool/ReleaseFailed call below. During
	// this window the token remains valid (gen is still odd/acquired) because
	// only this goroutine holds the instance — the pool contract guarantees
	// at most one holder per acquisition. No other goroutine can call
	// markAcquired (which would advance gen) until tryRelease completes
	// inside ReleaseToPool or ReleaseFailed.
	if !i.isCurrentToken(token) {
		panic("k8senv: double-release of instance " + i.id)
	}

	switch i.cfg.ReleaseStrategy {
	case ReleaseNone:
		// Skip all cleanup — return to pool immediately.

	case ReleaseClean:
		// Clean user resources and namespaces before returning to pool, so
		// the next consumer gets a clean instance. Only run if the instance
		// has started processes (kubeconfig exists and apiserver is reachable).
		// Resources must be deleted before namespaces because k8senv runs in
		// API-only mode (no kube-controller-manager), so namespace deletion
		// does not cascade-delete contained resources.
		if i.started.Load() {
			cleanCtx, cleanCancel := context.WithTimeout(context.Background(), i.cfg.CleanupTimeout)
			err := i.cleanNamespacedResources(cleanCtx)
			cleanCancel()
			if err != nil {
				cleanupErr := fmt.Errorf("resource cleanup during release: %w", err)
				i.setErr(cleanupErr)
				i.releaser.ReleaseFailed(i, token)
				return cleanupErr
			}

			cleanCtx, cleanCancel = context.WithTimeout(context.Background(), i.cfg.CleanupTimeout)
			err = i.cleanNamespaces(cleanCtx)
			cleanCancel()
			if err != nil {
				cleanupErr := fmt.Errorf("namespace cleanup during release: %w", err)
				i.setErr(cleanupErr)
				i.releaser.ReleaseFailed(i, token)
				return cleanupErr
			}
		}

	case ReleaseRestart:
		// Stop the instance. The next Acquire will start fresh with the
		// database restored from the cached template. No API-level cleanup
		// is needed since the DB is replaced on restart.
		ctx, cancel := context.WithTimeout(context.Background(), i.cfg.StopTimeout)
		defer cancel()
		if err := i.Stop(ctx); err != nil {
			stopErr := fmt.Errorf("stop during release: %w", err)
			i.setErr(stopErr)
			i.releaser.ReleaseFailed(i, token)
			return stopErr
		}

	default:
		// All valid strategies are handled above. An unknown value here
		// indicates a programmer error — the strategy is validated at
		// construction time by InstanceConfig.Validate, so this branch
		// should be unreachable.
		panic(fmt.Sprintf("k8senv: unknown release strategy: %v", i.cfg.ReleaseStrategy))
	}

	// Atomically check shutdown state and release to pool. This eliminates
	// the TOCTOU race where Shutdown could start between checking
	// IsShuttingDown and calling pool.Release.
	i.releaser.ReleaseToPool(i, token)
	return nil
}
