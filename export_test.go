package k8senv

import "github.com/giantswarm/k8senv/internal/core"

// ResetForTesting resets the singleton manager state so that the next
// call to NewManager creates a fresh instance. This is exported only
// for use in test packages (package k8senv_test).
//
// If a manager already exists, Shutdown is called first to stop any running
// processes. Returns an error if Shutdown fails; the singleton state is still
// reset regardless so tests can proceed.
func ResetForTesting() error { return resetForTesting() }

// ConfigSnapshot is an alias for core.ManagerConfig, exported for test
// assertions. Using a type alias instead of a parallel struct means new
// fields added to core.ManagerConfig are automatically available without
// manual synchronization.
type ConfigSnapshot = core.ManagerConfig

// ApplyOptionsForTesting creates a default managerConfig, applies the given
// options, and returns the resulting core.ManagerConfig. This tests the option
// closures directly without touching the singleton.
func ApplyOptionsForTesting(opts ...ManagerOption) ConfigSnapshot {
	cfg := defaultManagerConfig()
	for _, opt := range opts {
		opt(&cfg)
	}

	return cfg.ManagerConfig
}
