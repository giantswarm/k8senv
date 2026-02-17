package core

import (
	"errors"
	"fmt"
	"time"
)

// ReleaseStrategy controls what happens when an Instance is released back to the pool.
type ReleaseStrategy int

const (
	// ReleaseRestart stops the instance without performing any API-level
	// cleanup. The next Acquire starts a fresh instance — kine's Start()
	// copies the SQLite database from the cached template, restoring the
	// database to its pre-test state. This is the safest and simplest
	// strategy: no cleanup code to get wrong, full isolation via DB restore.
	// This is the default strategy.
	ReleaseRestart ReleaseStrategy = iota

	// ReleaseClean cleans all non-system namespaces and their resources
	// but keeps the instance running. The next Acquire reuses the same
	// running instance. Faster than ReleaseRestart (no stop/start cycle)
	// but relies on cleanup correctness for isolation.
	ReleaseClean

	// ReleaseNone performs no cleanup. The instance is returned to the pool
	// as-is with all namespaces and resources intact. Use this only when
	// tests use unique namespaces and never share state, or when cleanup
	// overhead is unacceptable.
	//
	// WARNING: Previous test state persists. The next consumer of this
	// instance will see all namespaces and resources from prior tests.
	// Use unique namespaces (e.g., with test name or UUID prefix) to
	// ensure isolation.
	ReleaseNone
)

// IsValid reports whether s is a recognized ReleaseStrategy value.
func (s ReleaseStrategy) IsValid() bool {
	switch s {
	case ReleaseRestart, ReleaseClean, ReleaseNone:
		return true
	default:
		return false
	}
}

// String returns the name of the strategy.
func (s ReleaseStrategy) String() string {
	switch s {
	case ReleaseRestart:
		return "ReleaseRestart"
	case ReleaseClean:
		return "ReleaseClean"
	case ReleaseNone:
		return "ReleaseNone"
	default:
		return fmt.Sprintf("ReleaseStrategy(%d)", int(s))
	}
}

// ManagerConfig holds configuration for Manager instances.
//
// Concurrency contract: all fields are immutable after construction via
// NewManagerWithConfig. Instance goroutines read KineBinary and
// KubeAPIServerBinary without synchronization, relying on this guarantee.
// The CRD cache path is stored as separate runtime state in
// Manager.cachedDBPath to preserve this immutability contract.
type ManagerConfig struct {
	KineBinary          string
	KubeAPIServerBinary string
	AcquireTimeout      time.Duration
	DefaultDBPath       string // initial DB path from WithPrepopulateDB
	BaseDataDir         string
	CRDDir              string

	// PoolSize is the maximum number of instances the pool will create.
	// A positive value caps the pool; Acquire blocks when all instances
	// are in use. 0 means unlimited (instances created on demand).
	// Default: 4.
	PoolSize int

	// ReleaseStrategy controls what happens when an Instance is released
	// back to the pool. Default: ReleaseRestart.
	ReleaseStrategy ReleaseStrategy

	// CRDCacheTimeout is the overall timeout for CRD cache creation, including
	// spinning up a temporary kine + kube-apiserver, applying CRDs, and copying
	// the resulting database. Readiness timeouts for the temporary processes are
	// derived from this value. Default: 5 minutes.
	CRDCacheTimeout time.Duration

	// InstanceStartTimeout is the maximum time allowed for an instance's
	// kine + kube-apiserver processes to start and become ready. This timeout
	// is used for both kine and apiserver readiness checks.
	// Default: 5 minutes.
	InstanceStartTimeout time.Duration

	// InstanceStopTimeout is the maximum time allowed per-process for an
	// instance's processes to stop gracefully. Each of kube-apiserver and kine
	// independently receives this full timeout for its SIGTERM/SIGKILL shutdown
	// sequence. Since the two processes are stopped sequentially (apiserver
	// first, then kine), the worst-case total stop duration for one instance
	// is 2*InstanceStopTimeout. Default: 10 seconds.
	InstanceStopTimeout time.Duration

	// CleanupTimeout is the maximum time for namespace cleanup during
	// release. This timeout covers API calls to list and delete non-system
	// namespaces. Although only exercised when ReleaseStrategy is
	// ReleaseClean, a positive value is always required by Validate
	// because validation does not vary by strategy. Default: 30 seconds.
	CleanupTimeout time.Duration
}

// Validate checks all ManagerConfig invariants and returns an error describing
// every violation found. It uses errors.Join to report multiple issues at once,
// allowing callers to fix all problems in a single pass rather than playing
// whack-a-mole with one error at a time.
//
// Validate is called by NewManagerWithConfig (which panics on error, since
// invalid config is a programmer error) and by Initialize (which returns the
// error, providing defense in depth).
func (c ManagerConfig) Validate() error {
	var errs []error

	if c.KineBinary == "" {
		errs = append(errs, errors.New("kine binary path must not be empty"))
	}
	if c.KubeAPIServerBinary == "" {
		errs = append(errs, errors.New("kube-apiserver binary path must not be empty"))
	}
	if c.AcquireTimeout <= 0 {
		errs = append(errs, fmt.Errorf("acquire timeout must be greater than 0, got %s", c.AcquireTimeout))
	}
	if c.BaseDataDir == "" {
		errs = append(errs, errors.New("base data directory must not be empty"))
	}
	if c.InstanceStartTimeout <= 0 {
		errs = append(errs, fmt.Errorf("instance start timeout must be greater than 0, got %s", c.InstanceStartTimeout))
	}
	if c.InstanceStopTimeout <= 0 {
		errs = append(errs, fmt.Errorf("instance stop timeout must be greater than 0, got %s", c.InstanceStopTimeout))
	}
	if c.CleanupTimeout <= 0 {
		errs = append(errs, fmt.Errorf("cleanup timeout must be greater than 0, got %s", c.CleanupTimeout))
	}
	if c.CRDCacheTimeout <= 0 {
		errs = append(errs, fmt.Errorf("CRD cache timeout must be greater than 0, got %s", c.CRDCacheTimeout))
	}
	if c.PoolSize < 0 {
		errs = append(errs, fmt.Errorf("pool size must not be negative, got %d", c.PoolSize))
	}
	if !c.ReleaseStrategy.IsValid() {
		errs = append(errs, fmt.Errorf("invalid release strategy: %v", c.ReleaseStrategy))
	}

	return errors.Join(errs...)
}

// InstanceConfig holds configuration for Instance objects.
// All fields are immutable after construction via NewInstance.
type InstanceConfig struct {
	// StartTimeout is the maximum time for kine + kube-apiserver to become ready.
	StartTimeout time.Duration
	// StopTimeout is the maximum time per-process for graceful shutdown.
	// This timeout is passed to [kubestack.Stack.Stop], which applies it
	// independently to each of kube-apiserver and kine. The worst-case
	// total stop duration is therefore 2*StopTimeout.
	StopTimeout time.Duration
	// CleanupTimeout is the maximum time for namespace cleanup during
	// release. Although only exercised when ReleaseStrategy is
	// ReleaseClean, a positive value is always required by Validate.
	CleanupTimeout time.Duration
	// MaxStartRetries is the number of startup attempts before giving up.
	MaxStartRetries int
	// CachedDBPath is the path to a pre-populated SQLite database to copy
	// into the instance's data directory before starting kine. Empty means
	// start with a fresh database.
	CachedDBPath        string
	KineBinary          string
	KubeAPIServerBinary string
	ReleaseStrategy     ReleaseStrategy
}

// Validate checks all InstanceConfig invariants and returns an error describing
// every violation found. It uses errors.Join to report multiple issues at once,
// allowing callers to fix all problems in a single pass rather than playing
// whack-a-mole with one error at a time.
//
// Validate is called by NewInstance (which panics on error, since invalid config
// is a programmer error), providing a single source of truth for validation logic.
func (c InstanceConfig) Validate() error {
	var errs []error

	if c.StartTimeout <= 0 {
		errs = append(errs, fmt.Errorf("start timeout must be greater than 0, got %s", c.StartTimeout))
	}
	if c.StopTimeout <= 0 {
		errs = append(errs, fmt.Errorf("stop timeout must be greater than 0, got %s", c.StopTimeout))
	}
	if c.CleanupTimeout <= 0 {
		errs = append(errs, fmt.Errorf("cleanup timeout must be greater than 0, got %s", c.CleanupTimeout))
	}
	if c.MaxStartRetries <= 0 {
		errs = append(errs, fmt.Errorf("max start retries must be greater than 0, got %d", c.MaxStartRetries))
	}
	if c.KineBinary == "" {
		errs = append(errs, errors.New("kine binary path must not be empty"))
	}
	if c.KubeAPIServerBinary == "" {
		errs = append(errs, errors.New("kube-apiserver binary path must not be empty"))
	}
	if !c.ReleaseStrategy.IsValid() {
		errs = append(errs, fmt.Errorf("invalid release strategy: %v", c.ReleaseStrategy))
	}

	return errors.Join(errs...)
}
