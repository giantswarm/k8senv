package core

import (
	"errors"
	"fmt"
	"time"
)

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
	PrepopulateDBPath   string // initial DB path from WithPrepopulateDB
	BaseDataDir         string
	CRDDir              string

	// PoolSize is the maximum number of instances the pool will create.
	// A positive value caps the pool; Acquire blocks when all instances
	// are in use. 0 means unlimited (instances created on demand).
	// Default: 4.
	PoolSize int

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

	// CleanupTimeout is the maximum time for SQLite purge during release.
	// Default: 30 seconds.
	CleanupTimeout time.Duration

	// ShutdownDrainTimeout is the maximum time Shutdown() waits for
	// in-flight ReleaseToPool operations to complete before proceeding
	// with instance teardown. Default: 30 seconds.
	ShutdownDrainTimeout time.Duration
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

	requireNonEmpty(&errs, "kine binary path", c.KineBinary)
	requireNonEmpty(&errs, "kube-apiserver binary path", c.KubeAPIServerBinary)
	requirePositiveDuration(&errs, "acquire timeout", c.AcquireTimeout)
	requireNonEmpty(&errs, "base data directory", c.BaseDataDir)
	requirePositiveDuration(&errs, "instance start timeout", c.InstanceStartTimeout)
	requirePositiveDuration(&errs, "instance stop timeout", c.InstanceStopTimeout)
	requirePositiveDuration(&errs, "cleanup timeout", c.CleanupTimeout)
	requirePositiveDuration(&errs, "CRD cache timeout", c.CRDCacheTimeout)
	requirePositiveDuration(&errs, "shutdown drain timeout", c.ShutdownDrainTimeout)
	if c.PoolSize < 0 {
		errs = append(errs, fmt.Errorf("pool size must not be negative, got %d", c.PoolSize))
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
	// CleanupTimeout is the maximum time for SQLite purge during release.
	CleanupTimeout time.Duration
	// MaxStartRetries is the number of startup attempts before giving up.
	MaxStartRetries int
	// CachedDBPath is the path to a pre-populated SQLite database to copy
	// into the instance's data directory before starting kine. Empty means
	// start with a fresh database.
	CachedDBPath        string
	KineBinary          string
	KubeAPIServerBinary string
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

	requirePositiveDuration(&errs, "start timeout", c.StartTimeout)
	requirePositiveDuration(&errs, "stop timeout", c.StopTimeout)
	requirePositiveDuration(&errs, "cleanup timeout", c.CleanupTimeout)
	if c.MaxStartRetries <= 0 {
		errs = append(errs, fmt.Errorf("max start retries must be greater than 0, got %d", c.MaxStartRetries))
	}
	requireNonEmpty(&errs, "kine binary path", c.KineBinary)
	requireNonEmpty(&errs, "kube-apiserver binary path", c.KubeAPIServerBinary)

	return errors.Join(errs...)
}

// requirePositiveDuration appends a validation error if d is not positive.
func requirePositiveDuration(errs *[]error, name string, d time.Duration) {
	if d <= 0 {
		*errs = append(*errs, fmt.Errorf("%s must be greater than 0, got %s", name, d))
	}
}

// requireNonEmpty appends a validation error if value is empty.
func requireNonEmpty(errs *[]error, name string, value string) {
	if value == "" {
		*errs = append(*errs, errors.New(name+" must not be empty"))
	}
}
