package k8senv

import "time"

// ResetForTesting resets the singleton manager state so that the next
// call to NewManager creates a fresh instance. This is exported only
// for use in test packages (package k8senv_test).
func ResetForTesting() { resetForTesting() }

// ConfigSnapshot holds a copy of managerConfig fields for test assertions.
// Exported only via export_test.go so that the _test package can verify
// option closures actually mutate the config without accessing internals.
type ConfigSnapshot struct {
	PoolSize             int
	ReleaseStrategy      ReleaseStrategy
	KineBinary           string
	KubeAPIServerBinary  string
	AcquireTimeout       time.Duration
	DefaultDBPath        string
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
		DefaultDBPath:        cfg.DefaultDBPath,
		CRDDir:               cfg.CRDDir,
		BaseDataDir:          cfg.BaseDataDir,
		CRDCacheTimeout:      cfg.CRDCacheTimeout,
		InstanceStartTimeout: cfg.InstanceStartTimeout,
		InstanceStopTimeout:  cfg.InstanceStopTimeout,
		CleanupTimeout:       cfg.CleanupTimeout,
		ShutdownDrainTimeout: cfg.ShutdownDrainTimeout,
	}
}
