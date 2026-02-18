//go:build integration

package k8senv_poolsize_test

import (
	"testing"

	"github.com/giantswarm/k8senv"
	"github.com/giantswarm/k8senv/tests/internal/testutil"
)

var sharedManager k8senv.Manager

func TestMain(m *testing.M) {
	testutil.SetupAndRun(m, &sharedManager, "k8senv-poolsize-test-*",
		k8senv.WithPoolSize(2),
	)
}
