package crdcache

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
)

// walkYAMLFiles returns all YAML files in a directory, sorted for determinism.
func walkYAMLFiles(dirPath string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(dirPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext == ".yaml" || ext == ".yml" {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk directory %s: %w", dirPath, err)
	}
	slices.Sort(files)
	return files, nil
}
