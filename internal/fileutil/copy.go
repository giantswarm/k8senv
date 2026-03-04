package fileutil

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// defaultFileMode is the permission used when CopyFileOptions.Mode is zero.
const defaultFileMode = os.FileMode(0o644)

// CopyFileOptions configures file copy behavior.
type CopyFileOptions struct {
	Mode   os.FileMode // Optional: 0 means use default 0644
	Sync   bool        // If true, call Sync() before closing dst
	Atomic bool        // If true, write to a temp file then rename to dst (implies Sync for durability)
}

// CopyFile copies a file from src to dst, creating parent directories as needed.
// If opts is nil, uses default behavior (no chmod, no sync, no atomic).
// It returns an error if src or dst is empty.
//
// If src and dst refer to the same underlying file (checked via os.SameFile),
// CopyFile returns nil without performing any I/O.
//
// The destination file is created with the target permissions via os.OpenFile,
// avoiding a window where the file has broader permissions than intended.
// If opts.Mode is set, that mode is used; otherwise defaults to 0644.
//
// When opts.Atomic is true, data is written to a temporary file in the same
// directory as dst, then renamed to dst. On POSIX systems rename is atomic,
// preventing concurrent readers from observing a partially-written file.
func CopyFile(src, dst string, opts *CopyFileOptions) (retErr error) {
	if src == "" {
		return fmt.Errorf("source path: %w", ErrEmptyPath)
	}
	if dst == "" {
		return fmt.Errorf("destination path: %w", ErrEmptyPath)
	}

	srcFile, err := os.Open(src) //nolint:gosec // G304: paths are from controlled sources
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer func() {
		if closeErr := srcFile.Close(); closeErr != nil && retErr == nil {
			retErr = fmt.Errorf("close source: %w", closeErr)
		}
	}()

	// Guard against copying a file onto itself. On the non-atomic path,
	// opening dst with O_TRUNC would truncate src before reading it.
	same, err := isSameFile(srcFile, dst)
	if err != nil {
		return err
	}
	if same {
		return nil
	}

	// Ensure parent directory exists.
	if err := EnsureDirForFile(dst); err != nil {
		return fmt.Errorf("prepare destination: %w", err)
	}

	// Normalize options to avoid nil checks throughout.
	var o CopyFileOptions
	if opts != nil {
		o = *opts
	}

	dstFile, writePath, err := openDstFile(dst, resolveFileMode(o.Mode), o.Atomic)
	if err != nil {
		return err
	}

	defer func() {
		// Only remove temp files on error. On the non-atomic path,
		// O_TRUNC already destroyed the original content — removing
		// the partial file would lose any data that was written.
		if retErr != nil && o.Atomic {
			_ = os.Remove(writePath)
		}
	}()

	if _, err = io.Copy(dstFile, srcFile); err != nil {
		_ = dstFile.Close()
		return fmt.Errorf("copy data: %w", err)
	}

	return finalizeCopy(dstFile, writePath, dst, o.Sync, o.Atomic)
}

// finalizeCopy syncs (if requested), closes, and renames the destination file.
// It always closes dstFile before returning (even on sync error), so the
// caller must not close the file again.
func finalizeCopy(dstFile *os.File, writePath, dst string, doSync, atomic bool) error {
	if doSync || atomic {
		if err := dstFile.Sync(); err != nil {
			_ = dstFile.Close()
			return fmt.Errorf("sync destination: %w", err)
		}
	}

	if err := dstFile.Close(); err != nil {
		return fmt.Errorf("close destination: %w", err)
	}

	if atomic {
		if err := os.Rename(writePath, dst); err != nil {
			return fmt.Errorf("rename temp file to destination: %w", err)
		}
	}

	return nil
}

// resolveFileMode returns the given mode, defaulting to 0o644 when zero.
func resolveFileMode(mode os.FileMode) os.FileMode {
	if mode != 0 {
		return mode
	}
	return defaultFileMode
}

// isSameFile reports whether the open file srcFile refers to the same file as dstPath.
// It stats dst first so the common case (new destination) returns without an fstat on src.
// Stat on the open fd is race-free for src (immune to rename/unlink of the source
// path). The dst check uses os.Stat by path, which is subject to TOCTOU — acceptable
// because callers operate on controlled (non-adversarial) paths.
func isSameFile(srcFile *os.File, dstPath string) (bool, error) {
	dstInfo, err := os.Stat(dstPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("stat destination: %w", err)
	}
	srcInfo, err := srcFile.Stat()
	if err != nil {
		return false, fmt.Errorf("stat source: %w", err)
	}
	return os.SameFile(srcInfo, dstInfo), nil
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
			_ = os.Remove(writePath) //nolint:gosec // G703: writePath is from os.CreateTemp, not user input
			return nil, "", fmt.Errorf("chmod temp file: %w", err)
		}
		return tmpFile, writePath, nil
	}

	f, err := os.OpenFile( //nolint:gosec // G304: paths are from controlled sources
		dst,
		os.O_WRONLY|os.O_CREATE|os.O_TRUNC,
		mode,
	)
	if err != nil {
		return nil, "", fmt.Errorf("create destination: %w", err)
	}
	return f, dst, nil
}
