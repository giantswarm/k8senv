package k8senv

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/giantswarm/k8senv/internal/core"
	"k8s.io/client-go/rest"
)

// Singleton state for NewManager. The first call creates the manager;
// subsequent calls return the same instance and log a warning.
//
// singletonMu protects singletonMgr, singletonCreated, and singletonCfg
// so that resetForTesting (used in tests) is concurrency-safe with NewManager.
var (
	singletonMu      sync.Mutex
	singletonMgr     Manager
	singletonCreated bool
	singletonCfg     managerConfig
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
//nolint:ireturn // Returns interface by design for testability.
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
// Returns ErrInstanceReleased if called after Release has completed. The check
// uses a wrapper-level atomic flag that is set by Release. If Config and
// Release race, Config may succeed one final time; the underlying instance has
// its own generation guard as defense in depth.
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
// Returns ErrDoubleRelease if called more than once on the same acquisition.
//
// After Release returns, any subsequent call to Config on this wrapper returns
// ErrInstanceReleased.
func (w *instanceWrapper) Release() error {
	// Two-layer release guard:
	//   1. w.released (CAS here) — per-wrapper flag that catches the common case
	//      of a single caller releasing twice. Returns ErrDoubleRelease immediately
	//      without touching pool state.
	//   2. core.Instance.Release(token) — generation-counter CAS inside the core
	//      layer that catches cross-wrapper races where the same instance has been
	//      re-acquired by another consumer. This fires when a stale wrapper somehow
	//      bypasses layer 1 (e.g., two wrappers issued for the same acquisition,
	//      which would be a programming error).
	// Both layers are needed: layer 1 gives a clean error; layer 2 is defense
	// in depth against invariant violations in pool management.
	if !w.released.CompareAndSwap(false, true) {
		return ErrDoubleRelease
	}
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
	return managerConfig{ManagerConfig: core.ManagerConfig{
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
//
// If a manager already exists, Shutdown is called first to stop any running
// kine/kube-apiserver processes. Returns an error if Shutdown fails; the
// singleton state is still reset regardless so tests can proceed.
func resetForTesting() error {
	singletonMu.Lock()
	defer singletonMu.Unlock()

	var shutdownErr error
	if singletonMgr != nil {
		if err := singletonMgr.Shutdown(); err != nil {
			shutdownErr = fmt.Errorf("shutting down existing manager: %w", err)
		}
	}

	singletonMgr = nil
	singletonCreated = false
	singletonCfg = managerConfig{}

	return shutdownErr
}

// NewManager returns the process-level singleton Manager.
//
// The first call creates the manager with the given options and stores it.
// Subsequent calls return the same instance and log a warning. If the
// subsequent call provides options that produce a different configuration
// than the stored singleton, the warning includes which fields differ so
// the caller can identify the conflict.
//
// This performs no I/O operations; call Initialize before Acquire.
//
// The singleton is never reset after Shutdown; callers that need a fresh
// manager must restart the process (or, in tests, use a separate test binary).
//
// Panics if any option receives an invalid value. See individual With*
// functions for constraints.
//
//nolint:ireturn // Returns interface by design for testability.
func NewManager(opts ...ManagerOption) Manager {
	singletonMu.Lock()
	defer singletonMu.Unlock()

	if !singletonCreated {
		cfg := defaultManagerConfig()
		for _, opt := range opts {
			opt(&cfg)
		}
		singletonMgr = &managerWrapper{mgr: core.NewManagerWithConfig(cfg.toCoreConfig())}
		singletonCfg = cfg
		singletonCreated = true
	} else {
		logDuplicateNewManager(opts)
	}
	return singletonMgr
}

// logDuplicateNewManager logs a warning when NewManager is called after the
// singleton has already been created. If opts are provided, it applies them
// to a fresh default config and compares against the stored config, listing
// any fields that differ so the caller can identify conflicting options.
func logDuplicateNewManager(opts []ManagerOption) {
	if len(opts) == 0 {
		core.Logger().Warn("NewManager called more than once; returning existing singleton (options ignored)")
		return
	}

	incoming := defaultManagerConfig()
	for _, opt := range opts {
		opt(&incoming)
	}

	diffs := configDiffs(singletonCfg, incoming)
	if len(diffs) == 0 {
		core.Logger().Warn("NewManager called more than once; returning existing singleton (options ignored)")
		return
	}

	core.Logger().Warn(
		"NewManager called more than once with different options; returning existing singleton (options ignored)",
		"conflicts", strings.Join(diffs, ", "),
	)
}

// configDiffs compares two managerConfig values and returns a human-readable
// description of each field that differs. Returns nil if the configs are equal.
//
// When adding a new field to core.ManagerConfig, add a corresponding diff*
// call below. TestConfigDiffsCoversAllFields (options_test.go) is a canary
// test that fails if a field is added without updating this function.
func configDiffs(stored, incoming managerConfig) []string {
	var diffs []string

	diffInt := func(name string, a, b int) {
		if a != b {
			diffs = append(diffs, fmt.Sprintf("%s: %d != %d", name, a, b))
		}
	}

	diffStr := func(name, a, b string) {
		if a != b {
			diffs = append(diffs, fmt.Sprintf("%s: %q != %q", name, a, b))
		}
	}

	diffDur := func(name string, a, b time.Duration) {
		if a != b {
			diffs = append(diffs, fmt.Sprintf("%s: %s != %s", name, a, b))
		}
	}

	diffInt("PoolSize", stored.PoolSize, incoming.PoolSize)

	if stored.ReleaseStrategy != incoming.ReleaseStrategy {
		diffs = append(diffs, fmt.Sprintf("ReleaseStrategy: %s != %s",
			stored.ReleaseStrategy, incoming.ReleaseStrategy))
	}
	diffStr("KineBinary", stored.KineBinary, incoming.KineBinary)
	diffStr("KubeAPIServerBinary", stored.KubeAPIServerBinary, incoming.KubeAPIServerBinary)
	diffDur("AcquireTimeout", stored.AcquireTimeout, incoming.AcquireTimeout)
	diffStr("PrepopulateDBPath", stored.PrepopulateDBPath, incoming.PrepopulateDBPath)
	diffStr("BaseDataDir", stored.BaseDataDir, incoming.BaseDataDir)
	diffStr("CRDDir", stored.CRDDir, incoming.CRDDir)
	diffDur("CRDCacheTimeout", stored.CRDCacheTimeout, incoming.CRDCacheTimeout)
	diffDur("InstanceStartTimeout", stored.InstanceStartTimeout, incoming.InstanceStartTimeout)
	diffDur("InstanceStopTimeout", stored.InstanceStopTimeout, incoming.InstanceStopTimeout)
	diffDur("CleanupTimeout", stored.CleanupTimeout, incoming.CleanupTimeout)
	diffDur("ShutdownDrainTimeout", stored.ShutdownDrainTimeout, incoming.ShutdownDrainTimeout)

	return diffs
}
