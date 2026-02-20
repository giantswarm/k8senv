//go:build integration

package k8senv_restart_test

import (
	"context"
	"sync"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// TestReleaseRestart verifies that Release() with ReleaseRestart strategy stops
// the instance and a subsequent Acquire starts a fresh one. Each cycle acquires
// an instance, exercises the API, then releases — the default ReleaseRestart
// strategy stops the instance, forcing a clean restart on next Acquire.
func TestReleaseRestart(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// Multiple acquire/release cycles to test fresh startups.
	// Each Release() stops the instance (ReleaseRestart), so the next
	// Acquire triggers a fresh start.
	for i := range 2 {
		runRestartCycle(t, ctx, i+1)
	}
}

// runRestartCycle runs a single acquire-exercise-release cycle. The function
// boundary gives defer the per-iteration scope needed to guarantee cleanup
// without duplicating release-before-fatal logic at every error check.
func runRestartCycle(t *testing.T, ctx context.Context, cycle int) {
	t.Helper()

	inst, err := sharedManager.Acquire(ctx)
	if err != nil {
		t.Fatalf("cycle %d: acquire failed: %v", cycle, err)
	}

	// releaseOnce ensures the instance is released exactly once. The explicit
	// Release call below is the behavior under test; the deferred call is a
	// safety net that fires only if the test exits early (e.g. via t.Fatalf).
	var releaseOnce sync.Once
	defer func() {
		releaseOnce.Do(func() {
			inst.Release() //nolint:errcheck,gosec // safety net on test failure
		})
	}()

	// Verify the instance works.
	cfg, err := inst.Config()
	if err != nil {
		t.Fatalf("cycle %d: config failed: %v", cycle, err)
	}

	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		t.Fatalf("cycle %d: client creation failed: %v", cycle, err)
	}

	_, err = client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("cycle %d: API call failed: %v", cycle, err)
	}

	// Release — ReleaseRestart stops the instance.
	// This is the core behavior under test; a failure here must fail the test.
	releaseOnce.Do(func() {
		if err := inst.Release(); err != nil {
			t.Errorf("cycle %d: release error: %v", cycle, err)
		}
	})
}
