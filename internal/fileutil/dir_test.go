package fileutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureDir(t *testing.T) {
	t.Parallel()
	t.Run("creates new directory", func(t *testing.T) {
		t.Parallel()
		base := t.TempDir()
		dir := filepath.Join(base, "newdir")

		if err := EnsureDir(dir); err != nil {
			t.Fatalf("EnsureDir() error: %v", err)
		}

		info, err := os.Stat(dir)
		if err != nil {
			t.Fatalf("stat after EnsureDir: %v", err)
		}
		if !info.IsDir() {
			t.Error("expected directory, got file")
		}
	})

	t.Run("creates nested directories", func(t *testing.T) {
		t.Parallel()
		base := t.TempDir()
		dir := filepath.Join(base, "a", "b", "c")

		if err := EnsureDir(dir); err != nil {
			t.Fatalf("EnsureDir() error: %v", err)
		}

		info, err := os.Stat(dir)
		if err != nil {
			t.Fatalf("stat after EnsureDir: %v", err)
		}
		if !info.IsDir() {
			t.Error("expected directory, got file")
		}
	})

	t.Run("idempotent on existing directory", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()

		if err := EnsureDir(dir); err != nil {
			t.Fatalf("EnsureDir() on existing dir error: %v", err)
		}

		info, err := os.Stat(dir)
		if err != nil {
			t.Fatalf("stat after EnsureDir: %v", err)
		}
		if !info.IsDir() {
			t.Error("expected directory, got file")
		}
	})
}

func TestEnsureDirForFile(t *testing.T) {
	t.Parallel()
	t.Run("creates parent directory", func(t *testing.T) {
		t.Parallel()
		base := t.TempDir()
		filePath := filepath.Join(base, "subdir", "file.txt")

		if err := EnsureDirForFile(filePath); err != nil {
			t.Fatalf("EnsureDirForFile() error: %v", err)
		}

		parentDir := filepath.Dir(filePath)
		info, err := os.Stat(parentDir)
		if err != nil {
			t.Fatalf("stat parent dir: %v", err)
		}
		if !info.IsDir() {
			t.Error("expected parent to be directory")
		}
	})

	t.Run("creates deeply nested parent", func(t *testing.T) {
		t.Parallel()
		base := t.TempDir()
		filePath := filepath.Join(base, "a", "b", "c", "file.txt")

		if err := EnsureDirForFile(filePath); err != nil {
			t.Fatalf("EnsureDirForFile() error: %v", err)
		}

		parentDir := filepath.Dir(filePath)
		info, err := os.Stat(parentDir)
		if err != nil {
			t.Fatalf("stat parent dir: %v", err)
		}
		if !info.IsDir() {
			t.Error("expected parent to be directory")
		}
	})

	t.Run("succeeds when parent already exists", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		filePath := filepath.Join(dir, "file.txt")

		if err := EnsureDirForFile(filePath); err != nil {
			t.Fatalf("EnsureDirForFile() error: %v", err)
		}
	})
}
