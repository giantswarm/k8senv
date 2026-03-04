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

// noCopy prevents logFiles from being copied after it holds open file handles.
// Embedding this type causes go vet's copylocks checker to report any value
// copy of logFiles, which would alias the underlying *os.File fields and allow
// one copy to close the other's file descriptors.
//
// This technique is the same pattern used by sync.Mutex and strings.Builder in
// the standard library.
type noCopy struct{}

// Lock is a no-op that satisfies sync.Locker, making go vet's copylocks
// analyzer treat any type embedding noCopy as uncopyable.
func (*noCopy) Lock() {}

// Unlock is a no-op required to satisfy sync.Locker.
func (*noCopy) Unlock() {}

// logFiles manages stdout/stderr file handles for a process.
// logFiles must not be copied after creation; use a pointer when passing or
// storing it to avoid aliasing the underlying file descriptors.
type logFiles struct {
	noCopy     noCopy // sentinel: go vet copylocks flags any copy of this struct
	stdoutFile *os.File
	stderrFile *os.File
}

// Close closes both log file handles and nils them to prevent double-close.
func (l *logFiles) Close() {
	if l.stdoutFile != nil {
		_ = l.stdoutFile.Close()
		l.stdoutFile = nil
	}
	if l.stderrFile != nil {
		_ = l.stderrFile.Close()
		l.stderrFile = nil
	}
}

// newLogFiles creates and initializes log files for a process.
// The processName is used to generate log file names (e.g., "kine" -> "kine-stdout.log").
// Returns a pointer to prevent copying the file handles.
func newLogFiles(dataDir, processName string) (*logFiles, error) {
	stdoutPath := filepath.Join(dataDir, processName+"-stdout.log")
	stderrPath := filepath.Join(dataDir, processName+"-stderr.log")

	//nolint:gosec // paths from controlled config, not user input
	stdoutFile, err := os.Create(stdoutPath)
	if err != nil {
		return nil, fmt.Errorf("create stdout log: %w", err)
	}
	//nolint:gosec // paths from controlled config, not user input
	stderrFile, err := os.Create(stderrPath)
	if err != nil {
		_ = stdoutFile.Close()
		return nil, fmt.Errorf("create stderr log: %w", err)
	}
	return &logFiles{stdoutFile: stdoutFile, stderrFile: stderrFile}, nil
}

// DefaultStopTimeout is the default timeout for stopping a process. It is used
// as a fallback by packages that manage process lifecycle (kubestack, crdcache)
// when no explicit stop timeout is configured.
const DefaultStopTimeout = 10 * time.Second

// ResolveStopTimeout returns timeout if positive, otherwise DefaultStopTimeout.
func ResolveStopTimeout(timeout time.Duration) time.Duration {
	if timeout > 0 {
		return timeout
	}
	return DefaultStopTimeout
}

// termGracePeriod is the maximum time to wait for a process to exit after
// SIGTERM before escalating to SIGKILL. The actual grace period is capped
// at the overall timeout.
const termGracePeriod = 5 * time.Second

// killDrainBudget is the time reserved from the total timeout for draining
// the done channel after SIGKILL has been sent (or after the process has
// already exited). SIGKILL cannot be caught, so the process should exit
// almost immediately. This budget is a safety net against indefinite blocking
// if cmd.Wait never returns (e.g., due to stuck I/O or kernel issues).
//
// This budget is carved out of the caller's timeout, not additive. The main
// SIGTERM/SIGKILL wait uses (timeout - drainReserve) and the drain uses the
// remainder, so the total blocking time never exceeds timeout.
const killDrainBudget = 2 * time.Second

// drainReserve returns the portion of timeout to reserve for draining the
// done channel after SIGKILL. It is capped at killDrainBudget and never
// exceeds half of timeout, so at least half the budget is available for the
// graceful SIGTERM/SIGKILL wait.
func drainReserve(timeout time.Duration) time.Duration {
	reserve := min(killDrainBudget, timeout/2)
	if reserve <= 0 {
		reserve = 1 * time.Millisecond // floor: avoid zero/negative timer
	}
	return reserve
}

// drainDone reads from the done channel with the given timeout as a hard
// upper bound. Under normal conditions cmd.Wait returns almost immediately
// after the process exits, so this timeout should never fire. It exists
// purely as a safety net to prevent indefinite blocking if cmd.Wait hangs
// due to stuck I/O or kernel issues.
//
// Returns true and the cmd.Wait error if the channel delivered in time,
// or false and a nil error if the timeout elapsed.
func drainDone(done <-chan error, timeout time.Duration) (bool, error) {
	// Fast path: the process usually has already exited by the time we
	// drain, so check without allocating a timer.
	select {
	case err := <-done:
		return true, err
	default:
	}

	t := time.NewTimer(timeout)
	defer t.Stop()

	select {
	case err := <-done:
		return true, err
	case <-t.C:
		return false, nil
	}
}

// stopWithDone implements the SIGTERM-then-SIGKILL shutdown sequence using the
// pre-existing waitDone channel that already has a goroutine calling cmd.Wait.
// This avoids spawning a second cmd.Wait goroutine, which would be undefined
// behavior.
//
// Precondition: b.cmd, b.cmd.Process, and b.waitDone must be non-nil
// (caller verifies cmd/Process; waitDone is set alongside cmd by SetupAndStart).
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
// Total blocking time is bounded by timeout. A portion of the timeout
// (killDrainBudget, capped at half of timeout) is reserved for draining the
// done channel after SIGKILL. The main SIGTERM/SIGKILL wait uses the remainder.
func (b *BaseProcess) stopWithDone(timeout time.Duration) error {
	if b.waitDone == nil {
		return fmt.Errorf("%s: done channel must not be nil", b.name)
	}

	// Partition the timeout budget: reserve a portion at the end for
	// draining the done channel after SIGKILL, use the rest for the
	// graceful SIGTERM/SIGKILL wait. This ensures total blocking time
	// never exceeds timeout.
	reserve := drainReserve(timeout)
	mainBudget := timeout - reserve

	// Send SIGTERM for graceful shutdown.
	if err := b.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		// Process already exited; drain the wait goroutine within the
		// caller's timeout budget to avoid blocking indefinitely.
		ok, waitErr := drainDone(b.waitDone, timeout)
		if !ok {
			return fmt.Errorf("%s: timed out draining process after signal failure", b.name)
		}
		return expectSignalExit(waitErr, b.name)
	}

	// Schedule SIGKILL after the grace period. If the process exits before
	// the grace period, killTimer.Stop() cancels the escalation.
	//
	// grace is clamped to mainBudget so SIGKILL always fires before the
	// main wait expires. This guarantees the process receives a kill signal
	// while the main timer is still running, giving drainDone a window to
	// collect the exit status rather than hitting the timeout path.
	grace := min(termGracePeriod, mainBudget)

	// Capture the process pointer before scheduling the kill timer.
	// Stop's defer nils b.cmd after this method returns, so the closure
	// must not access b.cmd — it uses the captured proc pointer instead.
	proc := b.cmd.Process
	killTimer := time.AfterFunc(grace, func() {
		// Kill after Wait (process already exited) is a no-op that returns
		// an "os: process already finished" error, which we intentionally
		// discard. This is safe because the OS has already released the
		// process, and Kill on a finished process is explicitly harmless.
		_ = proc.Kill()
	})
	defer killTimer.Stop()

	// Wait for process exit or main budget expiry.
	totalTimer := time.NewTimer(mainBudget)
	defer totalTimer.Stop()

	select {
	case err := <-b.waitDone:
		return expectSignalExit(err, b.name)
	case <-totalTimer.C:
		// Main budget expired. Drain with the reserved budget. SIGKILL
		// was already sent (grace <= mainBudget), so the process should
		// exit almost immediately; the reserve is a safety net.
		ok, waitErr := drainDone(b.waitDone, reserve)
		if !ok {
			return fmt.Errorf("%s: timed out waiting for process to exit after SIGKILL", b.name)
		}
		if err := expectSignalExit(waitErr, b.name); err != nil {
			return fmt.Errorf("%s stop timeout: %w", b.name, err)
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

// startCmd creates log files, sets up stdout/stderr, and starts the command.
// On success, caller owns the logFiles. On failure, log files are closed automatically.
// Returns a pointer to prevent copying the file handles.
func startCmd(cmd *exec.Cmd, dataDir, processName string) (*logFiles, error) {
	logFiles, err := newLogFiles(dataDir, processName)
	if err != nil {
		return nil, fmt.Errorf("create %s logs: %w", processName, err)
	}

	cmd.Stdout = logFiles.stdoutFile
	cmd.Stderr = logFiles.stderrFile

	if err := cmd.Start(); err != nil {
		logFiles.Close()
		return nil, fmt.Errorf("start %s process: %w", processName, err)
	}

	return logFiles, nil
}
