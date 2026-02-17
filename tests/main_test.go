//go:build integration

package k8senv_test

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

// sharedManager is the process-level singleton manager, created once in TestMain
// and shared by all integration tests in this package.
var sharedManager k8senv.Manager

// TestMain configures logging, creates the shared singleton manager, and runs
// all tests. Tests use sharedManager.Acquire() to get individual instances.
func TestMain(m *testing.M) {
	// Parse flags early so testutil.TestParallel() reads the actual -test.parallel value
	// from the command line instead of the default (GOMAXPROCS). m.Run() skips
	// re-parsing when flag.Parsed() is already true.
	flag.Parse()

	testutil.SetupTestLogging()
	testutil.RequireBinariesOrExit()

	tmpDir, err := os.MkdirTemp("", "k8senv-test-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create temp dir: %v\n", err)
		os.Exit(1)
	}

	mgr := k8senv.NewManager(
		k8senv.WithBaseDataDir(tmpDir),
		k8senv.WithAcquireTimeout(5*time.Minute),
		k8senv.WithPoolSize(testutil.TestParallel()),
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
