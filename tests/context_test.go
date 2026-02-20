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

	// Try to acquire with already-canceled context
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := sharedManager.Acquire(canceledCtx)
	if err == nil {
		t.Fatal("Expected error from Acquire with canceled context")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled error, got: %v", err)
	}
}
