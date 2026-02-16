package k8senv

import "github.com/giantswarm/k8senv/internal/core"

// managerConfig holds configuration for a Manager. This unexported type wraps
// core.ManagerConfig via embedding, keeping internal/core types out of the
// public API signature while avoiding field-by-field duplication.
type managerConfig struct {
	core.ManagerConfig
}

// toCoreConfig returns the embedded core.ManagerConfig.
func (c managerConfig) toCoreConfig() core.ManagerConfig {
	return c.ManagerConfig
}
