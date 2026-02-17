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
		t.Logf("Cycle %d: Acquiring instance...", i+1)
		startTime := time.Now()

		inst, err := sharedManager.Acquire(ctx)
		if err != nil {
			t.Fatalf("Cycle %d: Acquire failed: %v", i+1, err)
		}

		t.Logf("Cycle %d: Acquired instance %s in %v", i+1, inst.ID(), time.Since(startTime))

		// Verify the instance works.
		// Release before Fatal so the instance is returned to the pool;
		// t.Fatal would skip deferred Release via runtime.Goexit.
		cfg, err := inst.Config()
		if err != nil {
			if relErr := inst.Release(); relErr != nil {
				t.Logf("Cycle %d: release error: %v", i+1, relErr)
			}
			t.Fatalf("Cycle %d: Config failed: %v", i+1, err)
		}

		client, err := kubernetes.NewForConfig(cfg)
		if err != nil {
			if relErr := inst.Release(); relErr != nil {
				t.Logf("Cycle %d: release error: %v", i+1, relErr)
			}
			t.Fatalf("Cycle %d: Client creation failed: %v", i+1, err)
		}

		_, err = client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
		if err != nil {
			if relErr := inst.Release(); relErr != nil {
				t.Logf("Cycle %d: release error: %v", i+1, relErr)
			}
			t.Fatalf("Cycle %d: API call failed: %v", i+1, err)
		}

		t.Logf("Cycle %d: API operations successful", i+1)

		// Release — ReleaseRestart stops the instance.
		// This is the core behavior under test; a failure here must fail the test.
		if err := inst.Release(); err != nil {
			t.Errorf("Cycle %d: release error: %v", i+1, err)
		}
		t.Logf("Cycle %d: Released instance (restart strategy)", i+1)
	}
}
