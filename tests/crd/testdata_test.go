//go:build integration

package k8senv_crd_test

import (
	"fmt"
	"os"
	"path/filepath"
)

// testdataDir lives alongside this file; Go sets the test working directory to
// the package directory, so a relative path is correct.
const testdataDir = "testdata"

// setupSharedCRDDir creates a CRD directory under baseDir containing all CRDs
// needed by this package's tests. It copies files from the testdata/ directory
// so that each test run gets its own isolated copy. Returns the path to the CRD
// directory.
func setupSharedCRDDir(baseDir string) (string, error) {
	crdDir := filepath.Join(baseDir, "crds")
	if err := os.MkdirAll(crdDir, 0o750); err != nil {
		return "", fmt.Errorf("mkdir: %w", err)
	}

	// Entry names come from the filesystem, so both the read and the write are
	// confined to their directory with os.Root: it rejects any name that resolves
	// outside the root, whether via "..", an absolute path, or a symlink pointing
	// out of the tree.
	srcRoot, err := os.OpenRoot(testdataDir)
	if err != nil {
		return "", fmt.Errorf("open testdata dir: %w", err)
	}
	defer func() { _ = srcRoot.Close() }()

	dstRoot, err := os.OpenRoot(crdDir)
	if err != nil {
		return "", fmt.Errorf("open CRD dir: %w", err)
	}
	defer func() { _ = dstRoot.Close() }()

	entries, err := os.ReadDir(testdataDir)
	if err != nil {
		return "", fmt.Errorf("read testdata dir: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()

		data, err := srcRoot.ReadFile(name)
		if err != nil {
			return "", fmt.Errorf("read %s: %w", name, err)
		}

		if err := dstRoot.WriteFile(name, data, 0o644); err != nil {
			return "", fmt.Errorf("write %s: %w", name, err)
		}
	}

	return crdDir, nil
}
