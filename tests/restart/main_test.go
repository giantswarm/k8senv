//go:build integration

package k8senv_restart_test

import (
	"context"
	"flag"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/giantswarm/k8senv"
	"github.com/giantswarm/k8senv/tests/internal/testutil"
)

// sharedManager is the process-level singleton manager for restart tests,
// created once in TestMain with the default strategy (ReleaseRestart).
var sharedManager k8senv.Manager

// TestMain configures logging, creates a singleton manager with the default
// ReleaseRestart strategy, and runs all tests in this package.
func TestMain(m *testing.M) {
	// Parse flags early so testutil.TestParallel() reads the actual -test.parallel value
	// from the command line instead of the default (GOMAXPROCS). m.Run() skips
	// re-parsing when flag.Parsed() is already true.
	flag.Parse()

	testutil.SetupTestLogging()
	testutil.RequireBinariesOrExit()

	tmpDir, err := os.MkdirTemp("", "k8senv-restart-test-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create temp dir: %v\n", err)
		os.Exit(1)
	}

	mgr := k8senv.NewManager(
		k8senv.WithBaseDataDir(tmpDir),
		k8senv.WithAcquireTimeout(5*time.Minute),
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
