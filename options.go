package k8senv

import (
	"fmt"
	"time"
)

// requirePositive panics if v <= 0 with a descriptive message.
func requirePositive[T int | time.Duration](name string, v T) {
	if v <= 0 {
		panic(fmt.Sprintf("k8senv: %s must be greater than 0, got %v", name, v))
	}
}

// requireNonEmpty panics if s is empty with a descriptive message.
func requireNonEmpty(name, s string) {
	if s == "" {
		panic(fmt.Sprintf("k8senv: %s must not be empty", name))
	}
}

// ManagerOption configures a Manager during construction via NewManager.
// Each With* function returns a ManagerOption that sets a specific field.
//
// Several With* functions panic on invalid input (zero-value sizes, empty paths,
// non-positive durations). These panics are intentional: option values are
// typically compile-time constants or package-level variables, so an invalid
// value indicates a programmer error rather than a runtime condition. The
// pattern mirrors [regexp.MustCompile] — fail fast during initialization
// instead of returning errors that would be universally fatal anyway.
type ManagerOption func(*managerConfig)

// WithPoolSize sets the maximum number of instances the pool will create.
// A positive value caps the pool; Acquire blocks when all instances are in use
// and unblocks when one is released. A value of 0 means unlimited: instances
// are created on demand without an upper bound.
//
// Default: 4.
//
// The acquireTimeout (configured via WithAcquireTimeout) bounds how long
// Acquire can block waiting for a free instance, so set it high enough to
// account for both pool wait time and instance startup (~5-15s).
//
// Panics if size < 0.
func WithPoolSize(size int) ManagerOption {
	if size < 0 {
		panic(fmt.Sprintf("k8senv: pool size must not be negative, got %d", size))
	}
	return func(c *managerConfig) {
		c.PoolSize = size
	}
}

// WithKineBinary sets the path to the kine binary.
// Panics if binPath is empty.
func WithKineBinary(binPath string) ManagerOption {
	requireNonEmpty("kine binary path", binPath)
	return func(c *managerConfig) {
		c.KineBinary = binPath
	}
}

// WithKubeAPIServerBinary sets the path to the kube-apiserver binary.
// Panics if binPath is empty.
func WithKubeAPIServerBinary(binPath string) ManagerOption {
	requireNonEmpty("kube-apiserver binary path", binPath)
	return func(c *managerConfig) {
		c.KubeAPIServerBinary = binPath
	}
}

// WithAcquireTimeout sets the total timeout for Acquire(), covering instance
// startup time. Instance startup typically takes 5-15 seconds.
//
// Default: 30 seconds.
//
// Panics if d <= 0.
func WithAcquireTimeout(d time.Duration) ManagerOption {
	requirePositive("acquire timeout", d)
	return func(c *managerConfig) {
		c.AcquireTimeout = d
	}
}

// WithPrepopulateDB sets a default DB file to prepopulate all instances with.
// Panics if dbPath is empty.
func WithPrepopulateDB(dbPath string) ManagerOption {
	requireNonEmpty("prepopulate DB path", dbPath)
	return func(c *managerConfig) {
		c.DefaultDBPath = dbPath
	}
}

// WithCRDCacheTimeout sets the overall timeout for CRD cache creation.
// This covers spinning up a temporary kine + kube-apiserver, applying the YAML
// files from the CRD directory, waiting for CRDs to become established, and
// copying the resulting database. Readiness timeouts for the temporary processes
// are derived from this value.
//
// Default: 5 minutes.
//
// Panics if d <= 0.
func WithCRDCacheTimeout(d time.Duration) ManagerOption {
	requirePositive("CRD cache timeout", d)
	return func(c *managerConfig) {
		c.CRDCacheTimeout = d
	}
}

// WithInstanceStartTimeout sets the maximum time allowed for an instance's
// kine + kube-apiserver processes to start and become ready. This timeout is
// used for readiness checks on both kine (TCP probe) and kube-apiserver
// (/livez HTTP probe).
//
// Default: 5 minutes.
//
// Panics if d <= 0.
func WithInstanceStartTimeout(d time.Duration) ManagerOption {
	requirePositive("instance start timeout", d)
	return func(c *managerConfig) {
		c.InstanceStartTimeout = d
	}
}

// WithInstanceStopTimeout sets the maximum time allowed for an instance's
// processes to stop gracefully during shutdown or clean release.
//
// Default: 10 seconds.
//
// Panics if d <= 0.
func WithInstanceStopTimeout(d time.Duration) ManagerOption {
	requirePositive("instance stop timeout", d)
	return func(c *managerConfig) {
		c.InstanceStopTimeout = d
	}
}

// WithCRDDir sets a directory containing YAML files to pre-apply to the cached database.
// On manager creation, all YAML files in this directory will be applied to create
// a cached database that all pool instances share.
// The cache is keyed by a hash of the directory contents, so changes to the YAML files
// will automatically trigger a new cache.
// Panics if dirPath is empty.
func WithCRDDir(dirPath string) ManagerOption {
	requireNonEmpty("CRD directory path", dirPath)
	return func(c *managerConfig) {
		c.CRDDir = dirPath
	}
}

// WithBaseDataDir sets the base directory for instance data.
// Useful in CI environments where multiple projects may use k8senv simultaneously
// and need isolated data directories to prevent conflicts.
// If not set, defaults to "/tmp/k8senv".
// Panics if dir is empty.
func WithBaseDataDir(dir string) ManagerOption {
	requireNonEmpty("base data directory", dir)
	return func(c *managerConfig) {
		c.BaseDataDir = dir
	}
}
