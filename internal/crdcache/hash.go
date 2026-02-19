package crdcache

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"github.com/giantswarm/k8senv/internal/sentinel"
)

// ErrNoYAMLFiles is returned when a CRD directory contains no YAML files.
const ErrNoYAMLFiles = sentinel.Error("no YAML files found")

// hashedFile pairs a file path with its content read during hashing.
// This allows computeDirHash to return file contents alongside the hash,
// so downstream consumers (e.g., applyYAMLFiles) can use the already-read
// bytes instead of reading each file from disk a second time.
type hashedFile struct {
	path    string
	content []byte
}

// computeDirHash computes a deterministic SHA256 hash of all YAML files in a directory.
// Files are sorted by relative path for determinism. Both filenames and contents are hashed.
// Returns the first 16 hex characters (64 bits) of the hash and the file contents so
// callers can reuse them without redundant disk reads.
func computeDirHash(dirPath string) (string, []hashedFile, error) {
	paths, err := walkYAMLFiles(dirPath)
	if err != nil {
		return "", nil, fmt.Errorf("walk dir: %w", err)
	}

	if len(paths) == 0 {
		return "", nil, fmt.Errorf("%w in %s", ErrNoYAMLFiles, dirPath)
	}

	h := sha256.New()
	files := make([]hashedFile, 0, len(paths))

	for _, p := range paths {
		content, readErr := os.ReadFile(p) //nolint:gosec // G304: paths are from controlled directory walking
		if readErr != nil {
			return "", nil, fmt.Errorf("read %s: %w", p, readErr)
		}

		relPath, relErr := filepath.Rel(dirPath, p)
		if relErr != nil {
			return "", nil, fmt.Errorf("rel path: %w", relErr)
		}

		h.Write([]byte(relPath + "\x00")) // hash.Hash.Write never returns an error
		h.Write(content)
		h.Write([]byte{0}) // separator after content to prevent cross-file collisions

		files = append(files, hashedFile{path: p, content: content})
	}

	// Truncate the SHA-256 hex digest to 16 characters (64 bits). This is used
	// as a suffix in cache filenames (e.g., "cached-<hash>.db"), so brevity aids
	// readability in logs and on disk. 64 bits gives a birthday-bound collision
	// probability of ~1 in 2^32 (~4 billion), which is negligible for the small
	// number of distinct CRD directories a project will realistically have.
	return hex.EncodeToString(h.Sum(nil))[:16], files, nil
}
