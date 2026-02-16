//go:build integration

package k8senv_poolsize_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/giantswarm/k8senv"
	"github.com/giantswarm/k8senv/tests/internal/testutil"
)

// sharedManager is the process-level singleton manager for pool-size tests,
// created once in TestMain with WithPoolSize(2) to exercise bounded-pool behavior.
var sharedManager k8senv.Manager

// TestMain configures logging, creates a singleton manager with a bounded pool
// (size 2), and runs all tests in this package.
func TestMain(m *testing.M) {
	testutil.SetupTestLogging()
	testutil.RequireBinariesOrExit()

	tmpDir, err := os.MkdirTemp("", "k8senv-poolsize-test-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create temp dir: %v\n", err)
		os.Exit(1)
	}

	mgr := k8senv.NewManager(
		k8senv.WithBaseDataDir(tmpDir),
		k8senv.WithAcquireTimeout(5*time.Minute),
		k8senv.WithPoolSize(2),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	if err := mgr.Initialize(ctx); err != nil {
		cancel()
		fmt.Fprintf(os.Stderr, "Initialize failed: %v\n", err)
		os.Exit(1)
	}
	cancel()

	sharedManager = mgr

	os.Exit(testutil.RunTestMain(m, mgr, tmpDir))
}
