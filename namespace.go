package k8senv

import "github.com/giantswarm/k8senv/internal/core"

// SystemNamespaceNames returns the names of namespaces created by
// kube-apiserver that must never be deleted during cleanup (default,
// kube-system, kube-public, kube-node-lease). The returned slice is a
// copy; callers may modify it without affecting internal state.
func SystemNamespaceNames() []string {
	return core.SystemNamespaceNames()
}
