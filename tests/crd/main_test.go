//go:build integration

package k8senv_crd_test

import (
	"fmt"
	"testing"

	"github.com/giantswarm/k8senv"
	"github.com/giantswarm/k8senv/tests/internal/testutil"
)

var sharedManager k8senv.Manager

func TestMain(m *testing.M) {
	testutil.SetupAndRunWithHook(m, &sharedManager, "k8senv-crd-test-*",
		func(tmpDir string) ([]k8senv.ManagerOption, error) {
			crdDir, err := setupSharedCRDDir(tmpDir)
			if err != nil {
				return nil, fmt.Errorf("set up CRD dir: %w", err)
			}

			return []k8senv.ManagerOption{k8senv.WithCRDDir(crdDir)}, nil
		},
	)
}
