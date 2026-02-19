package process

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

// LogFiles manages stdout/stderr file handles for a process.
type LogFiles struct {
	stdoutFile *os.File
	stderrFile *os.File
	dataDir    string
	stdoutName string // e.g., "kine-stdout.log"
	stderrName string // e.g., "kube-apiserver-stderr.log"
}

// create creates stdout and stderr log files.
// Both files are assigned to the struct only after both creates succeed.
func (l *LogFiles) create() error {
	stdoutFile, err := os.Create(l.StdoutPath())
	if err != nil {
		return fmt.Errorf("create stdout log: %w", err)
	}
	stderrFile, err := os.Create(l.StderrPath())
	if err != nil {
		_ = stdoutFile.Close()
		return fmt.Errorf("create stderr log: %w", err)
	}
	l.stdoutFile = stdoutFile
	l.stderrFile = stderrFile
	return nil
}

// Close closes both log file handles and nils them to prevent double-close.
func (l *LogFiles) Close() {
	if l.stdoutFile != nil {
		_ = l.stdoutFile.Close()
		l.stdoutFile = nil
	}
	if l.stderrFile != nil {
		_ = l.stderrFile.Close()
		l.stderrFile = nil
	}
}

// StdoutPath returns the absolute path to the stdout log file.
func (l *LogFiles) StdoutPath() string {
	return filepath.Join(l.dataDir, l.stdoutName)
}

// StderrPath returns the absolute path to the stderr log file.
func (l *LogFiles) StderrPath() string {
	return filepath.Join(l.dataDir, l.stderrName)
}

// NewLogFiles creates and initializes log files for a process.
// The processName is used to generate log file names (e.g., "kine" -> "kine-stdout.log").
func NewLogFiles(dataDir, processName string) (LogFiles, error) {
	l := LogFiles{
		dataDir:    dataDir,
		stdoutName: processName + "-stdout.log",
		stderrName: processName + "-stderr.log",
	}
	if err := l.create(); err != nil {
		return LogFiles{}, err
	}
	return l, nil
}

// DefaultStopTimeout is the default timeout for stopping a process. It is used
// as a fallback by packages that manage process lifecycle (kubestack, crdcache)
// when no explicit stop timeout is configured.
const DefaultStopTimeout = 10 * time.Second

// termGracePeriod is the maximum time to wait for a process to exit after
// SIGTERM before escalating to SIGKILL. The actual grace period is capped
// at the overall timeout.
const termGracePeriod = 5 * time.Second

// killDrainTimeout is the hard upper bound for waiting on the done channel
// after SIGKILL has been sent (or after the process has already exited).
// SIGKILL cannot be caught, so the process should exit almost immediately.
// This timeout is a safety net against indefinite blocking if cmd.Wait
// never returns (e.g., due to stuck I/O or kernel issues).
const killDrainTimeout = 10 * time.Second

// drainDone reads from the done channel with the given timeout as a hard
// upper bound. Under normal conditions cmd.Wait returns almost immediately
// after the process exits, so this timeout should never fire. It exists
// purely as a safety net to prevent indefinite blocking if cmd.Wait hangs
// due to stuck I/O or kernel issues.
//
// Returns true and the cmd.Wait error if the channel delivered in time,
// or false and a nil error if the timeout elapsed.
func drainDone(done <-chan error, timeout time.Duration) (bool, error) {
	t := time.NewTimer(timeout)
	defer t.Stop()

	select {
	case err := <-done:
		return true, err
	case <-t.C:
		return false, nil
	}
}

// stopWithDone implements the SIGTERM-then-SIGKILL shutdown sequence using a
// pre-existing done channel that already has a goroutine calling cmd.Wait. This
// avoids spawning a second cmd.Wait goroutine, which would be undefined behavior.
// The done channel must receive the result of exactly one cmd.Wait call.
//
// Shutdown flow:
//  1. Send SIGTERM for graceful shutdown.
//  2. Schedule SIGKILL via time.AfterFunc after a grace period (canceled if
//     the process exits first).
//  3. Wait for process exit or total timeout.
//
// stopWithDone does not nil cmd or the done channel. The caller is responsible
// for clearing these references after stopWithDone returns so that subsequent
// calls (and IsStarted checks) see the process as stopped.
//
// Worst-case blocking duration is timeout + killDrainTimeout (currently 10s).
// This occurs when the main timeout expires and the post-SIGKILL drain also
// blocks for its full duration. Callers allocating time budgets should account
// for this additional killDrainTimeout beyond the configured timeout.
func stopWithDone(cmd *exec.Cmd, done <-chan error, timeout time.Duration, name string) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	if done == nil {
		return fmt.Errorf("%s: done channel must not be nil", name)
	}

	// Send SIGTERM for graceful shutdown.
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		// Process already exited; drain the wait goroutine with a hard
		// upper bound to avoid blocking indefinitely.
		ok, waitErr := drainDone(done, killDrainTimeout)
		if !ok {
			return fmt.Errorf("%s: timed out draining process after signal failure", name)
		}
		return expectSignalExit(waitErr, name)
	}

	// Schedule SIGKILL after the grace period. If the process exits before
	// the grace period, killTimer.Stop() cancels the escalation.
	//
	// grace is clamped to timeout so SIGKILL always fires before the total
	// timeout expires. This guarantees the process receives a kill signal
	// while totalTimer is still running, giving drainDone a window to
	// collect the exit status rather than hitting the timeout path.
	grace := min(termGracePeriod, timeout)
	killTimer := time.AfterFunc(grace, func() {
		// Kill after Wait (process already exited) is a no-op that returns
		// an "os: process already finished" error, which we intentionally
		// discard. This is safe because the OS has already released the
		// process, and Kill on a finished process is explicitly harmless.
		_ = cmd.Process.Kill()
	})
	// Safety: killTimer.Stop cancels the pending SIGKILL before this function
	// returns. This is safe because cmd.Process is only used by the kill
	// callback and by the caller (who must not nil cmd until stopWithDone
	// returns). The defer guarantees the timer is canceled on all exit paths.
	defer killTimer.Stop()

	// Wait for process exit or total timeout.
	totalTimer := time.NewTimer(timeout)
	defer totalTimer.Stop()

	select {
	case err := <-done:
		return expectSignalExit(err, name)
	case <-totalTimer.C:
		ok, waitErr := drainDone(done, killDrainTimeout)
		if !ok {
			return fmt.Errorf("%s: timed out waiting for process to exit after SIGKILL", name)
		}
		if err := expectSignalExit(waitErr, name); err != nil {
			return fmt.Errorf("%s stop timeout: %w", name, err)
		}
		return nil
	}
}

// expectSignalExit interprets an error from cmd.Wait after sending a
// termination signal. Exit errors caused by SIGTERM or SIGKILL are expected
// and treated as successful stops.
func expectSignalExit(err error, name string) error {
	if err == nil {
		return nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
			sig := status.Signal()
			if sig == syscall.SIGTERM || sig == syscall.SIGKILL {
				return nil
			}
		}
	}
	return fmt.Errorf("%s: %w", name, err)
}

// StartCmd creates log files, sets up stdout/stderr, and starts the command.
// On success, caller owns the LogFiles. On failure, log files are closed automatically.
func StartCmd(cmd *exec.Cmd, dataDir, processName string) (LogFiles, error) {
	logFiles, err := NewLogFiles(dataDir, processName)
	if err != nil {
		return LogFiles{}, fmt.Errorf("create %s logs: %w", processName, err)
	}

	cmd.Stdout = logFiles.stdoutFile
	cmd.Stderr = logFiles.stderrFile

	if err := cmd.Start(); err != nil {
		logFiles.Close()
		return LogFiles{}, fmt.Errorf("start %s process: %w", processName, err)
	}

	return logFiles, nil
}
