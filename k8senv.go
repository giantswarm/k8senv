package k8senv

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/giantswarm/k8senv/internal/core"
	"k8s.io/client-go/rest"
)

// Singleton state for NewManager. The first call creates the manager;
// subsequent calls return the same instance and log a warning.
//
// singletonMu protects both singletonMgr and singletonOnce so that
// resetForTesting (used in tests) is concurrency-safe with NewManager.
var (
	singletonMu   sync.Mutex
	singletonMgr  Manager
	singletonOnce sync.Once
)

// Compile-time interface satisfaction checks.
var (
	_ Manager  = (*managerWrapper)(nil)
	_ Instance = (*instanceWrapper)(nil)
)

// managerWrapper wraps core.Manager to implement the Manager interface.
// It serves as the concrete singleton implementation returned by NewManager.
// This allows returning Instance interface from Acquire instead of *core.Instance.
//
// The core.Manager is stored as a named (unexported) field rather than embedded
// to prevent callers from using type assertions to access internal methods
// (e.g., IsShuttingDown, ReleaseToPool) that are not part of the public Manager interface.
type managerWrapper struct {
	mgr *core.Manager
}

// Initialize wraps core.Manager.Initialize.
func (w *managerWrapper) Initialize(ctx context.Context) error {
	return w.mgr.Initialize(ctx)
}

// Acquire implements Manager.Acquire, returning Instance interface.
//
//nolint:ireturn // Returns Instance interface by design for testability (mockable).
func (w *managerWrapper) Acquire(ctx context.Context) (Instance, error) {
	inst, token, err := w.mgr.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	return &instanceWrapper{inst: inst, token: token}, nil
}

// Shutdown wraps core.Manager.Shutdown.
func (w *managerWrapper) Shutdown() error {
	return w.mgr.Shutdown()
}

// instanceWrapper wraps core.Instance to implement the Instance interface.
//
// The core.Instance is stored as a named (unexported) field rather than embedded
// to prevent callers from using type assertions to access internal methods
// that are not part of the public Instance interface.
//
// released tracks whether Release has been called on this wrapper. It prevents
// Config from returning stale data after Release, enforcing the contract that
// Config must only be called between Acquire and Release. The underlying
// core.Instance also checks its generation counter, but that check is tied to
// pool-level state (the instance may be re-acquired by another consumer). The
// wrapper-level flag provides a definitive per-acquisition guard.
type instanceWrapper struct {
	inst     *core.Instance
	token    uint64
	released atomic.Bool
}

// Config returns *rest.Config for connecting to this instance's kube-apiserver.
//
// Returns ErrInstanceReleased if called after Release. The check uses a
// wrapper-level atomic flag that is set once by Release, providing a
// definitive per-acquisition guard independent of pool-level state.
func (w *instanceWrapper) Config() (*rest.Config, error) {
	if w.released.Load() {
		return nil, ErrInstanceReleased
	}
	return w.inst.Config()
}

// Release returns the instance to the pool. The behavior depends on the
// ReleaseStrategy configured on the Manager (see WithReleaseStrategy).
//
// Returns nil on success; using defer inst.Release() is safe. On error the
// instance is already removed from the pool, so no corrective action is needed.
//
// After Release returns, any subsequent call to Config on this wrapper returns
// ErrInstanceReleased.
func (w *instanceWrapper) Release() error {
	w.released.Store(true)
	return w.inst.Release(w.token)
}

// ID returns a unique identifier for this instance.
// Delegates to the underlying core.Instance.
func (w *instanceWrapper) ID() string {
	return w.inst.ID()
}

// defaultManagerConfig returns a managerConfig populated with all default
// values. Both NewManager and test helpers use this to avoid duplicating
// the default field assignments.
func defaultManagerConfig() managerConfig {
	return managerConfig{core.ManagerConfig{
		PoolSize:             DefaultPoolSize,
		ReleaseStrategy:      DefaultReleaseStrategy,
		KineBinary:           DefaultKineBinary,
		KubeAPIServerBinary:  DefaultKubeAPIServerBinary,
		AcquireTimeout:       DefaultAcquireTimeout,
		BaseDataDir:          filepath.Join(os.TempDir(), DefaultBaseDataDirName),
		CRDCacheTimeout:      DefaultCRDCacheTimeout,
		InstanceStartTimeout: DefaultInstanceStartTimeout,
		InstanceStopTimeout:  DefaultInstanceStopTimeout,
		CleanupTimeout:       DefaultCleanupTimeout,
		ShutdownDrainTimeout: DefaultShutdownDrainTimeout,
	}}
}

// resetForTesting resets the singleton state so that the next call to
// NewManager creates a fresh manager. This follows the Go stdlib pattern
// (e.g., net/http/internal) for enabling test isolation within a single
// binary. It must only be called from tests.
func resetForTesting() {
	singletonMu.Lock()
	defer singletonMu.Unlock()

	singletonMgr = nil
	singletonOnce = sync.Once{}
}

// NewManager returns the process-level singleton Manager.
//
// The first call creates the manager with the given options and stores it.
// Subsequent calls return the same instance — options are ignored and a
// warning is logged. This performs no I/O operations; call Initialize
// before Acquire.
//
// The singleton is never reset after Shutdown; callers that need a fresh
// manager must restart the process (or, in tests, use a separate test binary).
//
// Panics if any option receives an invalid value. See individual With*
// functions for constraints.
//
//nolint:ireturn // Returns Manager interface by design for testability (mockable).
func NewManager(opts ...ManagerOption) Manager {
	singletonMu.Lock()
	defer singletonMu.Unlock()

	// created is written inside the Do closure and read after Do returns.
	// sync.Once guarantees the closure completes (happens-before) Do returns,
	// so the write is visible here without additional synchronization.
	created := false
	singletonOnce.Do(func() {
		cfg := defaultManagerConfig()
		for _, opt := range opts {
			opt(&cfg)
		}
		singletonMgr = &managerWrapper{mgr: core.NewManagerWithConfig(cfg.toCoreConfig())}
		created = true
	})
	if !created {
		core.Logger().Warn("NewManager called more than once; returning existing singleton (options ignored)")
	}
	return singletonMgr
}
