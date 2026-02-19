//go:build integration

package k8senv_test

import (
	"context"
	"testing"

	"golang.org/x/sync/errgroup"
)

// TestInitializeIdempotent verifies that calling Initialize multiple times on
// the shared manager is safe and returns nil (no-op after first success).
func TestInitializeIdempotent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// Initialize was already called in TestMain; calling again should be a no-op
	if err := sharedManager.Initialize(ctx); err != nil {
		t.Fatalf("Second Initialize failed: %v", err)
	}

	// Manager should still work
	inst, err := sharedManager.Acquire(ctx)
	if err != nil {
		t.Fatalf("Acquire after double Initialize failed: %v", err)
	}
	if err = inst.Release(); err != nil {
		t.Logf("release error: %v", err)
	}
}

// TestInitializeConcurrent verifies that calling Initialize concurrently on
// the shared manager is safe and all calls return nil.
func TestInitializeConcurrent(t *testing.T) {
	t.Parallel()

	var g errgroup.Group
	for range 10 {
		g.Go(func() error {
			return sharedManager.Initialize(context.Background())
		})
	}
	if err := g.Wait(); err != nil {
		t.Fatalf("concurrent Initialize failed: %v", err)
	}

	// Should be able to acquire after concurrent initializations
	inst, err := sharedManager.Acquire(context.Background())
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}
	if err = inst.Release(); err != nil {
		t.Logf("release error: %v", err)
	}
}
