package crdcache

import (
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func writeTestFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write test file %s: %v", name, err)
	}
}

func TestWalkYAMLFiles_FindsYAML(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeTestFile(t, dir, "a.yaml", "kind: A")
	writeTestFile(t, dir, "b.yml", "kind: B")

	files, err := walkYAMLFiles(dir)
	if err != nil {
		t.Fatalf("walkYAMLFiles() error: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(files))
	}
}

func TestWalkYAMLFiles_IgnoresNonYAML(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeTestFile(t, dir, "crd.yaml", "kind: CRD")
	writeTestFile(t, dir, "readme.md", "# readme")
	writeTestFile(t, dir, "config.json", "{}")
	writeTestFile(t, dir, "script.sh", "#!/bin/bash")

	files, err := walkYAMLFiles(dir)
	if err != nil {
		t.Fatalf("walkYAMLFiles() error: %v", err)
	}
	if len(files) != 1 {
		t.Errorf("expected 1 yaml file, got %d", len(files))
	}
}

func TestWalkYAMLFiles_ReturnsSorted(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeTestFile(t, dir, "c.yaml", "kind: C")
	writeTestFile(t, dir, "a.yaml", "kind: A")
	writeTestFile(t, dir, "b.yaml", "kind: B")

	files, err := walkYAMLFiles(dir)
	if err != nil {
		t.Fatalf("walkYAMLFiles() error: %v", err)
	}
	if len(files) != 3 {
		t.Fatalf("expected 3 files, got %d", len(files))
	}
	for i := 1; i < len(files); i++ {
		if files[i] < files[i-1] {
			t.Errorf("files not sorted: %v", files)
			break
		}
	}
}

func TestWalkYAMLFiles_NestedDirectories(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	subDir := filepath.Join(dir, "subdir")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatalf("create subdir: %v", err)
	}

	writeTestFile(t, dir, "top.yaml", "kind: Top")
	writeTestFile(t, subDir, "nested.yaml", "kind: Nested")

	files, err := walkYAMLFiles(dir)
	if err != nil {
		t.Fatalf("walkYAMLFiles() error: %v", err)
	}
	if len(files) != 2 {
		t.Errorf("expected 2 files, got %d", len(files))
	}
}

func TestWalkYAMLFiles_EmptyDirectory(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	files, err := walkYAMLFiles(dir)
	if err != nil {
		t.Fatalf("walkYAMLFiles() error: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("expected 0 files, got %d", len(files))
	}
}

func TestWalkYAMLFiles_CaseInsensitive(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeTestFile(t, dir, "a.YAML", "kind: A")
	writeTestFile(t, dir, "b.YML", "kind: B")
	writeTestFile(t, dir, "c.Yaml", "kind: C")

	files, err := walkYAMLFiles(dir)
	if err != nil {
		t.Fatalf("walkYAMLFiles() error: %v", err)
	}
	if len(files) != 3 {
		t.Errorf("expected 3 files, got %d", len(files))
	}
}

func TestWalkYAMLFiles_NonexistentDir(t *testing.T) {
	t.Parallel()
	_, err := walkYAMLFiles("/nonexistent/path")
	if err == nil {
		t.Fatal("expected error for nonexistent directory")
	}
}

func isHex(s string) bool {
	_, err := hex.DecodeString(s)
	return err == nil
}

func TestComputeDirHash_ProducesHexHash(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeTestFile(t, dir, "crd.yaml", "apiVersion: v1\nkind: ConfigMap")

	hash, _, err := computeDirHash(dir)
	if err != nil {
		t.Fatalf("computeDirHash() error: %v", err)
	}
	if len(hash) != 16 {
		t.Errorf("hash length = %d, want 16", len(hash))
	}
	if !isHex(hash) {
		t.Errorf("hash %q contains non-hex characters", hash)
	}
}

func TestComputeDirHash_Deterministic(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeTestFile(t, dir, "a.yaml", "content-a")
	writeTestFile(t, dir, "b.yaml", "content-b")

	hash1, _, err := computeDirHash(dir)
	if err != nil {
		t.Fatalf("first computeDirHash() error: %v", err)
	}

	hash2, _, err := computeDirHash(dir)
	if err != nil {
		t.Fatalf("second computeDirHash() error: %v", err)
	}

	if hash1 != hash2 {
		t.Errorf("hashes differ: %q vs %q", hash1, hash2)
	}
}

func TestComputeDirHash_DifferentContent(t *testing.T) {
	t.Parallel()
	dir1 := t.TempDir()
	writeTestFile(t, dir1, "crd.yaml", "content-version-1")

	dir2 := t.TempDir()
	writeTestFile(t, dir2, "crd.yaml", "content-version-2")

	hash1, _, err := computeDirHash(dir1)
	if err != nil {
		t.Fatalf("computeDirHash(dir1) error: %v", err)
	}

	hash2, _, err := computeDirHash(dir2)
	if err != nil {
		t.Fatalf("computeDirHash(dir2) error: %v", err)
	}

	if hash1 == hash2 {
		t.Error("different content should produce different hashes")
	}
}

func TestComputeDirHash_DifferentFilenames(t *testing.T) {
	t.Parallel()
	dir1 := t.TempDir()
	writeTestFile(t, dir1, "first.yaml", "same-content")

	dir2 := t.TempDir()
	writeTestFile(t, dir2, "second.yaml", "same-content")

	hash1, _, err := computeDirHash(dir1)
	if err != nil {
		t.Fatalf("computeDirHash(dir1) error: %v", err)
	}

	hash2, _, err := computeDirHash(dir2)
	if err != nil {
		t.Fatalf("computeDirHash(dir2) error: %v", err)
	}

	if hash1 == hash2 {
		t.Error("different filenames should produce different hashes")
	}
}

func TestComputeDirHash_EmptyDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	_, _, err := computeDirHash(dir)
	if err == nil {
		t.Fatal("expected error for empty directory")
	}
	if !errors.Is(err, ErrNoYAMLFiles) {
		t.Errorf("expected ErrNoYAMLFiles, got: %v", err)
	}
}

func TestComputeDirHash_OnlyNonYAML(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeTestFile(t, dir, "readme.md", "# readme")
	writeTestFile(t, dir, "config.json", "{}")

	_, _, err := computeDirHash(dir)
	if err == nil {
		t.Fatal("expected error for directory with no YAML files")
	}
	if !errors.Is(err, ErrNoYAMLFiles) {
		t.Errorf("expected ErrNoYAMLFiles, got: %v", err)
	}
}

func TestComputeDirHash_NonexistentDir(t *testing.T) {
	t.Parallel()
	_, _, err := computeDirHash("/nonexistent/path")
	if err == nil {
		t.Fatal("expected error for nonexistent directory")
	}
}

func TestComputeDirHash_MultipleFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeTestFile(t, dir, "crd1.yaml", "apiVersion: v1\nkind: CRD1")
	writeTestFile(t, dir, "crd2.yaml", "apiVersion: v1\nkind: CRD2")
	writeTestFile(t, dir, "crd3.yml", "apiVersion: v1\nkind: CRD3")

	hash, _, err := computeDirHash(dir)
	if err != nil {
		t.Fatalf("computeDirHash() error: %v", err)
	}
	if len(hash) != 16 {
		t.Errorf("hash length = %d, want 16", len(hash))
	}
}

func TestWalkYAMLFiles_SkipsHiddenDirectories(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Create a visible subdirectory with a YAML file.
	visibleDir := filepath.Join(dir, "visible")
	if err := os.MkdirAll(visibleDir, 0o755); err != nil {
		t.Fatalf("create visible dir: %v", err)
	}
	writeTestFile(t, visibleDir, "crd.yaml", "kind: Visible")

	// Create a hidden subdirectory with a YAML file that should be skipped.
	hiddenDir := filepath.Join(dir, ".hidden")
	if err := os.MkdirAll(hiddenDir, 0o755); err != nil {
		t.Fatalf("create hidden dir: %v", err)
	}
	writeTestFile(t, hiddenDir, "secret.yaml", "kind: Hidden")

	// Create a nested hidden directory to verify recursive skipping.
	nestedHiddenDir := filepath.Join(dir, "visible", ".nested-hidden")
	if err := os.MkdirAll(nestedHiddenDir, 0o755); err != nil {
		t.Fatalf("create nested hidden dir: %v", err)
	}
	writeTestFile(t, nestedHiddenDir, "nested.yaml", "kind: NestedHidden")

	files, err := walkYAMLFiles(dir)
	if err != nil {
		t.Fatalf("walkYAMLFiles() error: %v", err)
	}

	// Only the file in the visible directory should be found.
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d: %v", len(files), files)
	}
	if filepath.Base(files[0]) != "crd.yaml" {
		t.Errorf("expected crd.yaml, got %s", filepath.Base(files[0]))
	}
}

func TestComputeDirHash_ContentChangeProducesDifferentHash(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeTestFile(t, dir, "crd.yaml", "original-content")

	hashBefore, _, err := computeDirHash(dir)
	if err != nil {
		t.Fatalf("computeDirHash() before: %v", err)
	}

	// Overwrite the same file with different content.
	writeTestFile(t, dir, "crd.yaml", "modified-content")

	hashAfter, _, err := computeDirHash(dir)
	if err != nil {
		t.Fatalf("computeDirHash() after: %v", err)
	}

	if hashBefore == hashAfter {
		t.Error("changing file contents should produce a different hash")
	}
}
