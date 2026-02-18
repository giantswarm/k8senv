package kubestack

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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

	// PortRegistry coordinates port allocation across concurrent stacks.
	// Required: callers must provide a shared PortRegistry to prevent
	// duplicate port allocation. Typically created once per Manager and
	// shared across all instances.
	PortRegistry *netutil.PortRegistry

	// Logger (optional, defaults to slog.Default())
	Logger *slog.Logger
}

// DefaultMaxPortRetries is the default number of startup retries for
// transient failures such as port conflicts during stack startup.
const DefaultMaxPortRetries = 3

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

// validate checks that all required Config fields are set and returns an error
// describing every violation found. It uses errors.Join to report multiple
// issues at once, allowing callers to fix all problems in a single pass rather
// than playing whack-a-mole with one error at a time.
func (c Config) validate() error {
	var errs []error

	if c.KineBinary == "" {
		errs = append(errs, errors.New("kine binary path must not be empty"))
	}
	if c.APIServerBinary == "" {
		errs = append(errs, errors.New("api server binary path must not be empty"))
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
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	ports := cfg.PortRegistry
	return &Stack{config: cfg, log: log, ports: ports}, nil
}

// StartWithRetry creates and starts a kubestack, retrying up to maxRetries
// times on transient failures (e.g., port conflicts). Each retry creates a
// fresh Stack via [New], which allocates new ports via [netutil.PortRegistry],
// resolving the root cause of port collisions without backoff.
//
// Config validation errors from [New] are permanent and not retried.
// The readyCtx is checked before each attempt to avoid pointless retries
// after timeout. On failure, each partially-started stack is stopped using
// stopTimeout to release allocated ports.
func StartWithRetry(
	procCtx, readyCtx context.Context,
	cfg Config,
	maxRetries int,
	stopTimeout time.Duration,
) (*Stack, error) {
	if procCtx == nil {
		return nil, errors.New("procCtx must not be nil")
	}
	if readyCtx == nil {
		return nil, errors.New("readyCtx must not be nil")
	}
	if maxRetries < 1 {
		return nil, fmt.Errorf("maxRetries must be >= 1, got %d", maxRetries)
	}
	if stopTimeout <= 0 {
		return nil, fmt.Errorf("stopTimeout must be positive, got %s", stopTimeout)
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

		stack, err := New(cfg)
		if err != nil {
			return nil, fmt.Errorf("create kubestack: %w", err)
		}

		if err := stack.Start(procCtx, readyCtx); err != nil {
			lastErr = err
			log.Warn("kubestack start attempt failed",
				"attempt", attempt,
				"max_retries", maxRetries,
				"error", err,
			)
			if stopErr := stack.Stop(stopTimeout); stopErr != nil {
				log.Warn("cleanup partially-started kubestack", "error", stopErr)
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
	if s.started {
		return process.ErrAlreadyStarted
	}

	startTime := time.Now()
	s.log.Debug("starting kube stack")

	// 1. Ensure db directory
	if err := fileutil.EnsureDirForFile(s.config.SQLitePath); err != nil {
		return fmt.Errorf("create db dir: %w", err)
	}

	// 2. Allocate ports (registered in the injected port registry)
	kinePort, apiPort, err := s.ports.AllocatePortPair()
	if err != nil {
		return fmt.Errorf("allocate ports: %w", err)
	}
	s.kinePort = kinePort
	s.apiPort = apiPort
	s.log.Debug("ports allocated", "kine_port", kinePort, "api_port", apiPort)

	// Cleanup all resources on any error during Start. A single defer
	// consolidates cleanup for ports, kine, and apiserver. Resources are
	// cleaned in reverse creation order (apiserver, kine, ports).
	// StopCloseAndNil handles nil pointers gracefully, so this is safe
	// regardless of which resources were successfully initialized.
	defer func() {
		if retErr != nil {
			if err := process.StopCloseAndNil(&s.apiserver, process.DefaultStopTimeout); err != nil {
				s.log.Warn("cleanup apiserver after start failure", "error", err)
			}
			if err := process.StopCloseAndNil(&s.kine, process.DefaultStopTimeout); err != nil {
				s.log.Warn("cleanup kine after start failure", "error", err)
			}
			s.releasePorts()
		}
	}()

	// 3. Create kine process object
	kineProc, err := kine.New(kine.Config{
		Binary:       s.config.KineBinary,
		DataDir:      s.config.DataDir,
		SQLitePath:   s.config.SQLitePath,
		Port:         kinePort,
		CachedDBPath: s.config.CachedDBPath,
		Logger:       s.log,
	})
	if err != nil {
		return fmt.Errorf("create kine process: %w", err)
	}
	s.kine = kineProc

	// 4. Create apiserver process object. Endpoint() only needs the port
	// number, not a running kine process, so this is safe to call now.
	apiserverProc, err := apiserver.New(apiserver.Config{
		Binary:         s.config.APIServerBinary,
		DataDir:        s.config.DataDir,
		Port:           apiPort,
		EtcdEndpoint:   s.kine.Endpoint(),
		KubeconfigPath: s.config.KubeconfigPath,
		Logger:         s.log,
	})
	if err != nil {
		return fmt.Errorf("create apiserver process: %w", err)
	}
	s.apiserver = apiserverProc

	// 5. Generate kubeconfig before starting processes (only needs port)
	s.log.Debug("generating kubeconfig")
	if err := s.apiserver.WriteKubeconfig(); err != nil {
		return fmt.Errorf("write kubeconfig: %w", err)
	}

	// 6. Start both processes concurrently and wait for readiness.
	// The apiserver has built-in etcd retry logic, so it tolerates kine
	// not being ready yet. This removes kine's startup time from the
	// critical path.
	// errgroup.WithContext derives a child context that is canceled when any
	// goroutine returns an error. Using gCtx for readiness checks ensures that
	// if one process fails to start, the other's readiness poll is canceled
	// immediately rather than waiting for the full timeout.
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

	s.started = true
	s.log.Debug("kube stack started", "elapsed", time.Since(startTime))
	return nil
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
