package fileutil

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/giantswarm/k8senv/internal/sentinel"
)

// ErrEmptyPath is returned when a path argument is empty.
const ErrEmptyPath = sentinel.Error("path must not be empty")

// defaultDirMode is the permission used when creating directories.
const defaultDirMode = os.FileMode(0o755)

// EnsureDir creates a directory and all parent directories if they don't exist.
// Uses mode 0755. Returns nil if directory already exists.
func EnsureDir(path string) error {
	if path == "" {
		return fmt.Errorf("create directory: %w", ErrEmptyPath)
	}
	if err := os.MkdirAll(path, defaultDirMode); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}
	return nil
}

// EnsureDirForFile creates the parent directory of filePath if it does not
// already exist, ensuring the file can be created without a missing-directory error.
func EnsureDirForFile(filePath string) error {
	// Guard empty path explicitly: filepath.Dir("") returns "." which
	// would cause EnsureDir to silently succeed on the current directory.
	if filePath == "" {
		return fmt.Errorf("ensure directory for file: %w", ErrEmptyPath)
	}
	return EnsureDir(filepath.Dir(filePath))
}
