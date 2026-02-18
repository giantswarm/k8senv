//go:build integration

package k8senv_stressclean_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/giantswarm/k8senv/tests/internal/testutil"
)

// TestStressClean exercises the ReleaseClean namespace-cleanup code path under
// high concurrency. Each subtest acquires an instance, verifies it is clean
// (only system namespaces), creates random resources, and releases.
func TestStressClean(t *testing.T) {
	t.Parallel()

	for i := range testutil.StressSubtestCount() {
		t.Run(fmt.Sprintf("worker-%d", i), func(t *testing.T) {
			t.Parallel()
			testutil.StressWorker(context.Background(), t, sharedManager, i, "stress-clean")
		})
	}
}
