//go:build integration

package k8senv_stressclean_test

import (
	"context"
	"fmt"
	"math/rand/v2"
	"testing"

	"github.com/giantswarm/k8senv/tests/internal/testutil"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// TestStressClean exercises the ReleaseClean namespace-cleanup code path under
// high concurrency. Each subtest acquires an instance, verifies it is clean
// (only system namespaces), creates random resources, and releases.
func TestStressClean(t *testing.T) {
	t.Parallel()

	for i := range testutil.StressSubtestCount() {
		t.Run(fmt.Sprintf("worker-%d", i), func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()

			rng := rand.New(rand.NewPCG(uint64(i), 0)) //nolint:gosec // deterministic PRNG for reproducibility

			inst, client := testutil.AcquireWithClient(ctx, t, sharedManager)
			defer inst.Release() //nolint:errcheck // safe to ignore in defer; on failure instance is removed from pool

			stressVerifyCleanInstance(ctx, t, client)

			nsCount := rng.IntN(testutil.StressMaxNS) + 1
			namespaces := make([]string, 0, nsCount)

			for n := range nsCount {
				nsName := testutil.UniqueNS("stress-clean")
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

// stressVerifyCleanInstance asserts that the instance has only system
// namespaces, confirming ReleaseClean removed all user namespaces from the
// previous consumer.
func stressVerifyCleanInstance(ctx context.Context, t *testing.T, client kubernetes.Interface) {
	t.Helper()

	nsList, err := client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list namespaces: %v", err)
	}

	sysNS := testutil.SystemNamespaces()
	for i := range nsList.Items {
		if _, ok := sysNS[nsList.Items[i].Name]; !ok {
			t.Fatalf("instance not clean on acquire: unexpected namespace %q", nsList.Items[i].Name)
		}
	}
}
