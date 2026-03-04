package fileutil

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/giantswarm/k8senv/internal/sentinel"
)

// defaultFileMode is the permission used when CopyFileOptions.Mode is zero.
const defaultFileMode = os.FileMode(0o644)

// ErrEmptySrc is returned when a source path is empty.
const ErrEmptySrc = sentinel.Error("source path must not be empty")

// ErrEmptyDst is returned when a destination path is empty.
const ErrEmptyDst = sentinel.Error("destination path must not be empty")

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
		return ErrEmptySrc
	}
	if dst == "" {
		return ErrEmptyDst
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
		// Cleanup temp file on error. Go's *os.File.Close is safe to call
		// after a prior Close (poll.FD prevents double-close of the fd).
		if retErr != nil && o.Atomic {
			_ = dstFile.Close()
			_ = os.Remove(writePath)
		}
	}()

	if _, err = io.Copy(dstFile, srcFile); err != nil {
		_ = dstFile.Close()
		return fmt.Errorf("copy: %w", err)
	}

	return finalizeCopy(dstFile, writePath, dst, o.Sync || o.Atomic, o.Atomic)
}

// finalizeCopy syncs (if requested), closes, and renames the destination file.
func finalizeCopy(dstFile *os.File, writePath, dst string, doSync, atomic bool) error {
	if doSync {
		if err := dstFile.Sync(); err != nil {
			_ = dstFile.Close()
			return fmt.Errorf("sync: %w", err)
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
// Stat on the open fd is race-free for src (immune to rename/unlink of the source
// path). The dst check uses os.Stat by path, which is subject to TOCTOU — acceptable
// because callers operate on controlled (non-adversarial) paths.
// Returns false when dst does not exist (the common case for a new copy).
func isSameFile(srcFile *os.File, dstPath string) (bool, error) {
	srcInfo, err := srcFile.Stat()
	if err != nil {
		return false, fmt.Errorf("stat source: %w", err)
	}
	dstInfo, err := os.Stat(dstPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("stat destination: %w", err)
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
