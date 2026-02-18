// Package fileutil provides file operation utilities for directory and file management.
//
// EnsureDir creates directories recursively, and CopyFile copies files with
// support for explicit permissions, fsync, and atomic writes via temp-file-then-rename.
// These are used throughout k8senv for preparing instance data directories,
// prepopulating SQLite databases, and writing configuration files.
package fileutil
