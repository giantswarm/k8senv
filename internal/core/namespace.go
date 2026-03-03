package core

import (
	"context"
	"fmt"
	"slices"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
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

// cleanupQPS is the client-side QPS limit for cleanup clients. Set higher
// than instance.go's user-facing instanceQPS because cleanup issues many
// small deletions in rapid succession and benefits from zero throttling.
// The target is a local, ephemeral kube-apiserver with no external consumers.
const cleanupQPS = 10_000

// cleanupBurst is the client-side burst limit for cleanup clients, matching
// cleanupQPS for the same reason.
const cleanupBurst = 10_000

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
	client, err := i.getOrBuildCleanupClient()
	if err != nil {
		return fmt.Errorf("build cleanup client for namespace readiness: %w", err)
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

// getOrBuildCachedClient deduplicates the cache-check → build-config →
// create-client → CAS-store pattern shared by the cleanup client builder.
// It checks the cache first, builds a high-QPS rest.Config if needed,
// calls build to create the client, and stores it via CAS.
func getOrBuildCachedClient[T any](
	i *Instance,
	get func(*clientCache) T,
	isNil func(T) bool,
	build func(*rest.Config) (T, error),
	set func(*clientCache, T),
	name string,
) (T, error) {
	if cache := i.clients.Load(); cache != nil {
		if v := get(cache); !isNil(v) {
			return v, nil
		}
	}

	cfg, err := i.getOrBuildRestConfig()
	if err != nil {
		var zero T
		return zero, fmt.Errorf("build config for %s: %w", name, err)
	}
	cfg.QPS = cleanupQPS
	cfg.Burst = cleanupBurst

	v, err := build(cfg)
	if err != nil {
		var zero T
		return zero, fmt.Errorf("create %s: %w", name, err)
	}

	if err := i.casClientCache(func(cc *clientCache) *clientCache {
		if !isNil(get(cc)) {
			return nil // another goroutine won the race
		}
		updated := *cc
		set(&updated, v)
		return &updated
	}); err != nil {
		var zero T
		return zero, fmt.Errorf("cache %s: %w", name, err)
	}
	// Return the locally-built client directly instead of re-reading from
	// i.clients.Load(), which could race with a concurrent Stop() that
	// stores nil between the CAS and the Load.
	return v, nil
}

// getOrBuildCleanupClient returns the cached cleanup client or creates one.
// It effectively disables client-side rate limiting (QPS=10000, Burst=10000)
// because the client only targets a local, ephemeral kube-apiserver — no
// shared infrastructure can be overwhelmed.
func (i *Instance) getOrBuildCleanupClient() (*kubernetes.Clientset, error) {
	return getOrBuildCachedClient(i,
		func(cc *clientCache) *kubernetes.Clientset { return cc.clientset },
		func(c *kubernetes.Clientset) bool { return c == nil },
		kubernetes.NewForConfig,
		func(cc *clientCache, c *kubernetes.Clientset) { cc.clientset = c },
		"cleanup client",
	)
}
