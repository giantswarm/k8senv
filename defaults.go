package k8senv

import "time"

// Default configuration values for NewManager.
// These constants are exported so callers can reference the defaults
// when building custom configurations relative to them (e.g.,
// 2 * DefaultAcquireTimeout).
const (
	// DefaultPoolSize is the maximum number of instances the pool will create.
	// Acquire blocks when all instances are in use and unblocks when one is
	// released. Set to 0 for unlimited (on-demand creation without bound).
	DefaultPoolSize = 4

	// DefaultKineBinary is the binary name used to locate kine in PATH.
	DefaultKineBinary = "kine"

	// DefaultKubeAPIServerBinary is the binary name used to locate
	// kube-apiserver in PATH.
	DefaultKubeAPIServerBinary = "kube-apiserver"

	// DefaultAcquireTimeout is the total time allowed for pool acquisition
	// and instance startup. Under pool contention, increase this to account
	// for both wait time and startup (~5-15 seconds).
	DefaultAcquireTimeout = 30 * time.Second

	// DefaultBaseDataDirName is the directory name under the system temp
	// directory where instance data is stored. The full path is computed
	// as filepath.Join(os.TempDir(), DefaultBaseDataDirName).
	DefaultBaseDataDirName = "k8senv"

	// DefaultCRDCacheTimeout is the overall timeout for CRD cache creation,
	// including spinning up a temporary kine + kube-apiserver, applying CRDs,
	// and copying the resulting database.
	DefaultCRDCacheTimeout = 5 * time.Minute

	// DefaultInstanceStartTimeout is the maximum time allowed for an
	// instance's kine + kube-apiserver processes to start and become ready.
	DefaultInstanceStartTimeout = 5 * time.Minute

	// DefaultInstanceStopTimeout is the maximum time allowed for an
	// instance's processes to stop gracefully.
	DefaultInstanceStopTimeout = 10 * time.Second

	// DefaultCleanupTimeout is the maximum time allowed for namespace
	// cleanup during release. This timeout covers API calls to list and
	// delete non-system namespaces, which may need more time than process
	// shutdown (StopTimeout). Although only exercised when ReleaseStrategy
	// is ReleaseClean, a positive value is always required because config
	// validation does not vary by strategy.
	DefaultCleanupTimeout = 30 * time.Second

	// DefaultShutdownDrainTimeout is the maximum time Shutdown() waits
	// for in-flight ReleaseToPool operations to complete before proceeding.
	// If InstanceStopTimeout is configured larger than this value (e.g. for
	// slow CI), an in-flight release performing ReleaseRestart could exceed
	// the drain window, causing Shutdown() to proceed prematurely. Increase
	// this timeout to at least match the longest expected release duration.
	DefaultShutdownDrainTimeout = 30 * time.Second

	// DefaultReleaseStrategy is the strategy used by Instance.Release()
	// when no explicit strategy is configured via WithReleaseStrategy.
	// ReleaseRestart stops the instance on release; the next Acquire
	// starts fresh with the database restored from the cached template.
	DefaultReleaseStrategy = ReleaseRestart
)
