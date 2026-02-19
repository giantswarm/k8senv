//go:build integration

package k8senv_crd_test

import (
	"context"
	"testing"

	"github.com/giantswarm/k8senv"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// verifyCRDExists checks that a CRD with the given name exists.
func verifyCRDExists(ctx context.Context, t *testing.T, inst k8senv.Instance, crdName string) {
	t.Helper()

	cfg, err := inst.Config()
	if err != nil {
		t.Fatalf("failed to get config: %v", err)
	}

	extClient, err := apiextensionsclient.NewForConfig(cfg)
	if err != nil {
		t.Fatalf("failed to create apiextensions client: %v", err)
	}

	_, err = extClient.ApiextensionsV1().CustomResourceDefinitions().Get(ctx, crdName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("crd %s not found: %v", crdName, err)
	}
}
