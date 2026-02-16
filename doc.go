// Package k8senv provides a lightweight Kubernetes testing framework with instance pooling.
//
// k8senv manages kube-apiserver instances backed by kine (an etcd-compatible SQLite shim)
// with lazy initialization, allowing parallel tests to share instances while maintaining
// isolation through namespaces.
//
// # Basic Usage
//
//	import "github.com/giantswarm/k8senv"
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
//	defer inst.Release(false) // Returns nil on success; safe to ignore in defer
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
//	        defer inst.Release(false) // Returns nil on success; safe to ignore
//	        // Use unique namespaces for isolation
//	    })
//	}
//
// # API-Only Mode
//
// k8senv runs only kube-apiserver without scheduler or controller-manager.
// This means pods remain Pending and controllers don't reconcile, but this is
// ideal for API server testing (CRDs, RBAC, namespaces, ConfigMaps, Secrets).
package k8senv
