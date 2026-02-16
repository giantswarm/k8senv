//go:build integration

package k8senv_test

import (
	"context"
	"errors"
	"testing"
)

// TestContextCancelDuringAcquire exercises the ctx.Done() path in pool.Acquire.
func TestContextCancelDuringAcquire(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// Acquire an instance
	inst, err := sharedManager.Acquire(ctx)
	if err != nil {
		t.Fatalf("First Acquire failed: %v", err)
	}
	defer func() {
		if err := inst.Release(false); err != nil {
			t.Logf("release error: %v", err)
		}
	}()

	// Try to acquire with already-canceled context
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err = sharedManager.Acquire(canceledCtx)
	if err == nil {
		t.Fatal("Expected error from Acquire with canceled context")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Expected context.Canceled error, got: %v", err)
	}

	t.Logf("Acquire correctly failed with canceled context: %v", err)
}
