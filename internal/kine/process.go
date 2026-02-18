package kine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"time"

	"github.com/giantswarm/k8senv/internal/fileutil"
	"github.com/giantswarm/k8senv/internal/process"
)

// readinessPollInterval is the interval between consecutive TCP connection
// attempts when waiting for the kine process to become ready.
const readinessPollInterval = 10 * time.Millisecond

// readinessDialTimeout is the per-attempt timeout for the TCP dial used in
// kine readiness checks. 1 second is generous for a localhost connection;
// early attempts that fail because kine is not yet listening return
// immediately with a connection-refused error, so this timeout only guards
// against pathological cases (e.g., SYN sent but no SYN-ACK).
const readinessDialTimeout = time.Second

// Compile-time interface satisfaction check.
var _ process.Stoppable = (*Process)(nil)

// Config holds the configuration for a kine process.
type Config struct {
	Binary       string // Path to kine binary (default: "kine")
	DataDir      string // Working directory for logs
	SQLitePath   string // Path to SQLite database
	Port         int    // Listen port
	CachedDBPath string // Optional: source DB to prepopulate from

	// Logger (optional, defaults to slog.Default())
	Logger *slog.Logger
}

// Process manages a kine process lifecycle.
type Process struct {
	config Config
	base   process.BaseProcess
}

// validate checks that all required Config fields are set and returns an error
// describing the first missing or invalid field.
func (c Config) validate() error {
	if c.Binary == "" {
		return errors.New("binary path must not be empty")
	}
	if c.DataDir == "" {
		return errors.New("data dir must not be empty")
	}
	if c.SQLitePath == "" {
		return errors.New("sqlite path must not be empty")
	}
	if c.Port <= 0 {
		return errors.New("port must be positive")
	}
	return nil
}

// New creates a new kine Process with the given configuration.
// It returns an error if any required field is missing or invalid.
// New performs no I/O; all side effects (such as database prepopulation)
// are deferred to Start.
func New(cfg Config) (*Process, error) {
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("invalid kine config: %w", err)
	}
	return &Process{
		config: cfg,
		base:   process.NewBaseProcess("kine", cfg.Logger),
	}, nil
}

// Start launches the kine process. If CachedDBPath is set in the
// configuration, Start prepopulates the SQLite database before launching.
func (p *Process) Start(ctx context.Context) error {
	if p.base.IsStarted() {
		return process.ErrAlreadyStarted
	}

	// Ensure a clean database state before launching.
	if p.config.CachedDBPath != "" {
		// Restore from cached template (e.g., pre-loaded CRDs).
		if err := prepopulateDB(p.config.CachedDBPath, p.config.SQLitePath); err != nil {
			return fmt.Errorf("prepopulate kine database: %w", err)
		}
	} else {
		// No template — remove any stale database so kine creates a fresh
		// one. Without this, ReleaseRestart would reopen the previous
		// test's database, leaking state across acquisitions.
		if err := removeSQLiteFiles(p.config.SQLitePath); err != nil {
			return fmt.Errorf("remove stale kine database: %w", err)
		}
	}

	args := []string{
		"--endpoint=sqlite://" + p.config.SQLitePath,
		fmt.Sprintf("--listen-address=127.0.0.1:%d", p.config.Port),
		"--metrics-bind-address=0", // Disable metrics server to avoid port conflicts
	}

	cmd := exec.CommandContext(ctx, p.config.Binary, args...)
	if err := p.base.SetupAndStart(cmd, p.config.DataDir); err != nil {
		return fmt.Errorf("setup and start kine process: %w", err)
	}
	return nil
}

// WaitReady polls the kine TCP port until it's accepting connections.
func (p *Process) WaitReady(ctx context.Context, timeout time.Duration) error {
	addr := fmt.Sprintf("127.0.0.1:%d", p.config.Port)

	log := p.base.Logger()
	dialer := &net.Dialer{Timeout: readinessDialTimeout}
	if err := process.WaitReady(ctx, process.WaitReadyConfig{
		Interval:      readinessPollInterval,
		Timeout:       timeout,
		Name:          "kine",
		Port:          p.config.Port,
		Logger:        log,
		ProcessExited: p.base.Exited(),
	}, func(checkCtx context.Context, attempt int) (bool, error) {
		conn, err := dialer.DialContext(checkCtx, "tcp", addr)
		if err != nil {
			log.Debug("waitForKine attempt", "port", p.config.Port, "attempt", attempt, "error", err)
			return false, nil // Not ready yet
		}
		_ = conn.Close() // best-effort close of readiness check connection
		return true, nil // kine is listening
	}); err != nil {
		return fmt.Errorf("kine not ready: %w", err)
	}
	return nil
}

// Endpoint returns the kine endpoint URL for kube-apiserver to connect to.
func (p *Process) Endpoint() string {
	return fmt.Sprintf("http://127.0.0.1:%d", p.config.Port)
}

// Stop terminates the kine process with the given timeout.
func (p *Process) Stop(timeout time.Duration) error {
	return p.base.Stop(timeout)
}

// Close releases log file handles held by the process.
func (p *Process) Close() {
	p.base.Close()
}

// prepopulateDB copies a SQLite database file to the destination path.
// This is used to prepopulate kine's SQLite database with existing state.
func prepopulateDB(srcPath, dstPath string) error {
	mode := os.FileMode(0o600)
	if err := fileutil.CopyFile(srcPath, dstPath, &fileutil.CopyFileOptions{Mode: &mode}); err != nil {
		return fmt.Errorf("copy database from %s to %s: %w", srcPath, dstPath, err)
	}
	return nil
}

// removeSQLiteFiles removes a SQLite database and its companion WAL/SHM files.
// Missing files are silently ignored.
func removeSQLiteFiles(dbPath string) error {
	for _, path := range []string{dbPath, dbPath + "-wal", dbPath + "-shm"} {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove %s: %w", path, err)
		}
	}
	return nil
}
