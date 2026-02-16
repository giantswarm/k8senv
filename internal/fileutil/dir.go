package fileutil

import (
	"fmt"
	"os"
	"path/filepath"
)

// EnsureDir creates a directory and all parent directories if they don't exist.
// Uses mode 0755. Returns nil if directory already exists.
func EnsureDir(path string) error {
	if err := os.MkdirAll(path, 0o755); err != nil {
		return fmt.Errorf("create directory %s: %w", path, err)
	}
	return nil
}

// EnsureDirForFile creates the parent directory of filePath if it does not
// already exist, ensuring the file can be created without a missing-directory error.
func EnsureDirForFile(filePath string) error {
	if err := EnsureDir(filepath.Dir(filePath)); err != nil {
		return fmt.Errorf("ensure dir for %s: %w", filePath, err)
	}
	return nil
}
