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
)
