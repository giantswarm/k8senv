//go:build integration

package k8senv_crd_test

import (
	"context"
	"testing"

	"github.com/giantswarm/k8senv"
	"github.com/giantswarm/k8senv/tests/internal/testutil"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// uniqueNS returns a namespace name unique across all parallel tests.
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

// verifyCRDExists checks that a CRD with the given name exists.
func verifyCRDExists(ctx context.Context, t *testing.T, inst k8senv.Instance, crdName string) {
	t.Helper()

	cfg, err := inst.Config()
	if err != nil {
		t.Fatalf("Failed to get config: %v", err)
	}

	extClient, err := apiextensionsclient.NewForConfig(cfg)
	if err != nil {
		t.Fatalf("Failed to create apiextensions client: %v", err)
	}

	_, err = extClient.ApiextensionsV1().CustomResourceDefinitions().Get(ctx, crdName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("CRD %s not found: %v", crdName, err)
	}
}
