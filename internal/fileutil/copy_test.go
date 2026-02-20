package fileutil

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func createTestFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("create test file: %v", err)
	}
	return path
}

func readDst(t *testing.T, path string) string {
	t.Helper()
	got, err := os.ReadFile(path) //nolint:gosec // G304: path is test-controlled
	if err != nil {
		t.Fatalf("read destination: %v", err)
	}
	return string(got)
}

func TestCopyFile_EmptySourcePath(t *testing.T) {
	t.Parallel()
	dstDir := t.TempDir()
	dst := filepath.Join(dstDir, "dest.txt")

	err := CopyFile("", dst, nil)
	if err == nil {
		t.Fatal("expected error for empty source path, got nil")
	}

	if !errors.Is(err, ErrEmptySrc) {
		t.Errorf("error = %v, want %v", err, ErrEmptySrc)
	}
}

func TestCopyFile_EmptyDestinationPath(t *testing.T) {
	t.Parallel()
	srcDir := t.TempDir()
	src := createTestFile(t, srcDir, "source.txt", "content")

	err := CopyFile(src, "", nil)
	if err == nil {
		t.Fatal("expected error for empty destination path, got nil")
	}

	if !errors.Is(err, ErrEmptyDst) {
		t.Errorf("error = %v, want %v", err, ErrEmptyDst)
	}
}

func TestCopyFile_BothPathsEmpty(t *testing.T) {
	t.Parallel()

	err := CopyFile("", "", nil)
	if err == nil {
		t.Fatal("expected error for empty paths, got nil")
	}

	// Source is validated first, so its error takes precedence.
	if !errors.Is(err, ErrEmptySrc) {
		t.Errorf("error = %v, want %v", err, ErrEmptySrc)
	}
}

func TestCopyFile_BasicCopy(t *testing.T) {
	t.Parallel()
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	content := "hello world"
	src := createTestFile(t, srcDir, "source.txt", content)
	dst := filepath.Join(dstDir, "dest.txt")

	if err := CopyFile(src, dst, nil); err != nil {
		t.Fatalf("CopyFile() error: %v", err)
	}

	if got := readDst(t, dst); got != content {
		t.Errorf("content = %q, want %q", got, content)
	}
}

func TestCopyFile_CreatesParentDirectories(t *testing.T) {
	t.Parallel()
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	content := "nested content"
	src := createTestFile(t, srcDir, "source.txt", content)
	dst := filepath.Join(dstDir, "a", "b", "dest.txt")

	if err := CopyFile(src, dst, nil); err != nil {
		t.Fatalf("CopyFile() error: %v", err)
	}

	if got := readDst(t, dst); got != content {
		t.Errorf("content = %q, want %q", got, content)
	}
}

func TestCopyFile_CustomMode(t *testing.T) {
	t.Parallel()
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	src := createTestFile(t, srcDir, "source.txt", "mode test")
	dst := filepath.Join(dstDir, "dest.txt")

	mode := os.FileMode(0o600)
	if err := CopyFile(src, dst, &CopyFileOptions{Mode: &mode}); err != nil {
		t.Fatalf("CopyFile() error: %v", err)
	}

	info, err := os.Stat(dst)
	if err != nil {
		t.Fatalf("stat destination: %v", err)
	}
	if got := info.Mode().Perm(); got != mode {
		t.Errorf("file mode = %o, want %o", got, mode)
	}
}

func TestCopyFile_SyncOption(t *testing.T) {
	t.Parallel()
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	content := "sync content"
	src := createTestFile(t, srcDir, "source.txt", content)
	dst := filepath.Join(dstDir, "dest.txt")

	if err := CopyFile(src, dst, &CopyFileOptions{Sync: true}); err != nil {
		t.Fatalf("CopyFile() error: %v", err)
	}

	if got := readDst(t, dst); got != content {
		t.Errorf("content = %q, want %q", got, content)
	}
}

func TestCopyFile_AtomicOption(t *testing.T) {
	t.Parallel()
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	content := "atomic content"
	src := createTestFile(t, srcDir, "source.txt", content)
	dst := filepath.Join(dstDir, "dest.txt")

	if err := CopyFile(src, dst, &CopyFileOptions{Atomic: true}); err != nil {
		t.Fatalf("CopyFile() error: %v", err)
	}

	if got := readDst(t, dst); got != content {
		t.Errorf("content = %q, want %q", got, content)
	}
}

func TestCopyFile_AllOptions(t *testing.T) {
	t.Parallel()
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	content := "all options"
	src := createTestFile(t, srcDir, "source.txt", content)
	dst := filepath.Join(dstDir, "dest.txt")

	mode := os.FileMode(0o600)
	if err := CopyFile(src, dst, &CopyFileOptions{
		Mode:   &mode,
		Sync:   true,
		Atomic: true,
	}); err != nil {
		t.Fatalf("CopyFile() error: %v", err)
	}

	if got := readDst(t, dst); got != content {
		t.Errorf("content = %q, want %q", got, content)
	}

	info, err := os.Stat(dst)
	if err != nil {
		t.Fatalf("stat destination: %v", err)
	}
	if got := info.Mode().Perm(); got != mode {
		t.Errorf("file mode = %o, want %o", got, mode)
	}
}

func TestCopyFile_EmptyFile(t *testing.T) {
	t.Parallel()
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	src := createTestFile(t, srcDir, "empty.txt", "")
	dst := filepath.Join(dstDir, "dest.txt")

	if err := CopyFile(src, dst, nil); err != nil {
		t.Fatalf("CopyFile() error: %v", err)
	}

	if got := readDst(t, dst); got != "" {
		t.Errorf("expected empty file, got %d bytes", len(got))
	}
}

func TestCopyFile_LargeContent(t *testing.T) {
	t.Parallel()
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	// 1MB of data
	content := make([]byte, 1024*1024)
	for i := range content {
		content[i] = byte(i % 256)
	}
	src := filepath.Join(srcDir, "large.bin")
	if err := os.WriteFile(src, content, 0o644); err != nil {
		t.Fatalf("create large file: %v", err)
	}
	dst := filepath.Join(dstDir, "dest.bin")

	if err := CopyFile(src, dst, nil); err != nil {
		t.Fatalf("CopyFile() error: %v", err)
	}

	got, err := os.ReadFile(dst) //nolint:gosec // G304: path is test-controlled
	if err != nil {
		t.Fatalf("read destination: %v", err)
	}
	if len(got) != len(content) {
		t.Errorf("content length = %d, want %d", len(got), len(content))
	}
}

func TestCopyFile_SourceNotFound(t *testing.T) {
	t.Parallel()
	dstDir := t.TempDir()
	dst := filepath.Join(dstDir, "dest.txt")

	err := CopyFile("/nonexistent/source.txt", dst, nil)
	if err == nil {
		t.Fatal("expected error for nonexistent source")
	}
}

func TestCopyFile_OverwritesExisting(t *testing.T) {
	t.Parallel()
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	src := createTestFile(t, srcDir, "source.txt", "new content")
	_ = createTestFile(t, dstDir, "dest.txt", "old content")
	dst := filepath.Join(dstDir, "dest.txt")

	if err := CopyFile(src, dst, nil); err != nil {
		t.Fatalf("CopyFile() error: %v", err)
	}

	if got := readDst(t, dst); got != "new content" {
		t.Errorf("content = %q, want %q", got, "new content")
	}
}

func TestCopyFile_SameFileViaSymlink(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Create the real file.
	src := createTestFile(t, dir, "source.txt", "original")

	// Create a symlink directory that points at the same directory.
	symlinkDir := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(dir, symlinkDir); err != nil {
		t.Skipf("symlinks not supported on this platform: %v", err)
	}

	// dst is the same inode as src, but reached through the symlink.
	dst := filepath.Join(symlinkDir, "source.txt")

	// CopyFile should detect the same underlying file and return nil without
	// performing any I/O, preserving the original content.
	if err := CopyFile(src, dst, nil); err != nil {
		t.Fatalf("CopyFile() error: %v", err)
	}

	if got := readDst(t, src); got != "original" {
		t.Errorf("content after self-copy = %q, want %q", got, "original")
	}
}

func TestCopyFile_AtomicNoTempFiles(t *testing.T) {
	t.Parallel()
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	src := createTestFile(t, srcDir, "source.txt", "content")
	dst := filepath.Join(dstDir, "dest.txt")

	if err := CopyFile(src, dst, &CopyFileOptions{Atomic: true}); err != nil {
		t.Fatalf("CopyFile() error: %v", err)
	}

	entries, err := os.ReadDir(dstDir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 1 {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Errorf("expected 1 file in dst dir, got %d: %v", len(entries), names)
	}
}
