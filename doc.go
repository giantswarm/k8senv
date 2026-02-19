// Package k8senv provides a lightweight Kubernetes testing framework with instance pooling.
//
// k8senv manages kube-apiserver instances backed by kine (an etcd-compatible SQLite shim)
// with lazy initialization, allowing parallel tests to share instances while maintaining
// isolation through namespaces.
//
// # Basic Usage
//
//	import (
//	    "context"
//	    "log"
//
//	    "github.com/giantswarm/k8senv"
//	)
//
//	ctx := context.Background()
//
//	mgr := k8senv.NewManager()
//	if err := mgr.Initialize(ctx); err != nil {
//	    log.Fatal(err)
//	}
//	defer mgr.Shutdown()
//
//	inst, err := mgr.Acquire(ctx)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer inst.Release() // safe to ignore in defer
//
//	cfg, err := inst.Config()
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	// import "k8s.io/client-go/kubernetes"
//	client, err := kubernetes.NewForConfig(cfg)
//	// Use client...
//
// # Parallel Testing
//
// Instances are created on demand. Use Go's -parallel flag to control concurrency:
//
//	mgr := k8senv.NewManager()
//	if err := mgr.Initialize(ctx); err != nil {
//	    t.Fatal(err)
//	}
//	defer mgr.Shutdown()
//
//	for i := 0; i < 10; i++ {
//	    t.Run(fmt.Sprintf("test-%d", i), func(t *testing.T) {
//	        t.Parallel()
//	        inst, err := mgr.Acquire(ctx)
//	        if err != nil {
//	            t.Fatal(err)
//	        }
//	        // Use t.Cleanup instead of defer so Release runs when the
//	        // subtest completes, not when the outer function returns.
//	        t.Cleanup(func() { _ = inst.Release() })
//	        // Use unique namespaces for isolation
//	    })
//	}
//
// # API-Only Mode
//
// k8senv runs only kube-apiserver without scheduler or controller-manager.
// This means pods remain Pending and controllers don't reconcile, but this is
// ideal for API server testing (CRDs, RBAC, namespaces, ConfigMaps, Secrets).
//
// # Logging
//
// By default k8senv derives its logger from [slog.Default]. Call [SetLogger]
// to direct k8senv log output to a custom [log/slog.Logger]:
//
//	k8senv.SetLogger(myLogger.With("component", "k8senv"))
//
// Pass nil to reset to the default behavior.
//
// # Error Handling
//
// The package exports sentinel errors for inspection with [errors.Is].
// Operations wrap these sentinels with additional context, so callers should
// use [errors.Is] rather than direct comparison:
//
//	inst, err := mgr.Acquire(ctx)
//	if err != nil {
//	    switch {
//	    case errors.Is(err, k8senv.ErrNotInitialized):
//	        // Initialize was not called before Acquire
//	    case errors.Is(err, k8senv.ErrShuttingDown):
//	        // Manager is shutting down, stop requesting instances
//	    case errors.Is(err, k8senv.ErrPoolClosed):
//	        // Pool was closed during shutdown
//	    default:
//	        // Unexpected error (timeout, process failure, etc.)
//	    }
//	}
//
// Instance methods return their own sentinels:
//
//	cfg, err := inst.Config()
//	if errors.Is(err, k8senv.ErrInstanceReleased) {
//	    // Instance was already released; Config is no longer valid
//	}
//
// CRD initialization reports sentinels for invalid CRD directories:
//
//	err := mgr.Initialize(ctx)
//	if errors.Is(err, k8senv.ErrNoYAMLFiles) {
//	    // CRD directory contained no YAML files
//	}
//
// See the exported Err variables for the complete list of sentinel errors.
package k8senv
