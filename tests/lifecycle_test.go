//go:build integration

package k8senv_test

import (
	"context"
	"sync"
	"testing"
)

// =============================================================================
// Manager Lifecycle Tests
// =============================================================================

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

	errs := make([]error, 10)
	var wg sync.WaitGroup
	for i := range 10 {
		wg.Go(func() {
			errs[i] = sharedManager.Initialize(context.Background())
		})
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("concurrent Initialize call %d failed: %v", i, err)
		}
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
