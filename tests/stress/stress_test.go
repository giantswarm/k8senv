//go:build integration

package k8senv_stress_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/giantswarm/k8senv/tests/internal/testutil"
)

// TestStress spawns parallel subtests that each acquire an instance, verify it
// is clean (only system namespaces), create random resources, and release.
func TestStress(t *testing.T) {
	t.Parallel()

	for i := range testutil.StressSubtestCount(t) {
		t.Run(fmt.Sprintf("worker-%d", i), func(t *testing.T) {
			t.Parallel()
			testutil.StressWorker(context.Background(), t, sharedManager, i, "stress")
		})
	}
}
