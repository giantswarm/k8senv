package core

import (
	"context"
	"fmt"
	"slices"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
)

// systemNamespaces lists the namespaces created by kube-apiserver that must
// never be deleted during cleanup. Declared as a fixed-size array to prevent
// accidental append via the built-in append function, which requires a slice.
// isSystemNamespace and SystemNamespaceNames are derived from it.
var systemNamespaces = [...]string{
	"default",
	"kube-system",
	"kube-public",
	"kube-node-lease",
}

// SystemNamespaceNames returns the names of namespaces created by
// kube-apiserver that must never be deleted during cleanup. The returned
// slice is a copy; callers may modify it without affecting internal state.
func SystemNamespaceNames() []string {
	return append([]string(nil), systemNamespaces[:]...)
}

// isSystemNamespace reports whether name is a namespace created by
// kube-apiserver that must never be deleted during cleanup. These namespaces
// are required for the instance to function correctly on reuse.
func isSystemNamespace(name string) bool {
	return slices.Contains(systemNamespaces[:], name)
}

// internalQPS is the client-side QPS limit for internal clients (namespace
// readiness polling). Set high to avoid client-go rate limiting against the
// local, ephemeral kube-apiserver with no external consumers.
const internalQPS = 10_000

// internalBurst is the client-side burst limit for internal clients,
// matching internalQPS for the same reason.
const internalBurst = 10_000

// nsReadinessPollInterval is the polling interval for waitForSystemNamespaces.
const nsReadinessPollInterval = 10 * time.Millisecond

// nsReadinessTimeout bounds the wait for system namespaces independently of
// the caller's context, so a very long acquire timeout does not spin here
// indefinitely if something is fundamentally wrong.
const nsReadinessTimeout = 30 * time.Second

// waitForSystemNamespaces polls the kube-apiserver until all 4 system
// namespaces (default, kube-system, kube-public, kube-node-lease) exist.
// Called during startup after /livez passes but before the instance is marked
// as started, to close the gap where /livez returns 200 before the namespace
// controller has created all system namespaces.
func (i *Instance) waitForSystemNamespaces(ctx context.Context) error {
	client, err := i.getOrBuildInternalClient()
	if err != nil {
		return fmt.Errorf("build internal client for namespace readiness: %w", err)
	}

	// Use a local timeout so we don't spin forever, but also respect the
	// caller's context (e.g. acquire timeout).
	pollCtx, cancel := context.WithTimeout(ctx, nsReadinessTimeout)
	defer cancel()

	if err := wait.PollUntilContextCancel(
		pollCtx,
		nsReadinessPollInterval,
		true,
		func(ctx context.Context) (bool, error) {
			nsList, listErr := client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
			if listErr != nil {
				// Transient errors are expected during early startup.
				i.log.Debug("namespace readiness poll error", "error", listErr)
				return false, nil
			}

			found := 0
			for idx := range nsList.Items {
				if isSystemNamespace(nsList.Items[idx].Name) {
					found++
				}
			}
			if found >= len(systemNamespaces) {
				return true, nil
			}

			i.log.Debug("waiting for system namespaces", "found", found, "expected", len(systemNamespaces))
			return false, nil
		},
	); err != nil {
		return fmt.Errorf("poll for system namespaces: %w", err)
	}

	return nil
}

// getOrBuildInternalClient returns the cached internal client or creates one.
// The client is used for namespace readiness polling during startup. It
// effectively disables client-side rate limiting (QPS=10000, Burst=10000)
// because the client only targets a local, ephemeral kube-apiserver — no
// shared infrastructure can be overwhelmed.
func (i *Instance) getOrBuildInternalClient() (*kubernetes.Clientset, error) {
	if cache := i.clients.Load(); cache != nil && cache.clientset != nil {
		return cache.clientset, nil
	}

	cfg, err := i.getOrBuildRestConfig()
	if err != nil {
		return nil, fmt.Errorf("build config for internal client: %w", err)
	}
	cfg.QPS = internalQPS
	cfg.Burst = internalBurst

	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("create internal client: %w", err)
	}

	if err := i.casClientCache(func(cc *clientCache) *clientCache {
		if cc.clientset != nil {
			return nil // another goroutine won the race
		}
		updated := *cc
		updated.clientset = client
		return &updated
	}); err != nil {
		return nil, fmt.Errorf("cache internal client: %w", err)
	}
	// Return the locally-built client directly instead of re-reading from
	// i.clients.Load(), which could race with a concurrent Stop() that
	// stores nil between the CAS and the Load.
	return client, nil
}
