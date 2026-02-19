package core

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/giantswarm/k8senv/internal/crdcache"
	"github.com/giantswarm/k8senv/internal/fileutil"
	"github.com/giantswarm/k8senv/internal/netutil"
	"github.com/giantswarm/k8senv/internal/sentinel"
)

// managerState represents the lifecycle state of a Manager.
type managerState uint32

const (
	managerCreated      managerState = iota // Zero value; NewManagerWithConfig returns in this state
	managerInitializing                     // Initialize in progress
	managerReady                            // Acquire allowed
	managerShuttingDown                     // Shutdown called
)

// ErrShuttingDown is returned by Acquire when the Manager is shutting down.
const ErrShuttingDown = sentinel.Error("manager is shutting down")

// ErrNotInitialized is returned by Acquire when Initialize has not been called.
const ErrNotInitialized = sentinel.Error("manager not initialized")

// ErrNoYAMLFiles is re-exported from crdcache so the public API imports only
// from core, preserving the layering: public API → core → crdcache.
const ErrNoYAMLFiles = crdcache.ErrNoYAMLFiles

// ErrMissingKind is re-exported from crdcache so the public API imports only
// from core, preserving the layering: public API → core → crdcache.
const ErrMissingKind = crdcache.ErrMissingKind

// ErrCRDEstablishTimeout is re-exported from crdcache so the public API
// imports only from core, preserving the layering: public API → core → crdcache.
const ErrCRDEstablishTimeout = crdcache.ErrCRDEstablishTimeout

// Verify Manager implements InstanceReleaser at compile time.
var _ InstanceReleaser = (*Manager)(nil)

// Manager is the concrete implementation of the Manager interface.
// It coordinates a Pool of kube-apiserver instances for testing.
// It is safe for concurrent use by multiple goroutines.
//
// Configuration is stored in the cfg field and is immutable after
// construction. Runtime state (cachedDBPath, pool, state) is kept in
// separate fields to maintain a clear boundary between configuration
// and mutable state.
//
// Synchronization strategy:
//   - state is an atomic managerState enum (created → initializing → ready → shuttingDown).
//     Acquire reads it with a single atomic load for the fast path.
//   - pool is an atomic.Pointer[Pool], set once during Initialize and read
//     lock-free by Acquire, Shutdown, tryReleaseToPool, and ReleaseFailed.
//   - initMu serializes concurrent Initialize calls (needed for TestInitializeConcurrent).
//   - inflight counts goroutines inside tryReleaseToPool's check-and-release window.
//     Shutdown sets managerShuttingDown then waits on inflightDone for inflight
//     to reach zero. tryReleaseToPool closes inflightDone (via sync.Once) when
//     inflight drops to zero during shutdown.
type Manager struct {
	cfg ManagerConfig

	// cachedDBPath holds the path to the CRD-prepopulated database,
	// set during Initialize. Separated from cfg to preserve the
	// immutable-after-construction contract of ManagerConfig.
	cachedDBPath string

	// ports coordinates port allocation across all instances and temporary
	// stacks (e.g., CRD cache creation). Created during construction and
	// shared via dependency injection.
	ports *netutil.PortRegistry

	pool atomic.Pointer[Pool]

	state atomic.Uint32 // managerState; zero value is managerCreated

	// inflight counts goroutines inside tryReleaseToPool's check-and-release
	// window. Shutdown waits for inflight to reach zero before proceeding,
	// preventing the TOCTOU race where Shutdown could set the state between
	// tryReleaseToPool's state check and pool.Release call.
	inflight atomic.Int64

	// inflightDone is closed when inflight drops to zero during shutdown,
	// replacing the former spin-wait with a notification-based drain.
	inflightDone chan struct{}

	// inflightDoneOnce ensures inflightDone is closed at most once, even
	// if multiple goroutines decrement inflight to zero concurrently.
	inflightDoneOnce sync.Once

	// initMu serializes concurrent Initialize calls. Pool reads use
	// atomic.Pointer and do not require initMu.
	initMu sync.Mutex
}

// loadState returns the current manager lifecycle state.
func (m *Manager) loadState() managerState {
	return managerState(m.state.Load())
}

// storeState sets the manager lifecycle state.
func (m *Manager) storeState(s managerState) {
	m.state.Store(uint32(s))
}

// NewManagerWithConfig creates a Manager with the provided configuration.
// This performs no I/O operations. Call Initialize before Acquire.
//
// Panics if cfg.Validate() reports any errors. Invalid configuration is a
// programmer error that should be caught at construction time, similar to
// regexp.MustCompile.
func NewManagerWithConfig(cfg ManagerConfig) *Manager {
	if err := cfg.Validate(); err != nil {
		panic(fmt.Sprintf("k8senv: invalid manager config: %v", err))
	}
	return &Manager{
		cfg:          cfg,
		cachedDBPath: cfg.PrepopulateDBPath,
		ports:        netutil.NewPortRegistry(nil),
		inflightDone: make(chan struct{}),
	}
}

// Initialize performs expensive initialization operations.
// Must be called before Acquire. Returns error instead of panicking.
// Safe to call multiple times: after a successful initialization, subsequent
// calls return nil immediately. If initialization fails, subsequent calls
// retry the initialization instead of returning a cached error permanently.
func (m *Manager) Initialize(ctx context.Context) error {
	m.initMu.Lock()
	defer m.initMu.Unlock()

	switch m.loadState() {
	case managerReady:
		return nil
	case managerShuttingDown:
		return ErrShuttingDown
	case managerCreated, managerInitializing:
		// Continue with initialization (or retry after prior failure).
	}

	m.storeState(managerInitializing)

	// Defense in depth: validate config even though NewManagerWithConfig
	// already panics on invalid config. This catches cases where Manager
	// is constructed via struct literal (bypassing NewManagerWithConfig).
	if err := m.cfg.Validate(); err != nil {
		m.storeState(managerCreated)
		return fmt.Errorf("invalid config: %w", err)
	}

	if err := m.doInitialize(ctx); err != nil {
		// Roll back partial state so Acquire sees nil pool (ErrNotInitialized)
		// and a subsequent Initialize call can retry from scratch.
		// Stop all instances before nilling the pool to avoid orphaning
		// processes and data directories.
		if p := m.pool.Load(); p != nil {
			// Stop instances in parallel. Each instance is independent
			// and stopping is I/O-bound, so parallel stops reduce
			// worst-case rollback latency from N*StopTimeout to 1*StopTimeout.
			// This mirrors the parallel stop pattern used in Shutdown.
			var wg sync.WaitGroup
			for _, inst := range p.Instances() {
				if inst == nil {
					continue
				}
				wg.Add(1)
				go func(i *Instance) { //nolint:contextcheck // rollback must use background context; caller's context may be canceled
					defer wg.Done()
					// Use a bounded background context instead of the caller's context.
					// The caller's context may already be canceled (a common cause of
					// the initialization failure), which would cause Stop to return
					// immediately and orphan running processes.
					stopCtx, stopCancel := context.WithTimeout(context.Background(), m.cfg.InstanceStopTimeout)
					defer stopCancel()
					if stopErr := i.Stop(stopCtx); stopErr != nil {
						Logger().Warn("failed to stop instance during rollback",
							"id", i.ID(), "error", stopErr)
					}
				}(
					inst,
				)
			}
			wg.Wait()
		}
		m.pool.Store(nil)
		// Reset cachedDBPath so a retry doesn't use a stale path
		// pointing to a cache that may have been cleaned up.
		m.cachedDBPath = m.cfg.PrepopulateDBPath
		m.storeState(managerCreated)
		return fmt.Errorf("initialize: %w", err)
	}

	m.storeState(managerReady)
	return nil
}

// doInitialize contains the actual initialization logic.
func (m *Manager) doInitialize(ctx context.Context) error {
	if err := fileutil.EnsureDir(m.cfg.BaseDataDir); err != nil {
		return fmt.Errorf("init base dir: %w", err)
	}

	if m.cfg.CRDDir != "" {
		result, err := crdcache.EnsureCache(ctx, crdcache.Config{
			CRDDir:              m.cfg.CRDDir,
			CacheDir:            m.cfg.BaseDataDir,
			KineBinary:          m.cfg.KineBinary,
			KubeAPIServerBinary: m.cfg.KubeAPIServerBinary,
			Timeout:             m.cfg.CRDCacheTimeout,
			StopTimeout:         m.cfg.InstanceStopTimeout,
			PortRegistry:        m.ports,
			Logger:              Logger(),
		})
		if err != nil {
			return fmt.Errorf("ensure CRD cache: %w", err)
		}
		m.cachedDBPath = result.CachePath
	}

	instCfg := InstanceConfig{
		StartTimeout:        m.cfg.InstanceStartTimeout,
		StopTimeout:         m.cfg.InstanceStopTimeout,
		CleanupTimeout:      m.cfg.CleanupTimeout,
		MaxStartRetries:     defaultMaxStartRetries,
		CachedDBPath:        m.cachedDBPath,
		KineBinary:          m.cfg.KineBinary,
		KubeAPIServerBinary: m.cfg.KubeAPIServerBinary,
		ReleaseStrategy:     m.cfg.ReleaseStrategy,
	}

	factory := m.instanceFactory(m.cfg.BaseDataDir, instCfg)
	m.pool.Store(NewPool(factory, m.cfg.PoolSize))

	return nil
}

// genID generates a random 8-character hex ID for instance naming.
func genID() string {
	return fmt.Sprintf(
		"%08x",
		rand.Uint32(), //nolint:gosec // G404: instance IDs need uniqueness, not cryptographic strength
	)
}

// instanceFactory returns an InstanceFactory that creates instances with the
// given base data directory and configuration. The factory generates unique IDs,
// constructs per-instance directories, and wires the manager as the releaser.
func (m *Manager) instanceFactory(baseDataDir string, cfg InstanceConfig) InstanceFactory {
	return func(index int) (*Instance, error) {
		id := genID()
		instID := fmt.Sprintf("inst-%d-%s", index, id)
		instDir := filepath.Join(baseDataDir, instID)
		return NewInstance(NewInstanceParams{
			ID:       instID,
			DataDir:  instDir,
			Releaser: m,
			Ports:    m.ports,
			Config:   cfg,
		}), nil
	}
}

// Acquire gets an Instance, creating one on demand if none are free.
// Implements lazy start: the instance's processes are started on first acquisition.
//
// The acquireTimeout (configured via WithAcquireTimeout) covers instance startup
// time. Instance startup typically takes 5-15 seconds.
//
// Returns ErrNotInitialized if Initialize has not been called.
// Returns ErrShuttingDown if the Manager is shutting down.
func (m *Manager) Acquire(ctx context.Context) (*Instance, uint64, error) {
	// Fast path: single atomic load replaces the former two-mutex acquirePreChecks.
	switch m.loadState() {
	case managerShuttingDown:
		return nil, 0, ErrShuttingDown
	case managerReady:
		// Continue to pool acquisition.
	case managerCreated, managerInitializing:
		return nil, 0, ErrNotInitialized
	}

	pool := m.pool.Load()
	if pool == nil {
		return nil, 0, ErrNotInitialized
	}

	acquireCtx, cancel := context.WithTimeout(ctx, m.cfg.AcquireTimeout)
	defer cancel()

	inst, token, err := pool.Acquire(acquireCtx)
	if err != nil {
		return nil, 0, fmt.Errorf("acquire instance from pool: %w", err)
	}

	// Recheck shutdown state after pool acquisition. Between the pre-check
	// and the pool.Acquire return, Shutdown may have started. Without this
	// recheck we could hand out an instance while/after Shutdown is stopping
	// all other instances. Stop the instance ourselves because Shutdown's
	// iteration over pool.Instances may have already passed this instance.
	//
	// Bookkeeping note: the instance is stopped here but remains in
	// pool.Instances(). When Shutdown iterates pool.Instances() it will
	// call Stop on this instance again. This is a harmless no-op because
	// Instance.Stop is idempotent.
	if m.loadState() == managerShuttingDown {
		m.stopInstanceDuringShutdown(acquireCtx, inst, token)
		return nil, 0, ErrShuttingDown
	}

	// Lazy start: start Instance if not already started
	if !inst.IsStarted() {
		// Use acquireCtx to ensure bounded startup time and prevent hanging indefinitely
		if err := inst.Start(acquireCtx); err != nil {
			// Mark Instance as failed instead of releasing back to Pool
			inst.setErr(err)
			pool.ReleaseFailed( //nolint:contextcheck // ReleaseFailed is fire-and-forget cleanup; it creates its own bounded context internally
				inst,
				token,
			)
			return nil, 0, fmt.Errorf("start instance: %w", err)
		}
	}

	return inst, token, nil
}

// IsShuttingDown reports whether Shutdown has been called.
func (m *Manager) IsShuttingDown() bool {
	return m.loadState() == managerShuttingDown
}

// ReleaseToPool atomically checks the shutdown state and either returns the
// instance to the pool or stops it. Returns true if the instance was returned
// to the pool, false if the manager was shutting down and the instance was
// stopped instead.
//
// Concurrency with Shutdown:
//
// ReleaseToPool and Shutdown may execute concurrently. The inflight counter
// guarantees exactly-once cleanup of every instance regardless of ordering:
//
//   - If ReleaseToPool runs first: tryReleaseToPool increments inflight,
//     checks the state, and calls pool.Release before decrementing. Shutdown
//     waits on inflightDone until inflight reaches zero, so the instance is
//     safely back in the free stack before Shutdown iterates pool.Instances.
//
//   - If Shutdown runs first: Shutdown stores managerShuttingDown, then
//     waits on inflightDone for inflight to drain. When tryReleaseToPool
//     increments inflight and checks state, it sees shuttingDown and
//     returns false (after decrementing). The caller stops the instance
//     itself.
//
//   - Overlapping window: multiple ReleaseToPool calls can execute
//     concurrently (each increments inflight independently). Shutdown
//     blocks on inflightDone until the last in-flight release completes
//     and closes the channel.
//
// Callers should expect that after Shutdown begins, ReleaseToPool returns
// false and the instance is stopped rather than pooled.
//
// Implements InstanceReleaser.
func (m *Manager) ReleaseToPool(i *Instance, token uint64) bool {
	released := m.tryReleaseToPool(i, token)
	if released {
		return true
	}

	// Manager is shutting down: stop the instance instead of returning it
	// to the pool. ReleaseToPool has no caller context, so create one with
	// the configured stop timeout.
	ctx, cancel := context.WithTimeout(context.Background(), m.cfg.InstanceStopTimeout)
	defer cancel()
	m.stopInstanceDuringShutdown(ctx, i, token)
	return false
}

// stopInstanceDuringShutdown clears the instance's busy flag and stops it.
// Used by both the Acquire shutdown recheck and the ReleaseToPool shutdown
// path to ensure consistent cleanup when the manager is shutting down and an
// instance cannot be returned to the pool.
//
// The provided context bounds the stop operation. Callers that have a context
// (e.g., Acquire) pass it directly. Callers without a context (e.g.,
// ReleaseToPool) create a background context with the configured stop timeout.
//
// Panics on double-release (busy flag already clear), which indicates a
// programming error.
func (m *Manager) stopInstanceDuringShutdown(ctx context.Context, i *Instance, token uint64) {
	if !i.tryRelease(token) {
		panic("k8senv: double-release of instance " + i.ID())
	}
	if err := i.Stop(ctx); err != nil {
		i.log.Warn("failed to stop instance during shutdown", "error", err)
	}
}

// tryReleaseToPool increments the inflight counter, checks the shutdown state,
// and either releases the instance to the pool or returns false. The inflight
// counter brackets the state check and pool.Release, preventing Shutdown from
// proceeding while any release is in progress. This eliminates the TOCTOU race
// where Shutdown could set the state between the check and the pool.Release call.
//
// The defer ensures the inflight counter is decremented even if pool.Release
// panics (e.g., on double-release detection). When the decrement reaches zero
// during shutdown, the defer closes inflightDone to unblock Shutdown.
//
// Returns true if the instance was returned to the pool, false if the manager
// is shutting down.
func (m *Manager) tryReleaseToPool(i *Instance, token uint64) bool {
	m.inflight.Add(1)
	defer func() {
		if m.inflight.Add(-1) == 0 && m.loadState() == managerShuttingDown {
			m.inflightDoneOnce.Do(func() { close(m.inflightDone) })
		}
	}()

	if m.loadState() == managerShuttingDown {
		return false
	}

	pool := m.pool.Load()
	if pool == nil {
		return false
	}

	pool.Release(i, token)
	return true
}

// ReleaseFailed marks the instance as permanently failed and removes it from
// the pool. Delegates to Pool.ReleaseFailed.
//
// Implements InstanceReleaser.
func (m *Manager) ReleaseFailed(i *Instance, token uint64) {
	pool := m.pool.Load()
	if pool == nil {
		return
	}

	pool.ReleaseFailed(i, token)
}

// Shutdown stops all instances and cleans up. Safe to call even if Initialize
// was never called. Safe to call multiple times (idempotent: the first call
// performs cleanup; subsequent calls return nil because the pool is already
// drained and closed). Returns an error if any instance fails to stop.
//
// Concurrency with ReleaseToPool and Acquire:
//
// Shutdown stores managerShuttingDown, then waits up to 30 seconds for the
// inflight counter to reach zero before proceeding. If the timeout expires,
// Shutdown logs a warning and continues with instance cleanup. This interacts
// with ReleaseToPool and Acquire:
//
//   - In-flight ReleaseToPool calls: Shutdown waits on the inflightDone
//     channel until all concurrent tryReleaseToPool calls (which increment
//     inflight) complete their check-and-release. The last goroutine to
//     decrement inflight to zero closes the channel, unblocking Shutdown.
//     Any subsequent ReleaseToPool call sees the flag and stops the instance
//     itself. This guarantees that every instance is stopped exactly once:
//     either by Shutdown's iteration or by ReleaseToPool's fallback path.
//
//   - In-flight Acquire calls: Acquire rechecks the state after receiving an
//     instance from the pool. If Shutdown started between the pre-check
//     and the pool.Acquire return, Acquire stops the instance and returns
//     ErrShuttingDown.
//
// Lock independence: inflight is atomic (not a lock); initMu is used only
// during Initialize.
func (m *Manager) Shutdown() error {
	// Sequential consistency: storeState uses atomic.Store, which provides
	// a happens-before edge. All goroutines that subsequently call
	// loadState (in Acquire, tryReleaseToPool, Initialize) are guaranteed
	// to observe managerShuttingDown. This is the linearization point
	// that makes the inflight drain correct — tryReleaseToPool's state
	// check cannot see a stale "ready" value after this store completes.
	m.storeState(managerShuttingDown)

	drainTimeout := m.cfg.ShutdownDrainTimeout
	if m.inflight.Load() == 0 {
		m.inflightDoneOnce.Do(func() { close(m.inflightDone) })
	}
	drainTimer := time.NewTimer(drainTimeout)
	select {
	case <-m.inflightDone:
		drainTimer.Stop()
	case <-drainTimer.C:
		Logger().Warn("shutdown: timed out waiting for inflight operations to drain; proceeding",
			slog.Int64("inflight", m.inflight.Load()),
			slog.Duration("timeout", drainTimeout))
	}

	pool := m.pool.Load()

	if pool == nil {
		return nil
	}

	// Close the pool before stopping instances to provide defense in depth.
	// After the inflight drain above, no new tryReleaseToPool call can reach
	// pool.Release (they all see managerShuttingDown and return false). Closing
	// the pool here adds a second guard: even if a Release somehow reached the
	// pool, pool.Release checks p.closed and stops the instance instead of
	// returning it to the free stack. Closing early also unblocks any
	// pool.Acquire calls stuck on the bounded-pool semaphore (via closeCh).
	//
	// This is safe because:
	//   - pool.Instances() reads p.all regardless of p.closed.
	//   - Instance.Stop is idempotent, so double-stops are harmless.
	//   - pool.Close itself is idempotent.
	pool.Close()

	// Stop all instances concurrently. Each instance is independent, so
	// parallel stops reduce worst-case latency from N*StopTimeout to
	// 1*StopTimeout.
	instances := pool.Instances()
	stopErrs := make([]error, len(instances))
	var wg sync.WaitGroup
	for idx, i := range instances {
		if i == nil {
			continue
		}
		if i.IsBusy() {
			i.log.Warn("stopping instance that is still in use; " +
				"ensure all instances are released before calling Shutdown")
		}
		wg.Add(1)
		go func(pos int, inst *Instance) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), m.cfg.InstanceStopTimeout)
			defer cancel()
			if err := inst.Stop(ctx); err != nil {
				stopErrs[pos] = err
			}
		}(idx, i)
	}
	wg.Wait()

	var errs []error
	for _, err := range stopErrs {
		if err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}
