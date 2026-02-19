package core

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// systemNamespaces lists the namespaces created by kube-apiserver that must
// never be deleted during cleanup. This slice is the single source of truth;
// systemNamespaceCount and isSystemNamespace are derived from it.
var systemNamespaces = []string{
	"default",
	"kube-system",
	"kube-public",
	"kube-node-lease",
}

// systemNamespaceCount is the number of system namespaces.
var systemNamespaceCount = len(systemNamespaces)

// isSystemNamespace reports whether name is a namespace created by
// kube-apiserver that must never be deleted during cleanup. These namespaces
// are required for the instance to function correctly on reuse.
func isSystemNamespace(name string) bool {
	return slices.Contains(systemNamespaces, name)
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

// cleanupQPS is the client-side QPS limit for cleanup clients. Set high to
// effectively disable throttling — the target is a local, ephemeral
// kube-apiserver so there is no shared infrastructure to overwhelm.
const cleanupQPS = 10_000

// cleanupBurst is the client-side burst limit for cleanup clients, matching
// cleanupQPS for the same reason.
const cleanupBurst = 10_000

// gvrCleanupConcurrency is the maximum number of GVR types cleaned
// concurrently by cleanNamespacedResources. Matches the limit used by
// cleanNamespaces (errgroup.SetLimit(10)) and crdApplyConcurrency.
const gvrCleanupConcurrency = 10

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
			if found >= systemNamespaceCount {
				return true, nil
			}

			i.log.Debug("waiting for system namespaces", "found", found, "expected", systemNamespaceCount)
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
// initialUserNS is the already-fetched list of user namespaces from the caller.
// On the first iteration this list is used directly, avoiding a redundant List
// call immediately after the caller's own List in listUserNamespaces. Subsequent
// iterations re-list to confirm convergence.
func (i *Instance) cleanNamespaces(ctx context.Context, initialUserNS []string) error {
	client, err := i.getOrBuildCleanupClient()
	if err != nil {
		return fmt.Errorf("build cleanup client for namespace cleanup: %w", err)
	}

	// Unified cleanup loop: delete any user namespaces found and require
	// cleanupConfirmations consecutive clean List results before returning.
	consecutiveClean := 0

	// userNamespaces holds the working set for the current iteration.
	// Seeded with initialUserNS on iteration 0; re-listed on subsequent ones.
	userNamespaces := initialUserNS

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

		// On the first iteration use the caller-supplied list to avoid a
		// redundant List call — listUserNamespaces already fetched this data.
		// On subsequent iterations re-list to observe post-deletion state.
		if iteration > 0 {
			nsList, listErr := client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
			if listErr != nil {
				return fmt.Errorf("list namespaces for cleanup: %w", listErr)
			}

			userNamespaces = userNamespaces[:0]
			for idx := range nsList.Items {
				name := nsList.Items[idx].Name
				if !isSystemNamespace(name) {
					userNamespaces = append(userNamespaces, name)
				}
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

// cleanNamespacedResources discovers all namespaced resource types on the API
// server and deletes all instances in the given user namespaces. This must run
// before cleanNamespaces to avoid orphaned resources persisting in kine/SQLite
// storage after namespace objects are deleted.
//
// Returns an error only if discovery itself fails. Individual resource
// list/delete failures are logged at Debug level and skipped — some built-in
// types (e.g., events, endpoints) may have API quirks but are harmless since
// they live inside namespaces that will be deleted next.
func (i *Instance) cleanNamespacedResources(ctx context.Context, userNamespaces []string) error {
	gvrs, err := i.discoverDeletableGVRs()
	if err != nil {
		return err
	}

	dynClient, err := i.getOrBuildDynamicClient()
	if err != nil {
		return fmt.Errorf("build dynamic client for resource cleanup: %w", err)
	}

	i.log.Debug("discovered namespaced resource types", "count", len(gvrs))

	// Build the user-namespace set once and share it across all goroutines.
	// The map is read-only after construction, so concurrent access is safe.
	userNSSet := make(map[string]struct{}, len(userNamespaces))
	for _, ns := range userNamespaces {
		userNSSet[ns] = struct{}{}
	}

	g, gCtx := errgroup.WithContext(ctx)
	g.SetLimit(gvrCleanupConcurrency)

	for _, gvr := range gvrs {
		g.Go(func() error {
			i.deleteResourcesForGVR(gCtx, dynClient, gvr, userNSSet)
			return nil
		})
	}

	// Individual GVR failures are logged and swallowed inside
	// deleteResourcesForGVR, so Wait never returns a non-nil error.
	return g.Wait()
}

// discoverDeletableGVRs returns the set of namespaced resource types that
// support both list and delete verbs. Results are cached across Release calls
// since the set of API resources doesn't change (CRDs are pre-applied once
// during initialization and never modified). The cache is invalidated on Stop.
func (i *Instance) discoverDeletableGVRs() ([]schema.GroupVersionResource, error) {
	if cache := i.clients.Load(); cache != nil && cache.gvrs != nil {
		return *cache.gvrs, nil
	}

	disc, err := i.getOrBuildDiscoveryClient()
	if err != nil {
		return nil, fmt.Errorf("build discovery client for resource cleanup: %w", err)
	}

	// ServerPreferredNamespacedResources returns one entry per resource at the
	// preferred version, avoiding double-deleting resources under multiple API
	// versions. It may return partial results alongside an error for groups
	// that fail to load — use whatever we got.
	resourceLists, discErr := disc.ServerPreferredNamespacedResources()
	if discErr != nil && len(resourceLists) == 0 {
		return nil, fmt.Errorf("discover namespaced resources: %w", discErr)
	}

	var gvrs []schema.GroupVersionResource
	for _, list := range resourceLists {
		gv, parseErr := schema.ParseGroupVersion(list.GroupVersion)
		if parseErr != nil {
			i.log.Debug("resource cleanup skipped group", "group_version", list.GroupVersion, "error", parseErr)
			continue
		}
		for idx := range list.APIResources {
			res := &list.APIResources[idx]
			// Skip subresources (e.g., pods/status, pods/log).
			if strings.Contains(res.Name, "/") {
				continue
			}
			// Skip resources that don't support both list and delete.
			if !slices.Contains(res.Verbs, "list") || !slices.Contains(res.Verbs, "delete") {
				continue
			}
			gvrs = append(gvrs, schema.GroupVersionResource{
				Group:    gv.Group,
				Version:  gv.Version,
				Resource: res.Name,
			})
		}
	}

	i.casClientCache(func(cc *clientCache) *clientCache {
		if cc.gvrs != nil {
			return nil // another goroutine won the race
		}
		updated := *cc
		updated.gvrs = &gvrs
		return &updated
	})
	// Return the locally-built slice directly instead of re-reading from
	// i.clients.Load(), which could race with a concurrent Stop() that
	// stores nil between the CAS and the Load.
	return gvrs, nil
}

// deleteResourcesForGVR deletes all instances of the given resource type in the
// provided user namespaces. A cluster-wide List is used as a fast path: if no
// items exist in user namespaces (the common case for ~90% of GVR types), the
// function returns immediately. For GVRs that do have items, DeleteCollection
// is used for batch deletion with a follow-up List for finalizer-stuck resources.
//
// userNSSet is a pre-built read-only set of user namespace names for O(1)
// lookup, constructed once by the caller and shared across goroutines.
func (i *Instance) deleteResourcesForGVR(
	ctx context.Context,
	dynClient *dynamic.DynamicClient,
	gvr schema.GroupVersionResource,
	userNSSet map[string]struct{},
) {
	// Fast path: a single cluster-wide List determines whether any items exist
	// in user namespaces. Most GVR types (configmaps, secrets, pods, etc.) are
	// only populated in a few namespaces, so this avoids the much more expensive
	// per-namespace DeleteCollection + follow-up List for empty GVRs.
	list, err := dynClient.Resource(gvr).Namespace("").List(ctx, metav1.ListOptions{})
	if err != nil {
		i.log.Debug("resource cleanup skipped", "gvr", gvr.String(), "error", err)
		return
	}

	// Identify which user namespaces actually contain items for this GVR.
	nsWithItems := make(map[string]struct{})
	for idx := range list.Items {
		ns := list.Items[idx].GetNamespace()
		if _, ok := userNSSet[ns]; ok {
			nsWithItems[ns] = struct{}{}
		}
	}

	if len(nsWithItems) == 0 {
		return
	}

	i.log.Debug("cleaning namespaced resources", "gvr", gvr.String(), "namespaces_with_items", len(nsWithItems))

	for ns := range nsWithItems {
		i.deleteCollectionInNamespace(ctx, dynClient, gvr, ns)
	}
}

// deleteCollectionInNamespace batch-deletes all resources of a GVR in a single
// namespace. If DeleteCollection is not supported (405 MethodNotAllowed), it
// falls back to listing and deleting items individually. After a successful
// DeleteCollection, it re-lists the namespace to clear any finalizer-stuck
// resources via deleteResourceItem.
func (i *Instance) deleteCollectionInNamespace(
	ctx context.Context,
	dynClient *dynamic.DynamicClient,
	gvr schema.GroupVersionResource,
	ns string,
) {
	res := dynClient.Resource(gvr).Namespace(ns)

	err := res.DeleteCollection(ctx, metav1.DeleteOptions{}, metav1.ListOptions{})
	if apierrors.IsMethodNotSupported(err) {
		// Fallback: list and delete items individually.
		i.deleteItemsInNamespace(ctx, dynClient, gvr, ns)
		return
	}
	if err != nil {
		i.log.Debug("resource cleanup skipped", "gvr", gvr.String(), "namespace", ns, "error", err)
		return
	}

	// Follow-up: find resources stuck due to finalizers and clear them.
	remaining, listErr := res.List(ctx, metav1.ListOptions{})
	if listErr != nil {
		i.log.Debug("resource cleanup follow-up list failed", "gvr", gvr.String(), "namespace", ns, "error", listErr)
		return
	}
	for idx := range remaining.Items {
		i.deleteResourceItem(ctx, dynClient, gvr, &remaining.Items[idx])
	}
}

// deleteItemsInNamespace lists all resources of a GVR in a namespace and
// deletes each one individually via deleteResourceItem. Used as a fallback
// when DeleteCollection is not supported for the resource type.
func (i *Instance) deleteItemsInNamespace(
	ctx context.Context,
	dynClient *dynamic.DynamicClient,
	gvr schema.GroupVersionResource,
	ns string,
) {
	list, err := dynClient.Resource(gvr).Namespace(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		i.log.Debug("resource cleanup skipped", "gvr", gvr.String(), "namespace", ns, "error", err)
		return
	}
	for idx := range list.Items {
		i.deleteResourceItem(ctx, dynClient, gvr, &list.Items[idx])
	}
}

// deleteResourceItem clears finalizers (if any) and deletes a single resource
// item. Errors are logged at Debug level and swallowed — individual item
// failures must not block cleanup of remaining resources.
func (i *Instance) deleteResourceItem(
	ctx context.Context,
	dynClient *dynamic.DynamicClient,
	gvr schema.GroupVersionResource,
	item *unstructured.Unstructured,
) {
	ns := item.GetNamespace()
	name := item.GetName()

	// Clear finalizers if present so the resource can be deleted.
	if len(item.GetFinalizers()) > 0 {
		item.SetFinalizers(nil)
		if _, updateErr := dynClient.Resource(gvr).
			Namespace(ns).
			Update(ctx, item, metav1.UpdateOptions{}); updateErr != nil {
			i.log.Debug(
				"resource cleanup skipped",
				"gvr",
				gvr.String(),
				"namespace",
				ns,
				"name",
				name,
				"error",
				updateErr,
			)
			return
		}
		i.log.Debug("cleared finalizers", "gvr", gvr.String(), "namespace", ns, "name", name)
	}

	if delErr := dynClient.Resource(gvr).Namespace(ns).Delete(ctx, name, metav1.DeleteOptions{}); delErr != nil {
		if !apierrors.IsNotFound(delErr) {
			i.log.Debug(
				"resource cleanup skipped",
				"gvr",
				gvr.String(),
				"namespace",
				ns,
				"name",
				name,
				"error",
				delErr,
			)
		}
	}
}

// getOrBuildCachedClient deduplicates the cache-check → build-config →
// create-client → CAS-store pattern shared by the three cleanup client
// builders. It checks the cache first, builds a high-QPS rest.Config if
// needed, calls build to create the client, and stores it via CAS.
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

	i.casClientCache(func(cc *clientCache) *clientCache {
		if !isNil(get(cc)) {
			return nil // another goroutine won the race
		}
		updated := *cc
		set(&updated, v)
		return &updated
	})
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
		func(cc *clientCache) *kubernetes.Clientset { return cc.cleanup },
		func(c *kubernetes.Clientset) bool { return c == nil },
		kubernetes.NewForConfig,
		func(cc *clientCache, c *kubernetes.Clientset) { cc.cleanup = c },
		"cleanup client",
	)
}

// getOrBuildDiscoveryClient returns the cached discovery client or creates one.
func (i *Instance) getOrBuildDiscoveryClient() (*discovery.DiscoveryClient, error) {
	return getOrBuildCachedClient(i,
		func(cc *clientCache) *discovery.DiscoveryClient { return cc.discovery },
		func(c *discovery.DiscoveryClient) bool { return c == nil },
		discovery.NewDiscoveryClientForConfig,
		func(cc *clientCache, c *discovery.DiscoveryClient) { cc.discovery = c },
		"discovery client",
	)
}

// getOrBuildDynamicClient returns the cached dynamic client or creates one.
func (i *Instance) getOrBuildDynamicClient() (*dynamic.DynamicClient, error) {
	return getOrBuildCachedClient(i,
		func(cc *clientCache) *dynamic.DynamicClient { return cc.dynamic },
		func(c *dynamic.DynamicClient) bool { return c == nil },
		dynamic.NewForConfig,
		func(cc *clientCache, c *dynamic.DynamicClient) { cc.dynamic = c },
		"dynamic client",
	)
}

// listUserNamespaces returns the names of all non-system namespaces on the
// instance's kube-apiserver. It reuses the cached cleanup client (or builds one)
// and is designed as a cheap pre-check before the expensive resource sweep in
// cleanNamespacedResources. Returns nil if no user namespaces exist.
func (i *Instance) listUserNamespaces(ctx context.Context) ([]string, error) {
	client, err := i.getOrBuildCleanupClient()
	if err != nil {
		return nil, fmt.Errorf("build cleanup client for user namespace check: %w", err)
	}

	nsList, err := client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list namespaces for user namespace check: %w", err)
	}

	var names []string
	for idx := range nsList.Items {
		if !isSystemNamespace(nsList.Items[idx].Name) {
			names = append(names, nsList.Items[idx].Name)
		}
	}
	return names, nil
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
