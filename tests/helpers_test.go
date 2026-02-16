//go:build integration

package k8senv_test

import (
	"context"
	"testing"

	"github.com/giantswarm/k8senv"
	"github.com/giantswarm/k8senv/tests/internal/testutil"
	"k8s.io/client-go/kubernetes"
)

// uniqueNS returns a namespace name that is unique across all parallel tests.
func uniqueNS(prefix string) string {
	return testutil.UniqueNS(prefix)
}

// testParallel returns the effective -test.parallel value for the current test binary.
func testParallel() int {
	return testutil.TestParallel()
}

// acquireWithClient acquires an instance, gets its config, and creates a kubernetes client.
// Returns the instance and client. The caller is responsible for releasing the instance.
//
//nolint:ireturn // Test helper returns Instance and kubernetes.Interface matching the public API.
func acquireWithClient(ctx context.Context, t *testing.T, mgr k8senv.Manager) (k8senv.Instance, kubernetes.Interface) {
	t.Helper()

	return testutil.AcquireWithClient(ctx, t, mgr)
}
