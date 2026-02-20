//go:build integration

package k8senv_crd_test

import (
	"fmt"
	"os"
	"path/filepath"
)

// setupSharedCRDDir creates a CRD directory under baseDir containing all CRDs
// needed by this package's tests. It copies files from the testdata/ directory
// so that each test run gets its own isolated copy. Returns the path to the CRD
// directory.
func setupSharedCRDDir(baseDir string) (string, error) {
	crdDir := filepath.Join(baseDir, "crds")
	if err := os.MkdirAll(crdDir, 0o750); err != nil {
		return "", fmt.Errorf("mkdir: %w", err)
	}

	// testdata/ lives alongside this file; Go sets the test working directory
	// to the package directory, so a relative path is correct.
	srcDir := "testdata"
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return "", fmt.Errorf("read testdata dir: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		src := filepath.Join(srcDir, name)

		data, err := os.ReadFile(src) //nolint:gosec // src is a controlled testdata path
		if err != nil {
			return "", fmt.Errorf("read %s: %w", name, err)
		}

		dst := filepath.Join(crdDir, name)
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return "", fmt.Errorf("write %s: %w", name, err)
		}
	}

	return crdDir, nil
}
