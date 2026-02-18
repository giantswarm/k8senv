//go:build integration

package k8senv_restart_test

import (
	"context"
	"testing"
	"time"

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

	t.Logf("Cycle %d: Acquiring instance...", cycle)
	startTime := time.Now()

	inst, err := sharedManager.Acquire(ctx)
	if err != nil {
		t.Fatalf("Cycle %d: Acquire failed: %v", cycle, err)
	}

	// released tracks whether the explicit Release (the behavior under test)
	// has already been called, so the deferred safety net can skip it.
	released := false
	defer func() {
		if !released {
			inst.Release() //nolint:errcheck,gosec // safety net on test failure
		}
	}()

	t.Logf("Cycle %d: Acquired instance %s in %v", cycle, inst.ID(), time.Since(startTime))

	// Verify the instance works.
	cfg, err := inst.Config()
	if err != nil {
		t.Fatalf("Cycle %d: Config failed: %v", cycle, err)
	}

	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		t.Fatalf("Cycle %d: Client creation failed: %v", cycle, err)
	}

	_, err = client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("Cycle %d: API call failed: %v", cycle, err)
	}

	t.Logf("Cycle %d: API operations successful", cycle)

	// Release — ReleaseRestart stops the instance.
	// This is the core behavior under test; a failure here must fail the test.
	if err := inst.Release(); err != nil {
		t.Errorf("Cycle %d: release error: %v", cycle, err)
	}

	released = true
	t.Logf("Cycle %d: Released instance (restart strategy)", cycle)
}
