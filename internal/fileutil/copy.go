package fileutil

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// ErrEmptySrc is returned when a source path is empty.
var ErrEmptySrc = errors.New("source path must not be empty")

// ErrEmptyDst is returned when a destination path is empty.
var ErrEmptyDst = errors.New("destination path must not be empty")

// CopyFileOptions configures file copy behavior.
type CopyFileOptions struct {
	Mode   *os.FileMode // Optional: set specific permissions after copy (ignored on Windows)
	Sync   bool         // If true, call Sync() before closing dst
	Atomic bool         // If true, write to a temp file then rename to dst (prevents partial reads)
}

// CopyFile copies a file from src to dst, creating parent directories as needed.
// If opts is nil, uses default behavior (no chmod, no sync, no atomic).
// It returns an error if src or dst is empty.
//
// The destination file is created with the target permissions atomically via
// os.OpenFile, avoiding a window where the file has broader permissions than
// intended. If opts.Mode is set, that mode is used; otherwise defaults to 0644.
//
// When opts.Atomic is true, data is written to a temporary file in the same
// directory as dst, then renamed to dst. On POSIX systems rename is atomic,
// preventing concurrent readers from observing a partially-written file.
func CopyFile(src, dst string, opts *CopyFileOptions) (retErr error) {
	if src == "" {
		return ErrEmptySrc
	}
	if dst == "" {
		return ErrEmptyDst
	}

	// Ensure parent directory exists.
	if err := EnsureDirForFile(dst); err != nil {
		return fmt.Errorf("prepare destination: %w", err)
	}

	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer func() {
		if closeErr := srcFile.Close(); closeErr != nil && retErr == nil {
			retErr = fmt.Errorf("close source: %w", closeErr)
		}
	}()

	// Normalize options to avoid nil checks throughout.
	var o CopyFileOptions
	if opts != nil {
		o = *opts
	}

	dstFile, writePath, err := openDstFile(dst, resolveFileMode(&o), o.Atomic)
	if err != nil {
		return err
	}

	// closed tracks whether dstFile.Close has been called explicitly.
	// The defer is a safety net for early returns; it skips the close
	// if the file was already closed in the normal path below.
	closed := false
	defer func() {
		if !closed {
			if closeErr := dstFile.Close(); closeErr != nil && retErr == nil {
				retErr = fmt.Errorf("close destination: %w", closeErr)
			}
		}
		if retErr != nil {
			_ = os.Remove(writePath)
		}
	}()

	if _, err = io.Copy(dstFile, srcFile); err != nil {
		return fmt.Errorf("copy: %w", err)
	}

	// Sync data to disk when explicitly requested or when performing an
	// atomic write. For atomic writes, fsync before rename ensures data
	// durability — without it, a crash could leave the renamed file with
	// incomplete contents.
	if o.Sync || o.Atomic {
		if err := dstFile.Sync(); err != nil {
			return fmt.Errorf("sync: %w", err)
		}
	}

	// Close explicitly before rename so the file content is flushed.
	closed = true
	if err := dstFile.Close(); err != nil {
		return fmt.Errorf("close destination: %w", err)
	}

	// Atomic: rename temp file to final destination.
	if writePath != dst {
		if err := os.Rename(writePath, dst); err != nil {
			return fmt.Errorf("rename temp file to destination: %w", err)
		}
	}

	return nil
}

// resolveFileMode returns the file mode from opts, defaulting to 0o644.
func resolveFileMode(opts *CopyFileOptions) os.FileMode {
	if opts.Mode != nil {
		return *opts.Mode
	}
	return 0o644
}

// openDstFile opens the destination file for writing. When atomic is true, it
// creates a temp file in the same directory as dst (with the correct permissions)
// to enable an atomic rename after writing.
func openDstFile(dst string, mode os.FileMode, atomic bool) (*os.File, string, error) {
	if atomic {
		tmpFile, err := os.CreateTemp(filepath.Dir(dst), ".tmp-copy-*")
		if err != nil {
			return nil, "", fmt.Errorf("create temp file: %w", err)
		}
		writePath := tmpFile.Name()
		if err := tmpFile.Chmod(mode); err != nil {
			_ = tmpFile.Close()
			_ = os.Remove(writePath)
			return nil, "", fmt.Errorf("chmod temp file: %w", err)
		}
		return tmpFile, writePath, nil
	}

	f, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return nil, "", fmt.Errorf("create destination: %w", err)
	}
	return f, dst, nil
}
