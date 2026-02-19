package k8senv

import (
	"reflect"
	"time"

	"github.com/giantswarm/k8senv/internal/core"
)

// ManagerConfigFieldCount returns the number of fields in core.ManagerConfig.
// Exported for canary tests that verify configDiffs coverage.
func ManagerConfigFieldCount() int {
	return reflect.TypeFor[core.ManagerConfig]().NumField()
}

// ConfigDiffsForTesting calls configDiffs with two configs built from the
// provided option sets and returns the diff strings. This allows the _test
// package to exercise configDiffs without accessing unexported types.
func ConfigDiffsForTesting(storedOpts, incomingOpts []ManagerOption) []string {
	stored := defaultManagerConfig()
	for _, opt := range storedOpts {
		opt(&stored)
	}

	incoming := defaultManagerConfig()
	for _, opt := range incomingOpts {
		opt(&incoming)
	}

	return configDiffs(stored, incoming)
}

// ResetForTesting resets the singleton manager state so that the next
// call to NewManager creates a fresh instance. This is exported only
// for use in test packages (package k8senv_test).
//
// If a manager already exists, Shutdown is called first to stop any running
// processes. Returns an error if Shutdown fails; the singleton state is still
// reset regardless so tests can proceed.
func ResetForTesting() error { return resetForTesting() }

// ConfigSnapshot holds a copy of managerConfig fields for test assertions.
// Exported only via export_test.go so that the _test package can verify
// option closures actually mutate the config without accessing internals.
type ConfigSnapshot struct {
	PoolSize             int
	ReleaseStrategy      ReleaseStrategy
	KineBinary           string
	KubeAPIServerBinary  string
	AcquireTimeout       time.Duration
	PrepopulateDBPath    string
	CRDDir               string
	BaseDataDir          string
	CRDCacheTimeout      time.Duration
	InstanceStartTimeout time.Duration
	InstanceStopTimeout  time.Duration
	CleanupTimeout       time.Duration
	ShutdownDrainTimeout time.Duration
}

// ApplyOptionsForTesting creates a default managerConfig, applies the given
// options, and returns a ConfigSnapshot of the result. This tests the option
// closures directly without touching the singleton.
func ApplyOptionsForTesting(opts ...ManagerOption) ConfigSnapshot {
	cfg := defaultManagerConfig()
	for _, opt := range opts {
		opt(&cfg)
	}

	return ConfigSnapshot{
		PoolSize:             cfg.PoolSize,
		ReleaseStrategy:      cfg.ReleaseStrategy,
		KineBinary:           cfg.KineBinary,
		KubeAPIServerBinary:  cfg.KubeAPIServerBinary,
		AcquireTimeout:       cfg.AcquireTimeout,
		PrepopulateDBPath:    cfg.PrepopulateDBPath,
		CRDDir:               cfg.CRDDir,
		BaseDataDir:          cfg.BaseDataDir,
		CRDCacheTimeout:      cfg.CRDCacheTimeout,
		InstanceStartTimeout: cfg.InstanceStartTimeout,
		InstanceStopTimeout:  cfg.InstanceStopTimeout,
		CleanupTimeout:       cfg.CleanupTimeout,
		ShutdownDrainTimeout: cfg.ShutdownDrainTimeout,
	}
}
