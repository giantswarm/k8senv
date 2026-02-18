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
var (
	singletonMgr  Manager
	singletonOnce sync.Once
	mgrCallCount  atomic.Int64
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
//nolint:ireturn // Returns Instance interface by design for testability (mockable)
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
// to prevent callers from using type assertions to access internal lifecycle methods
// (e.g., Start, Stop, IsStarted, IsBusy) that are not part of the public Instance interface.
type instanceWrapper struct {
	inst  *core.Instance
	token uint64
}

// Config returns *rest.Config for connecting to this instance's kube-apiserver.
//
// Must be called while the instance is acquired; see Instance.Config for the
// concurrency contract and TOCTOU discussion.
func (w *instanceWrapper) Config() (*rest.Config, error) {
	return w.inst.Config()
}

// Release returns the instance to the pool. The behavior depends on the
// ReleaseStrategy configured on the Manager (see WithReleaseStrategy).
//
// Returns nil on success; using defer inst.Release() is safe. On error the
// instance is already removed from the pool, so no corrective action is needed.
func (w *instanceWrapper) Release() error {
	return w.inst.Release(w.token)
}

// ID returns a unique identifier for this instance.
// Delegates to the underlying core.Instance.
func (w *instanceWrapper) ID() string {
	return w.inst.ID()
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
//nolint:ireturn // Returns Manager interface by design for testability (mockable)
func NewManager(opts ...ManagerOption) Manager {
	callNum := mgrCallCount.Add(1)
	singletonOnce.Do(func() {
		cfg := managerConfig{core.ManagerConfig{
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
		for _, opt := range opts {
			opt(&cfg)
		}
		singletonMgr = &managerWrapper{mgr: core.NewManagerWithConfig(cfg.toCoreConfig())}
	})
	if callNum > 1 {
		core.Logger().Warn("NewManager called more than once; returning existing singleton (options ignored)")
	}
	return singletonMgr
}
