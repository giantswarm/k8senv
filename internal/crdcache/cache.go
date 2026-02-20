package crdcache

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/giantswarm/k8senv/internal/fileutil"
	"github.com/giantswarm/k8senv/internal/kubestack"
	"github.com/giantswarm/k8senv/internal/netutil"
	"github.com/giantswarm/k8senv/internal/process"
	"github.com/giantswarm/k8senv/internal/sentinel"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// Config holds configuration for CRD cache initialization.
type Config struct {
	CRDDir              string                // Directory containing YAML files
	CacheDir            string                // Directory to store cached databases
	KineBinary          string                // Path to kine binary
	KubeAPIServerBinary string                // Path to kube-apiserver binary
	Timeout             time.Duration         // Overall timeout for cache creation
	StopTimeout         time.Duration         // Timeout for stopping the temporary kube stack (zero uses 10s default)
	PortRegistry        *netutil.PortRegistry // Shared port registry for cross-instance coordination
	Logger              *slog.Logger          // Logger for operational messages (nil uses slog.Default)
}

// logger returns the configured logger or falls back to the default.
func (c Config) logger() *slog.Logger {
	if c.Logger != nil {
		return c.Logger
	}
	return slog.Default()
}

// stopTimeout returns the configured stop timeout or the default.
func (c Config) stopTimeout() time.Duration {
	if c.StopTimeout > 0 {
		return c.StopTimeout
	}
	return process.DefaultStopTimeout
}

// validate checks that all required Config fields are set and returns an error
// describing every violation found. It uses errors.Join to report multiple
// issues at once, allowing callers to fix all problems in a single pass rather
// than playing whack-a-mole with one error at a time.
func (c Config) validate() error {
	var errs []error

	if c.CRDDir == "" {
		errs = append(errs, errors.New("crd dir must not be empty"))
	}
	if c.CacheDir == "" {
		errs = append(errs, errors.New("cache dir must not be empty"))
	}
	if c.KineBinary == "" {
		errs = append(errs, errors.New("kine binary path must not be empty"))
	}
	if c.KubeAPIServerBinary == "" {
		errs = append(errs, errors.New("kube-apiserver binary path must not be empty"))
	}
	if c.Timeout <= 0 {
		errs = append(errs, errors.New("timeout must be positive"))
	}
	if c.PortRegistry == nil {
		errs = append(errs, errors.New("port registry must not be nil"))
	}

	return errors.Join(errs...)
}

// Result contains the outcome of cache initialization.
type Result struct {
	CachePath string // Path to the cached database file
	Hash      string // Hash of the CRD directory contents
	Created   bool   // true if cache was created, false if existing cache was used
}

// EnsureCache checks for an existing cache or creates one.
// If a cache with matching content hash exists, it returns immediately.
// Otherwise, it creates a new cache by spinning up a temporary kine + kube-apiserver,
// applying the YAML files, and copying the resulting database.
func EnsureCache(ctx context.Context, cfg Config) (*Result, error) {
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	// Compute hash of CRD directory contents and collect file contents.
	// The files (with their contents) are threaded through to applyYAMLFiles
	// to avoid both a redundant directory walk and redundant disk reads.
	hash, files, err := computeDirHash(cfg.CRDDir)
	if err != nil {
		return nil, fmt.Errorf("compute dir hash: %w", err)
	}

	cachePath := filepath.Join(cfg.CacheDir, fmt.Sprintf("cached-%s.db", hash))

	logger := cfg.logger()

	// Check if cache already exists
	if _, err := os.Stat(cachePath); err == nil {
		logger.Info("using existing CRD cache", "cache_path", cachePath, "hash", hash)
		return &Result{CachePath: cachePath, Hash: hash, Created: false}, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("stat cache file %s: %w", cachePath, err)
	}

	// Acquire file lock for inter-process coordination.
	// In-process serialization is guaranteed by the caller (manager initialization
	// runs once via sync.Once).
	lockPath := cachePath + ".lock"
	logger.Debug("acquiring cache lock", "lock_path", lockPath)
	lock, err := acquireFileLock(ctx, lockPath)
	if err != nil {
		return nil, fmt.Errorf("acquire lock: %w", err)
	}
	defer releaseFileLock(logger, lock)

	// Re-check cache (another process might have created it while we waited for lock)
	if _, err := os.Stat(cachePath); err == nil {
		logger.Info("using existing CRD cache (created while waiting)", "cache_path", cachePath, "hash", hash)
		return &Result{CachePath: cachePath, Hash: hash, Created: false}, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("stat cache file %s after lock acquisition: %w", cachePath, err)
	}

	// Create cache
	logger.Info("creating CRD cache", "crd_dir", cfg.CRDDir, "hash", hash)
	if err := createCache(ctx, cfg, cachePath, files); err != nil {
		return nil, fmt.Errorf("create cache: %w", err)
	}

	return &Result{CachePath: cachePath, Hash: hash, Created: true}, nil
}

// createCache spins up a temporary kine + kube-apiserver, applies CRDs, and copies the DB.
// It creates a temp directory, delegates to populateCache for the core work, and handles
// cleanup of the temp directory unconditionally.
// The files slice contains pre-read YAML file contents from computeDirHash.
func createCache(ctx context.Context, cfg Config, cachePath string, files []hashedFile) error {
	logger := cfg.logger()
	startTime := time.Now()

	// Create temp directory for this cache build
	tempDir, err := os.MkdirTemp(cfg.CacheDir, "crd-cache-build-")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer func() {
		if rmErr := os.RemoveAll(tempDir); rmErr != nil {
			logger.Debug("failed to remove temp dir", "dir", tempDir, "err", rmErr)
		}
	}()

	if err := populateCache(ctx, cfg, tempDir, cachePath, files); err != nil {
		return err
	}

	logger.Info("CRD cache created", "cache_path", cachePath, "elapsed", time.Since(startTime).Round(time.Millisecond))
	return nil
}

// populateCache performs the core cache-building work: starts a temporary kube stack,
// applies CRDs, waits for establishment, and copies the resulting database.
// The files slice contains pre-read YAML file contents from computeDirHash.
//
// The stack is always stopped exactly once via a sync.Once: the success path
// calls stopStack to flush kine writes before copying the database, and the
// deferred cleanup also calls stopStack so error paths are covered. The
// sync.Once guarantees the stop runs at most once regardless of code path.
func populateCache(ctx context.Context, cfg Config, tempDir, cachePath string, files []hashedFile) error {
	logger := cfg.logger()

	sqlitePath := filepath.Join(tempDir, "db", "state.db")
	kubeconfigPath := filepath.Join(tempDir, "kubeconfig.yaml")

	// Create timeout context for cache creation operations.
	// Uses a distinct name to avoid shadowing the parent ctx.
	timeoutCtx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()

	// Create process lifetime context derived from the timeout context so that
	// cancellation propagates to the temporary kube stack processes. Unlike
	// long-lived pool instances (which use context.Background to outlive
	// Acquire), this stack is ephemeral and must stop when the caller cancels
	// or the timeout expires.
	//
	// Deriving from timeoutCtx (rather than the parent ctx) provides an
	// additional safety net: if the cache creation timeout expires, procCtx
	// is automatically canceled, ensuring processes are stopped even without
	// explicit stopStack() calls.
	procCtx, procCancel := context.WithCancel(timeoutCtx)
	defer procCancel()

	// Start kube stack (kine + apiserver) with retry logic for transient
	// port conflicts. Each retry creates a fresh kubestack with new port
	// allocations. Per-process readiness timeouts use the overall cache
	// timeout since processes start concurrently and the timeout context
	// gates the total duration.
	readyTimeout := cfg.Timeout
	stack, err := kubestack.StartWithRetry(procCtx, timeoutCtx, kubestack.Config{
		DataDir:               tempDir,
		SQLitePath:            sqlitePath,
		KubeconfigPath:        kubeconfigPath,
		KineBinary:            cfg.KineBinary,
		APIServerBinary:       cfg.KubeAPIServerBinary,
		KineReadyTimeout:      readyTimeout,
		APIServerReadyTimeout: readyTimeout,
		StopTimeout:           cfg.stopTimeout(),
		PortRegistry:          cfg.PortRegistry,
		Logger:                logger,
	}, maxKubestackStartRetries)
	if err != nil {
		return fmt.Errorf("start kubestack for CRD cache: %w", err)
	}

	// stopStack stops the kube stack exactly once. Both the explicit call on
	// the success path (to flush kine writes before copying the DB) and the
	// deferred cleanup (for error paths) invoke this. sync.Once guarantees
	// the stop executes at most once, eliminating the risk of double-stop if
	// future code adds a return path between the explicit stop and defer.
	var stopOnce sync.Once
	var stopErr error
	stopStack := func() error {
		stopOnce.Do(func() {
			logger.Debug("stopping kube stack")
			if stopResult := stack.Stop(cfg.stopTimeout()); stopResult != nil {
				stopErr = fmt.Errorf("stop kube stack: %w", stopResult)
			}
		})
		return stopErr
	}
	defer func() {
		if stopCleanupErr := stopStack(); stopCleanupErr != nil {
			logger.Debug("failed to stop kube stack during cleanup", "err", stopCleanupErr)
		}
	}()

	// Apply CRDs and wait for establishment.
	if err := applyCRDs(timeoutCtx, logger, kubeconfigPath, cfg.CRDDir, files); err != nil {
		return err
	}

	// Stop the stack explicitly to flush kine writes before copying the DB.
	if err := stopStack(); err != nil {
		return err
	}

	// Copy the database now that kine writes are flushed.
	if err := fileutil.CopyFile(
		sqlitePath,
		cachePath,
		&fileutil.CopyFileOptions{Sync: true, Atomic: true},
	); err != nil {
		return fmt.Errorf("copy db to cache: %w", err)
	}

	return nil
}

// applyCRDs applies CRD YAML files and waits for all CRDs to reach the
// Established condition. It does not manage the kube stack lifecycle — the
// caller is responsible for starting and stopping the stack.
// The files slice contains pre-read YAML file contents from computeDirHash.
func applyCRDs(ctx context.Context, logger *slog.Logger, kubeconfigPath, crdDir string, files []hashedFile) error {
	// Get rest.Config from kubeconfig
	restCfg, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		return fmt.Errorf("build rest config: %w", err)
	}

	// Apply all YAML files
	logger.Info("applying YAML files", "dir", crdDir)
	if err := applyYAMLFiles(ctx, logger, restCfg, crdDir, files); err != nil {
		return fmt.Errorf("apply yaml files: %w", err)
	}

	// Wait for all CRDs to be established.
	// Without this, the cached DB may contain CRDs in "Installing" state
	// that never transition to "Established" when loaded by a fresh apiserver.
	if err := waitForCRDsEstablished(ctx, logger, restCfg); err != nil {
		return fmt.Errorf("wait for CRDs established: %w", err)
	}

	return nil
}

// maxKubestackStartRetries is the number of startup retries for transient failures
// such as port conflicts when starting a temporary kubestack for CRD caching.
const maxKubestackStartRetries = 3

// crdEstablishmentPollInterval is the interval between consecutive checks
// for CRD establishment status.
//
// Relationship to crdEstablishmentQPS / crdEstablishmentBurst:
// At 100ms intervals the polling loop issues ~10 requests/second, well within
// crdEstablishmentQPS (50 req/s). The burst allowance (100) lets the first
// request fire immediately without waiting for a token-bucket refill. If the
// poll interval were reduced below 20ms (>50 req/s), the client-go rate
// limiter would begin throttling and dominate the effective poll rate.
const crdEstablishmentPollInterval = 100 * time.Millisecond

// crdEstablishmentQPS is the client-go QPS override used when polling
// for CRD establishment. The default client-go QPS of 5 is far too low
// for a local, single-user kube-apiserver: at crdEstablishmentPollInterval
// (100ms = 10 req/s) the rate limiter would throttle every other request.
// 50 QPS provides comfortable headroom without risk, since the target is
// a localhost test server.
const crdEstablishmentQPS = 50

// crdEstablishmentBurst is the client-go burst override paired with
// crdEstablishmentQPS. It allows the first burst of requests to proceed
// without delay. The value (100) is 2x QPS, following client-go convention
// for short-lived, non-abusive callers.
const crdEstablishmentBurst = 100

// longWaitThreshold is the duration after which waitForCRDsEstablished logs a
// warning to help users diagnose slow CRD establishment. 10 seconds is well
// above the typical establishment time (<2s for local apiserver) while still
// catching genuinely stuck CRDs before the overall cache timeout expires.
const longWaitThreshold = 10 * time.Second

// ErrCRDEstablishTimeout is returned when CRDs do not reach the Established
// condition before the context deadline expires.
const ErrCRDEstablishTimeout = sentinel.Error("crd establishment timeout")

// defaultEstablishmentTimeout is the fallback timeout for
// waitForCRDsEstablished when the context has no deadline. This preserves
// the previous behavior of capping polling at ~30 seconds.
const defaultEstablishmentTimeout = 30 * time.Second

// isCRDEstablished reports whether a single CRD has the Established condition
// set to True.
func isCRDEstablished(crd *apiextensionsv1.CustomResourceDefinition) bool {
	for _, cond := range crd.Status.Conditions {
		if cond.Type == apiextensionsv1.Established && cond.Status == apiextensionsv1.ConditionTrue {
			return true
		}
	}
	return false
}

// findPendingCRDs returns the names of CRDs that have not yet reached the
// Established condition. Returns nil when all CRDs are established.
func findPendingCRDs(crdList *apiextensionsv1.CustomResourceDefinitionList) []string {
	var pending []string
	for i := range crdList.Items {
		if !isCRDEstablished(&crdList.Items[i]) {
			pending = append(pending, crdList.Items[i].Name)
		}
	}
	return pending
}

// logCRDEstablishmentProgress logs warnings and debug messages about pending
// CRDs. It returns true after the first warning has been emitted, allowing
// the caller to suppress duplicate warnings across loop iterations.
func logCRDEstablishmentProgress(
	ctx context.Context,
	logger *slog.Logger,
	pending []string,
	startTime time.Time,
	warned bool,
) bool {
	if !warned && time.Since(startTime) >= longWaitThreshold {
		warned = true
		// Clone pending before passing to slog: the caller reuses the slice
		// across loop iterations, so an async log handler could observe
		// mutated contents without a defensive copy.
		logger.Warn("CRD establishment is taking longer than expected",
			"elapsed", time.Since(startTime).Round(time.Millisecond),
			"pending_crds", slices.Clone(pending),
		)
	}

	if logger.Enabled(ctx, slog.LevelDebug) {
		// Clone: same reason as the Warn clone above.
		logger.Debug("waiting for CRD establishment",
			"pending_crds", slices.Clone(pending),
		)
	}

	return warned
}

// waitForCRDsEstablished polls until all CRDs in the cluster have the
// Established condition. The polling loop is driven by the context deadline.
// If the context has no deadline, a fallback timeout of defaultEstablishmentTimeout
// is applied to prevent unbounded polling.
func waitForCRDsEstablished(ctx context.Context, logger *slog.Logger, restCfg *rest.Config) error {
	// Ensure the polling loop is bounded. If the caller's context has no
	// deadline, apply a fallback timeout to prevent unbounded looping.
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, defaultEstablishmentTimeout)
		defer cancel()
	}

	clientCfg := rest.CopyConfig(restCfg)
	// Override client-go rate limits for the local, ephemeral cache-building
	// phase. See constant docs for the relationship between QPS, Burst, and
	// crdEstablishmentPollInterval.
	clientCfg.QPS = crdEstablishmentQPS
	clientCfg.Burst = crdEstablishmentBurst

	extClient, err := apiextensionsclient.NewForConfig(clientCfg)
	if err != nil {
		return fmt.Errorf("create apiextensions client: %w", err)
	}

	startTime := time.Now()
	warned := false

	ticker := time.NewTicker(crdEstablishmentPollInterval)
	defer ticker.Stop()

	for {
		crdList, err := extClient.ApiextensionsV1().CustomResourceDefinitions().List(ctx, metav1.ListOptions{})
		if err != nil {
			return fmt.Errorf("list CRDs: %w", err)
		}

		pending := findPendingCRDs(crdList)
		if len(pending) == 0 {
			if len(crdList.Items) == 0 {
				logger.Warn("zero CRDs found on API server; verify CRD directory contains valid CRD manifests")
			}
			return nil
		}

		warned = logCRDEstablishmentProgress(ctx, logger, pending, startTime, warned)

		select {
		case <-ctx.Done():
			return fmt.Errorf(
				"crd establishment did not complete after %s: pending CRDs: %v: %w",
				time.Since(startTime).Round(time.Millisecond),
				pending,
				ErrCRDEstablishTimeout,
			)
		case <-ticker.C:
		}
	}
}
