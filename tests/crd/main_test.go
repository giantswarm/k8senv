//go:build integration

package k8senv_crd_test

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/giantswarm/k8senv"
	"github.com/giantswarm/k8senv/tests/internal/testutil"
)

// sharedManager is the process-level singleton manager for CRD tests, created
// once in TestMain with all CRD files needed by this package's tests.
var sharedManager k8senv.Manager

// TestMain configures logging, sets up a CRD directory with all CRDs needed by
// this package, creates the singleton manager with WithCRDDir, and runs tests.
func TestMain(m *testing.M) {
	// Parse flags early so testParallel() reads the actual -test.parallel value
	// from the command line instead of the default (GOMAXPROCS). m.Run() skips
	// re-parsing when flag.Parsed() is already true.
	flag.Parse()

	testutil.SetupTestLogging()
	testutil.RequireBinariesOrExit()

	tmpDir, err := os.MkdirTemp("", "k8senv-crd-test-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create temp dir: %v\n", err)
		os.Exit(1)
	}

	// Create the shared CRD directory containing all CRDs needed by tests.
	crdDir, err := setupSharedCRDDir(tmpDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to set up CRD dir: %v\n", err)
		os.Exit(1)
	}

	mgr := k8senv.NewManager(
		k8senv.WithBaseDataDir(tmpDir),
		k8senv.WithAcquireTimeout(5*time.Minute),
		k8senv.WithCRDDir(crdDir),
		k8senv.WithPoolSize(testParallel()),
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

// setupSharedCRDDir creates a CRD directory under baseDir containing all CRDs
// needed by this package's tests. Returns the path to the CRD directory.
func setupSharedCRDDir(baseDir string) (string, error) {
	crdDir := filepath.Join(baseDir, "crds")
	if err := os.MkdirAll(crdDir, 0o750); err != nil {
		return "", fmt.Errorf("mkdir: %w", err)
	}

	files := map[string]string{
		"widget-crd.yaml":  sampleCRDWidget,
		"gadget-crd.yaml":  sampleCRDGadget,
		"gizmo-crd.yaml":   sampleCRDGizmo,
		"multi.yaml":       sampleMultiDoc,
		"sprocket-crd.yml": sampleCRDSprocket, // exercises .yml extension
	}

	for name, content := range files {
		if err := os.WriteFile(filepath.Join(crdDir, name), []byte(content), 0o644); err != nil {
			return "", fmt.Errorf("write %s: %w", name, err)
		}
	}

	return crdDir, nil
}
