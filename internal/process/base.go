package process

import (
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"syscall"
	"time"

	"github.com/giantswarm/k8senv/internal/sentinel"
)

// ErrAlreadyStarted is returned when Start is called on a process that is
// already running. Callers must Stop the process before starting it again.
const ErrAlreadyStarted = sentinel.Error("process already started")

// BaseProcess provides common process lifecycle management.
// Embed this in package-specific Process types to reuse Stop and Close methods.
//
// BaseProcess is not safe for concurrent use. Callers must serialize access
// to all methods, including SetupAndStart, Stop, Close, and IsStarted.
// In practice, the kubestack.Stack that embeds BaseProcess is itself serialized
// by the Instance's startMu mutex.
type BaseProcess struct {
	cmd      *exec.Cmd
	waitDone <-chan error    // receives cmd.Wait result; started once in SetupAndStart
	exited   <-chan struct{} // closed when process exits; readable by multiple goroutines
	logFiles LogFiles
	name     string       // Process name for logging (e.g., "kine", "kube-apiserver")
	log      *slog.Logger // Logger for operational messages
}

// NewBaseProcess creates a BaseProcess with the given name and logger.
// If logger is nil, slog.Default() is used as a fallback.
// Panics if name is empty, since an empty name produces confusing error
// messages throughout the process lifecycle (Stop, Close, log entries).
func NewBaseProcess(name string, logger *slog.Logger) BaseProcess {
	if name == "" {
		panic("k8senv: process name must not be empty")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return BaseProcess{name: name, log: logger}
}

// Stop terminates the process with the given timeout.
// After Stop returns, IsStarted reports false regardless of whether the stop
// succeeded, because the process is no longer in a known-running state.
// Safe to call when cmd is nil (e.g., if Start was never called or Stop was
// already called); returns nil immediately.
func (b *BaseProcess) Stop(timeout time.Duration) error {
	if b.cmd == nil {
		return nil
	}
	err := stopWithDone(b.cmd, b.waitDone, timeout, b.name)
	b.cmd = nil
	b.waitDone = nil
	b.exited = nil
	return err
}

// Close closes log file handles. If the process is still running (Stop was not
// called first), Close logs a warning and stops the process automatically to
// prevent resource leaks. Callers should always call Stop before Close; the
// auto-stop is a safety net, not an intended code path.
func (b *BaseProcess) Close() {
	if b.cmd != nil {
		b.log.Warn("process.Close called without Stop; stopping automatically",
			"process", b.name)
		// Best-effort stop; log but do not propagate the error since Close
		// has no error return and changing the signature would break the
		// Stoppable interface contract.
		if err := b.Stop(DefaultStopTimeout); err != nil {
			b.log.Warn("auto-stop during Close failed",
				"process", b.name, "error", err)
		}
	}
	b.logFiles.Close()
}

// Logger returns the logger used by this process.
func (b *BaseProcess) Logger() *slog.Logger {
	return b.log
}

// Exited returns a channel that is closed when the process exits. It is safe
// to select on from any number of goroutines. Returns nil if the process has
// not been started or has already been stopped.
func (b *BaseProcess) Exited() <-chan struct{} {
	return b.exited
}

// IsStarted reports whether the process has been started and not yet stopped.
func (b *BaseProcess) IsStarted() bool {
	return b.cmd != nil
}

// SetupAndStart creates log files, sets up stdout/stderr, and starts the command.
// The cmd must already have its Path and Args set. This sets Dir, Stdout, Stderr
// and calls Start(). On success, cmd, waitDone, and logFiles are populated.
//
// A single goroutine calling cmd.Wait is started here so that exactly one Wait
// call is made per process. The resulting channel is consumed by Stop.
//
// Returns ErrAlreadyStarted if the process is already running. Callers must
// Stop and Close the process before calling SetupAndStart again.
func (b *BaseProcess) SetupAndStart(cmd *exec.Cmd, dataDir string) error {
	if dataDir == "" {
		return errors.New("data directory must not be empty")
	}
	if b.cmd != nil {
		return ErrAlreadyStarted
	}

	cmd.Dir = dataDir
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Pdeathsig: syscall.SIGTERM,
	}

	logFiles, err := StartCmd(cmd, dataDir, b.name)
	if err != nil {
		return fmt.Errorf("start command: %w", err)
	}
	b.cmd = cmd
	b.logFiles = logFiles

	// Start the single cmd.Wait goroutine. cmd.Wait must be called exactly
	// once per started process; calling it a second time is undefined
	// behavior and may block indefinitely. By starting the goroutine here,
	// we guarantee the invariant and provide a done channel for Stop.
	//
	// Two channels are created:
	//   - done (buffered 1): receives the Wait error, consumed once by Stop.
	//   - exited (unbuffered, closed): broadcast signal readable by any number
	//     of goroutines (e.g., WaitReady polling loops) to detect early exit.
	done := make(chan error, 1)
	exited := make(chan struct{})
	go func() {
		done <- cmd.Wait()
		close(exited)
	}()
	b.waitDone = done
	b.exited = exited

	return nil
}
