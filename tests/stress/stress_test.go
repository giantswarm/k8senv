//go:build integration

package k8senv_stress_test

import (
	"context"
	"fmt"
	"math/rand/v2"
	"testing"

	"github.com/giantswarm/k8senv/tests/internal/testutil"
)

// TestStress spawns parallel subtests that each acquire an instance,
// create random namespaces and resources, verify them, and release.
func TestStress(t *testing.T) {
	t.Parallel()

	for i := range testutil.StressSubtestCount() {
		t.Run(fmt.Sprintf("worker-%d", i), func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()

			rng := rand.New(rand.NewPCG(uint64(i), 0)) //nolint:gosec // deterministic PRNG for reproducibility

			inst, client := testutil.AcquireWithClient(ctx, t, sharedManager)
			defer inst.Release() //nolint:errcheck // safe to ignore in defer; on failure instance is removed from pool

			nsCount := rng.IntN(testutil.StressMaxNS) + 1
			namespaces := make([]string, 0, nsCount)

			for n := range nsCount {
				nsName := testutil.UniqueName("stress")
				testutil.StressCreateNamespace(ctx, t, client, nsName)
				namespaces = append(namespaces, nsName)

				resCount := rng.IntN(testutil.StressMaxRes) + 1
				for r := range resCount {
					idx := n*testutil.StressMaxRes + r
					testutil.StressCreateRandomResource(ctx, t, client, nsName, idx, rng)
				}
			}

			for _, ns := range namespaces {
				testutil.StressDeleteNamespace(ctx, t, client, ns)
			}
		})
	}
}
