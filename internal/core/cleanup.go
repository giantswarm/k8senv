package core

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
)

// systemNamespaces is the set of namespaces that must never be deleted during
// cleanup. These are created by kube-apiserver itself and are required for the
// instance to function correctly on reuse.
var systemNamespaces = map[string]struct{}{
	"default":         {},
	"kube-system":     {},
	"kube-public":     {},
	"kube-node-lease": {},
}

// cleanupConfirmDelay is the short delay before a confirmation re-list when no
// user namespaces are found. This only needs to exceed the watch-cache
// propagation lag (typically <10 ms) to catch stale reads.
const cleanupConfirmDelay = 10 * time.Millisecond

// cleanupConfirmations is the number of consecutive List results that must
// contain only system namespaces before cleanNamespaces considers the instance
// clean. A single confirmation suffices because the watch cache is disabled
// (--watch-cache=false) and all reads go directly through kine to SQLite,
// which is ACID-compliant — reads are immediately consistent after writes.
const cleanupConfirmations = 1

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
//
// As a side effect, the Kubernetes client created here is stored in
// i.cleanupClient for reuse by later cleanNamespaces calls.
func (i *Instance) waitForSystemNamespaces(ctx context.Context) error {
	cfg, err := i.getOrBuildRestConfig()
	if err != nil {
		return fmt.Errorf("build kubeconfig for namespace readiness: %w", err)
	}
	// Disable client-side rate limiting so startup polling is not throttled.
	// This is safe because the client only targets a local, ephemeral
	// kube-apiserver that serves this single test process — there is no
	// shared infrastructure at risk of being overwhelmed.
	cfg.QPS = -1

	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return fmt.Errorf("create client for namespace readiness: %w", err)
	}
	i.cleanupClient.Store(client)

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
				if _, ok := systemNamespaces[nsList.Items[idx].Name]; ok {
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

// cleanNamespaces deletes all non-system namespaces from the instance's
// kube-apiserver. Because k8senv runs in API-only mode (no kube-controller-manager),
// the kubernetes finalizer in spec.Finalizers is never cleared automatically.
// A plain Delete puts namespaces into perpetual "Terminating" state. This method
// explicitly removes finalizers via the Finalize subresource after deletion.
//
// Returns nil immediately if no user namespaces exist (fast path).
func (i *Instance) cleanNamespaces(ctx context.Context) error {
	client := i.cleanupClient.Load()
	if client == nil {
		cfg, err := i.getOrBuildRestConfig()
		if err != nil {
			return fmt.Errorf("build kubeconfig for cleanup: %w", err)
		}

		// Disable client-side rate limiting for internal cleanup operations.
		// The cached Clientset retains its rate limiter across Release calls;
		// with rapid instance reuse the default QPS=5/Burst=10 starves between
		// cleanups, adding ~150 s of throttle waits across the stress test.
		// Safe here because the client only targets a local, ephemeral
		// kube-apiserver — no shared infrastructure can be overwhelmed.
		cfg.QPS = -1

		var clientErr error
		client, clientErr = kubernetes.NewForConfig(cfg)
		if clientErr != nil {
			return fmt.Errorf("create client for cleanup: %w", clientErr)
		}
		i.cleanupClient.Store(client)
	}

	// Unified cleanup loop: delete any user namespaces found and require
	// cleanupConfirmations consecutive clean List results before returning.
	consecutiveClean := 0

	// Reuse the slice across iterations to avoid per-loop heap allocation.
	// Same pattern as crdcache/cache.go pendingCRDs.
	var userNamespaces []string

	// Single timer reused via Reset to avoid per-iteration time.After leaks.
	confirmTimer := time.NewTimer(cleanupConfirmDelay)
	confirmTimer.Stop()
	defer confirmTimer.Stop()

	// Safety valve: cap the number of list-delete-confirm iterations to
	// prevent an unbounded loop if namespace deletion never converges (e.g.
	// finalizer removal races or persistent API errors). The context timeout
	// is the primary safeguard, but an iteration cap provides a deterministic
	// upper bound that fails fast with a clear error message rather than
	// burning the full timeout budget in a tight loop.
	const maxCleanupIterations = 100

	for iteration := 0; ; iteration++ {
		if iteration >= maxCleanupIterations {
			return fmt.Errorf(
				"namespace cleanup did not converge after %d iterations (%d user namespaces remaining)",
				maxCleanupIterations,
				len(userNamespaces),
			)
		}
		nsList, err := client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
		if err != nil {
			return fmt.Errorf("list namespaces for cleanup: %w", err)
		}

		userNamespaces = userNamespaces[:0]
		for idx := range nsList.Items {
			name := nsList.Items[idx].Name
			if _, ok := systemNamespaces[name]; !ok {
				userNamespaces = append(userNamespaces, name)
			}
		}

		if len(userNamespaces) == 0 {
			consecutiveClean++
			if consecutiveClean >= cleanupConfirmations {
				return nil
			}

			// Short delay: only needs to exceed watch-cache propagation lag.
			confirmTimer.Reset(cleanupConfirmDelay)
			select {
			case <-ctx.Done():
				// Drain the timer to avoid leaking it after context cancellation.
				if !confirmTimer.Stop() {
					<-confirmTimer.C
				}
				return fmt.Errorf("context expired waiting for namespace cleanup: %w", ctx.Err())
			case <-confirmTimer.C:
			}
			continue
		}

		// User namespaces found — reset confirmation counter and delete them.
		// No sleep after deletion: the next List round-trip provides natural
		// pacing, matching the old waitForNamespacesDrained behavior.
		consecutiveClean = 0
		i.log.Debug("cleaning user namespaces", "count", len(userNamespaces))

		g, gCtx := errgroup.WithContext(ctx)
		g.SetLimit(10)

		for _, name := range userNamespaces {
			g.Go(func() error {
				if err := deleteAndFinalizeNamespace(gCtx, client, i.log, name); err != nil {
					return fmt.Errorf("clean namespace %s: %w", name, err)
				}
				return nil
			})
		}

		if err := g.Wait(); err != nil {
			return err
		}
	}
}

// deleteAndFinalizeNamespace deletes a namespace and clears its finalizers via
// the Finalize subresource. In API-only mode the kubernetes finalizer is always
// present, so we construct a minimal namespace object directly instead of
// fetching it with Get, saving one HTTP round-trip per namespace.
func deleteAndFinalizeNamespace(ctx context.Context, client kubernetes.Interface, log *slog.Logger, name string) error {
	// Delete with zero grace period to skip any waiting.
	zero := int64(0)
	err := client.CoreV1().Namespaces().Delete(ctx, name, metav1.DeleteOptions{
		GracePeriodSeconds: &zero,
	})
	if apierrors.IsNotFound(err) {
		return nil // already gone
	}
	if err != nil {
		return fmt.Errorf("delete: %w", err)
	}

	// Construct a minimal namespace with no finalizers instead of fetching the
	// full object. The Finalize subresource only needs Name and Spec.Finalizers.
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec:       corev1.NamespaceSpec{Finalizers: nil},
	}
	_, err = client.CoreV1().Namespaces().Finalize(ctx, ns, metav1.UpdateOptions{})
	if apierrors.IsNotFound(err) {
		return nil // deleted between steps
	}
	if err != nil {
		return fmt.Errorf("finalize: %w", err)
	}

	log.Debug("finalized namespace", "namespace", name)
	return nil
}
