package kubestack

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"time"

	"github.com/giantswarm/k8senv/internal/apiserver"
	"github.com/giantswarm/k8senv/internal/fileutil"
	"github.com/giantswarm/k8senv/internal/kine"
	"github.com/giantswarm/k8senv/internal/netutil"
	"github.com/giantswarm/k8senv/internal/process"
	"golang.org/x/sync/errgroup"
)

// Config holds configuration for a kine + kube-apiserver pair.
type Config struct {
	// Required
	DataDir        string // Working directory for logs/config
	SQLitePath     string // Path to kine SQLite database
	KubeconfigPath string // Output path for kubeconfig

	// Optional (default to "kine" and "kube-apiserver")
	KineBinary      string
	APIServerBinary string

	// Optional (for prepopulating kine DB)
	CachedDBPath string

	// Timeouts (default to 30s for kine, 60s for apiserver)
	KineReadyTimeout      time.Duration
	APIServerReadyTimeout time.Duration

	// StopTimeout is the per-process timeout used when cleaning up
	// partially-started processes after a Start failure. If zero,
	// defaults to process.DefaultStopTimeout.
	StopTimeout time.Duration

	// PortRegistry coordinates port allocation across concurrent stacks.
	// Required: callers must provide a shared PortRegistry to prevent
	// duplicate port allocation. Typically created once per Manager and
	// shared across all instances.
	PortRegistry *netutil.PortRegistry

	// Logger (optional, defaults to slog.Default())
	Logger *slog.Logger
}

// Stack manages a coordinated kine + kube-apiserver pair.
type Stack struct {
	// Immutable after New: configuration, logger, and shared port registry.
	config Config
	log    *slog.Logger
	// ports is the canonical reference to the port registry, extracted from
	// Config.PortRegistry during construction. All port operations use this
	// field exclusively; config.PortRegistry is never accessed after New.
	ports *netutil.PortRegistry

	// Set by Start, cleared by Stop: process handles, allocated ports, and
	// lifecycle flag.
	kine      *kine.Process
	apiserver *apiserver.Process
	kinePort  int // allocated port for kine, released on Stop
	apiPort   int // allocated port for kube-apiserver, released on Stop
	started   bool
}

// stopTimeout returns the configured StopTimeout, falling back to
// process.DefaultStopTimeout when unset.
func (c Config) stopTimeout() time.Duration {
	if c.StopTimeout > 0 {
		return c.StopTimeout
	}
	return process.DefaultStopTimeout
}

// validate checks that all required Config fields are set and returns an error
// describing every violation found. It performs I/O via exec.LookPath to verify
// that configured binaries exist on $PATH. It uses errors.Join to report
// multiple issues at once, allowing callers to fix all problems in a single
// pass rather than playing whack-a-mole with one error at a time.
func (c Config) validate() error {
	var errs []error

	if c.KineBinary == "" {
		errs = append(errs, errors.New("kine binary path must not be empty"))
	} else if _, err := exec.LookPath(c.KineBinary); err != nil {
		errs = append(errs, fmt.Errorf("kine binary not found: %w", err))
	}
	if c.APIServerBinary == "" {
		errs = append(errs, errors.New("api server binary path must not be empty"))
	} else if _, err := exec.LookPath(c.APIServerBinary); err != nil {
		errs = append(errs, fmt.Errorf("api server binary not found: %w", err))
	}
	if c.DataDir == "" {
		errs = append(errs, errors.New("data dir must not be empty"))
	}
	if c.SQLitePath == "" {
		errs = append(errs, errors.New("sqlite path must not be empty"))
	}
	if c.KubeconfigPath == "" {
		errs = append(errs, errors.New("kubeconfig path must not be empty"))
	}
	if c.PortRegistry == nil {
		errs = append(errs, errors.New("port registry must not be nil"))
	}
	if c.KineReadyTimeout <= 0 {
		errs = append(errs, errors.New("kine ready timeout must be positive"))
	}
	if c.APIServerReadyTimeout <= 0 {
		errs = append(errs, errors.New("api server ready timeout must be positive"))
	}

	return errors.Join(errs...)
}

// New creates a new Stack. Does not start processes.
// Callers must set KineBinary, APIServerBinary, DataDir, SQLitePath,
// KubeconfigPath, and PortRegistry in cfg. Returns an error if any required
// field is missing or invalid.
func New(cfg Config) (*Stack, error) {
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("invalid kubestack config: %w", err)
	}
	return newValidated(cfg), nil
}

// newValidated constructs a Stack from a config that has already been
// validated. It is used internally to avoid redundant validation (e.g.,
// repeated exec.LookPath calls) inside retry loops.
func newValidated(cfg Config) *Stack {
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	ports := cfg.PortRegistry
	return &Stack{config: cfg, log: log, ports: ports}
}

// StartWithRetry creates and starts a kubestack, retrying up to maxRetries
// times on transient failures (e.g., port conflicts). Each retry creates a
// fresh Stack via [newValidated], which allocates new ports via
// [netutil.PortRegistry], resolving the root cause of port collisions without
// backoff.
//
// Config validation (including binary path lookups) is performed once before
// the retry loop. Validation errors are permanent and not retried.
// The readyCtx is checked before each attempt to avoid pointless retries
// after timeout. On failure, each partially-started stack is stopped using
// cfg.StopTimeout (or [process.DefaultStopTimeout] when unset) to release
// allocated ports.
func StartWithRetry(
	procCtx, readyCtx context.Context,
	cfg Config,
	maxRetries int,
) (*Stack, error) {
	if procCtx == nil {
		return nil, errors.New("procCtx must not be nil")
	}
	if readyCtx == nil {
		return nil, errors.New("readyCtx must not be nil")
	}
	// Best-effort guard: catches the most common mistake of passing the same
	// variable for both contexts. Cannot detect logically equivalent but
	// distinct context values.
	if procCtx == readyCtx {
		return nil, errors.New("procCtx and readyCtx must be different contexts; " +
			"procCtx governs process lifetime, readyCtx governs startup timeout")
	}
	if maxRetries < 1 {
		return nil, fmt.Errorf("maxRetries must be >= 1, got %d", maxRetries)
	}

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("invalid kubestack config: %w", err)
	}

	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}

	var lastErr error
	for attempt := 1; attempt <= maxRetries; attempt++ {
		select {
		case <-readyCtx.Done():
			if lastErr != nil {
				return nil, errors.Join(
					fmt.Errorf("context canceled after %d attempts: %w", attempt-1, readyCtx.Err()),
					fmt.Errorf("last attempt error: %w", lastErr),
				)
			}
			return nil, readyCtx.Err()
		default:
		}

		stack := newValidated(cfg)

		if err := stack.Start(procCtx, readyCtx); err != nil {
			lastErr = err
			log.Warn("kubestack start attempt failed",
				"attempt", attempt,
				"max_retries", maxRetries,
				"error", err,
			)
			if stopErr := stack.Stop(cfg.stopTimeout()); stopErr != nil {
				log.Warn("cleanup partially-started kubestack", "error", stopErr)
			}
			if isPermanentStartError(err) {
				return nil, fmt.Errorf("permanent start failure (not retried): %w", err)
			}
			continue
		}

		if attempt > 1 {
			log.Info("kubestack start succeeded after retry", "attempt", attempt)
		}
		return stack, nil
	}

	return nil, fmt.Errorf("start kubestack after %d attempts: %w", maxRetries, lastErr)
}

// isPermanentStartError reports whether err is a permanent failure that will
// not resolve on retry. Transient errors (port conflicts, brief readiness
// timeouts) are retryable; everything else is permanent.
//
// Permanent errors include:
//   - process.ErrAlreadyStarted: logical error, the stack is already running
//   - os.ErrPermission: file/directory permission denied
//   - os.ErrNotExist: missing binary, database template, or directory
//   - exec.ErrNotFound: binary not found in PATH
//   - fileutil.ErrEmptySrc, fileutil.ErrEmptyDst: invalid configuration
//   - context.Canceled, context.DeadlineExceeded: caller gave up
func isPermanentStartError(err error) bool {
	permanentErrors := []error{
		process.ErrAlreadyStarted,
		os.ErrPermission,
		os.ErrNotExist,
		exec.ErrNotFound,
		fileutil.ErrEmptySrc,
		fileutil.ErrEmptyDst,
		context.Canceled,
		context.DeadlineExceeded,
	}
	for _, target := range permanentErrors {
		if errors.Is(err, target) {
			return true
		}
	}
	return false
}

// Start launches both kine and kube-apiserver concurrently and waits for
// both to become ready. Since ports are allocated upfront, apiserver's
// --etcd-servers flag is configured before either process starts. The
// apiserver tolerates kine not being ready yet via its built-in etcd
// connection retry logic.
//
// Start requires two separate contexts because process lifetime and startup
// readiness have fundamentally different scopes:
//
//   - processCtx governs the OS process lifetime of both kine and
//     kube-apiserver. It is passed to [exec.CommandContext], so canceling it
//     sends a kill signal to the child processes. Callers typically derive
//     processCtx from [context.Background] so that processes outlive the
//     startup call and persist until explicitly stopped via [Stack.Stop].
//     This context is canceled by [Instance.Stop] when the instance is
//     released with clean=true or during manager shutdown.
//
//   - readyCtx governs how long Start waits for both processes to become
//     ready (kine accepting TCP connections, apiserver returning 200 on
//     /livez). It is the caller's context, typically carrying an acquire
//     timeout. If readyCtx is canceled or times out, readiness polling
//     stops and Start returns an error, but the child processes themselves
//     are unaffected (they were started under processCtx). The cleanup
//     defer then stops any partially-started processes.
//
// Internally, Start derives a third context (gCtx) from readyCtx via
// [errgroup.WithContext]. If either process fails its readiness check, gCtx
// is canceled, which immediately unblocks the other process's readiness
// poll rather than waiting for the full timeout.
//
// On error, any started processes are stopped and closed before returning.
//
// Start is not safe for concurrent use. Callers must ensure that Start (and
// Stop) are not called concurrently on the same Stack. In practice, each Stack
// is owned by a single Instance whose startMu serializes lifecycle calls.
func (s *Stack) Start(processCtx, readyCtx context.Context) (retErr error) {
	if processCtx == nil {
		return errors.New("processCtx must not be nil")
	}
	if readyCtx == nil {
		return errors.New("readyCtx must not be nil")
	}
	// Best-effort guard: catches the most common mistake of passing the same
	// variable for both contexts. Cannot detect logically equivalent but
	// distinct context values.
	if processCtx == readyCtx {
		return errors.New("processCtx and readyCtx must be different contexts; " +
			"processCtx governs process lifetime, readyCtx governs startup timeout")
	}
	if s.started {
		return process.ErrAlreadyStarted
	}

	startTime := time.Now()
	s.log.Debug("starting kube stack")

	if err := fileutil.EnsureDirForFile(s.config.SQLitePath); err != nil {
		return fmt.Errorf("create db dir: %w", err)
	}

	if err := s.allocatePorts(); err != nil {
		return err
	}

	// Cleanup all resources on any error during Start. A single defer
	// consolidates cleanup for ports, kine, and apiserver. Resources are
	// cleaned in reverse creation order (apiserver, kine, ports).
	// StopCloseAndNil handles nil pointers gracefully, so this is safe
	// regardless of which resources were successfully initialized.
	defer func() {
		if retErr != nil {
			s.cleanupAfterStartFailure()
		}
	}()

	if err := s.createProcesses(); err != nil {
		return err
	}

	if err := s.startAndWaitForReady(processCtx, readyCtx); err != nil {
		return err
	}

	s.started = true
	s.log.Debug("kube stack started", "elapsed", time.Since(startTime))
	return nil
}

// allocatePorts reserves a pair of ports from the shared port registry
// and stores them on the stack for use by kine and kube-apiserver.
func (s *Stack) allocatePorts() error {
	kinePort, apiPort, err := s.ports.AllocatePortPair()
	if err != nil {
		return fmt.Errorf("allocate ports: %w", err)
	}
	s.kinePort = kinePort
	s.apiPort = apiPort
	s.log.Debug("ports allocated", "kine_port", kinePort, "api_port", apiPort)
	return nil
}

// createProcesses builds the kine and apiserver process objects and
// generates the kubeconfig file. No OS processes are started; this only
// prepares the configuration so that startAndWaitForReady can launch both
// concurrently.
func (s *Stack) createProcesses() error {
	kineProc, err := kine.New(kine.Config{
		Binary:       s.config.KineBinary,
		DataDir:      s.config.DataDir,
		SQLitePath:   s.config.SQLitePath,
		Port:         s.kinePort,
		CachedDBPath: s.config.CachedDBPath,
		StopTimeout:  s.config.stopTimeout(),
		Logger:       s.log,
	})
	if err != nil {
		return fmt.Errorf("create kine process: %w", err)
	}
	s.kine = kineProc

	// Endpoint() only needs the port number, not a running kine process,
	// so this is safe to call before kine starts.
	apiserverProc, err := apiserver.New(apiserver.Config{
		Binary:         s.config.APIServerBinary,
		DataDir:        s.config.DataDir,
		Port:           s.apiPort,
		EtcdEndpoint:   s.kine.Endpoint(),
		KubeconfigPath: s.config.KubeconfigPath,
		StopTimeout:    s.config.stopTimeout(),
		Logger:         s.log,
	})
	if err != nil {
		return fmt.Errorf("create apiserver process: %w", err)
	}
	s.apiserver = apiserverProc

	s.log.Debug("generating kubeconfig")
	if err := s.apiserver.WriteKubeconfig(); err != nil {
		return fmt.Errorf("write kubeconfig: %w", err)
	}
	return nil
}

// startAndWaitForReady launches kine and kube-apiserver concurrently under
// processCtx and waits for both to report readiness within readyCtx.
//
// The apiserver has built-in etcd retry logic, so it tolerates kine not
// being ready yet. This removes kine's startup time from the critical path.
//
// errgroup.WithContext derives a child context (gCtx) that is canceled when
// any goroutine returns an error. Using gCtx for readiness checks ensures
// that if one process fails to start, the other's readiness poll is
// canceled immediately rather than waiting for the full timeout.
//
// Precondition: both processCtx and readyCtx must be non-nil (validated by Start).
func (s *Stack) startAndWaitForReady(processCtx, readyCtx context.Context) error {
	g, gCtx := errgroup.WithContext(readyCtx)

	g.Go(func() error {
		s.log.Debug("starting kine")
		if err := s.kine.Start(processCtx); err != nil {
			return fmt.Errorf("start kine: %w", err)
		}
		s.log.Debug("waiting for kine readiness", "timeout", s.config.KineReadyTimeout)
		if err := s.kine.WaitReady(gCtx, s.config.KineReadyTimeout); err != nil {
			return fmt.Errorf("kine readiness: %w", err)
		}
		s.log.Debug("kine ready")
		return nil
	})

	g.Go(func() error {
		s.log.Debug("starting apiserver")
		if err := s.apiserver.Start(processCtx); err != nil {
			return fmt.Errorf("start apiserver: %w", err)
		}
		s.log.Debug("waiting for apiserver readiness", "timeout", s.config.APIServerReadyTimeout)
		if err := s.apiserver.WaitReady(gCtx, s.config.APIServerReadyTimeout); err != nil {
			return fmt.Errorf("apiserver readiness: %w", err)
		}
		s.log.Debug("apiserver ready")
		return nil
	})

	if err := g.Wait(); err != nil {
		return fmt.Errorf("start processes: %w", err)
	}
	return nil
}

// cleanupAfterStartFailure releases all resources acquired during a failed
// Start call. Resources are cleaned in reverse creation order (apiserver,
// kine, ports). StopCloseAndNil handles nil pointers gracefully, so this
// is safe regardless of which resources were successfully initialized.
func (s *Stack) cleanupAfterStartFailure() {
	cleanupTimeout := s.config.stopTimeout()
	if err := process.StopCloseAndNil(&s.apiserver, cleanupTimeout); err != nil {
		s.log.Warn("cleanup apiserver after start failure", "error", err)
	}
	if err := process.StopCloseAndNil(&s.kine, cleanupTimeout); err != nil {
		s.log.Warn("cleanup kine after start failure", "error", err)
	}
	s.releasePorts()
}

// Stop stops both processes (apiserver first, then kine) and releases
// allocated ports back to the injected port registry.
//
// The timeout is applied per-process, not as a total budget across both
// processes. Each of kube-apiserver and kine independently receives the
// full timeout for its SIGTERM/SIGKILL shutdown sequence. Since the two
// processes are stopped sequentially (apiserver first, then kine), the
// worst-case total stop duration is 2*timeout, not timeout.
//
// The sequential order is intentional: apiserver shuts down faster when kine
// is still available. If kine is stopped first or concurrently, apiserver
// stalls on etcd connection retries before completing its shutdown, negating
// any theoretical time savings from parallel stopping.
//
// Stop is not safe for concurrent use. Callers must ensure that Stop (and
// Start) are not called concurrently on the same Stack. In practice, each Stack
// is owned by a single Instance whose startMu serializes lifecycle calls.
func (s *Stack) Stop(timeout time.Duration) error {
	if timeout <= 0 {
		return fmt.Errorf("stop timeout must be positive, got %s", timeout)
	}
	if !s.started {
		return nil
	}
	s.started = false

	var errs []error
	if err := process.StopCloseAndNil(&s.apiserver, timeout); err != nil {
		errs = append(errs, fmt.Errorf("stop apiserver: %w", err))
	}
	if err := process.StopCloseAndNil(&s.kine, timeout); err != nil {
		errs = append(errs, fmt.Errorf("stop kine: %w", err))
	}

	s.releasePorts()

	return errors.Join(errs...)
}

// releasePorts releases allocated ports back to the registry and zeroes
// the stored values. Safe to call when ports are already zero (no-op).
func (s *Stack) releasePorts() {
	if s.kinePort != 0 {
		s.ports.Release(s.kinePort)
		s.kinePort = 0
	}
	if s.apiPort != 0 {
		s.ports.Release(s.apiPort)
		s.apiPort = 0
	}
}
