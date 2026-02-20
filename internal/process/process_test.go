package process

import (
	"errors"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestExpectSignalExit(t *testing.T) {
	t.Parallel()

	type testCase struct {
		err     error
		signal  syscall.Signal
		wantErr bool
	}

	tests := map[string]testCase{
		"nil error returns nil": {
			wantErr: false,
		},
		"SIGTERM exit is expected": {
			signal:  syscall.SIGTERM,
			wantErr: false,
		},
		"SIGKILL exit is expected": {
			signal:  syscall.SIGKILL,
			wantErr: false,
		},
		"other signal is unexpected": {
			signal:  syscall.SIGINT,
			wantErr: true,
		},
		"non-ExitError is unexpected": {
			err:     errors.New("some other error"),
			wantErr: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			inputErr := tc.err
			if inputErr == nil && tc.signal != 0 {
				inputErr = makeSignalExitError(t, tc.signal)
			}

			got := expectSignalExit(inputErr, "test-proc")

			if tc.wantErr && got == nil {
				t.Fatal("expected error, got nil")
			}
			if !tc.wantErr && got != nil {
				t.Fatalf("expected nil, got %v", got)
			}
		})
	}
}

func TestExpectSignalExit_WrapsProcessName(t *testing.T) {
	t.Parallel()

	err := expectSignalExit(errors.New("connection refused"), "my-proc")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	got := err.Error()
	if !strings.Contains(got, "my-proc") {
		t.Errorf("error %q should contain process name %q", got, "my-proc")
	}
	if !strings.Contains(got, "connection refused") {
		t.Errorf("error %q should contain original message %q", got, "connection refused")
	}
}

func TestDrainDone_ReceivesValue(t *testing.T) {
	t.Parallel()

	done := make(chan error, 1)
	done <- nil

	ok, err := drainDone(done, time.Second)
	if !ok {
		t.Fatal("expected ok=true when channel has a value")
	}
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestDrainDone_ReceivesError(t *testing.T) {
	t.Parallel()

	done := make(chan error, 1)
	want := errors.New("process crashed")
	done <- want

	ok, err := drainDone(done, time.Second)
	if !ok {
		t.Fatal("expected ok=true when channel has a value")
	}
	if !errors.Is(err, want) {
		t.Fatalf("expected %v, got %v", want, err)
	}
}

func TestDrainDone_TimesOutOnEmpty(t *testing.T) {
	t.Parallel()

	done := make(chan error) // unbuffered, never written to

	ok, err := drainDone(done, 10*time.Millisecond)
	if ok {
		t.Fatal("expected ok=false when timeout elapses")
	}
	if err != nil {
		t.Fatalf("expected nil error on timeout, got %v", err)
	}
}

func TestDrainReserve(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		timeout time.Duration
		want    time.Duration
	}{
		"large timeout caps at killDrainBudget": {
			timeout: 30 * time.Second,
			want:    killDrainBudget,
		},
		"medium timeout caps at killDrainBudget": {
			timeout: 10 * time.Second,
			want:    killDrainBudget,
		},
		"small timeout uses half": {
			timeout: 2 * time.Second,
			want:    1 * time.Second,
		},
		"tiny timeout uses half": {
			timeout: 100 * time.Millisecond,
			want:    50 * time.Millisecond,
		},
		"zero timeout uses floor": {
			timeout: 0,
			want:    1 * time.Millisecond,
		},
		"negative timeout uses floor": {
			timeout: -1 * time.Second,
			want:    1 * time.Millisecond,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := drainReserve(tc.timeout)
			if got != tc.want {
				t.Errorf("drainReserve(%v) = %v, want %v", tc.timeout, got, tc.want)
			}
		})
	}
}

func TestNewBaseProcess(t *testing.T) {
	t.Parallel()

	t.Run("creates process with name", func(t *testing.T) {
		t.Parallel()
		bp := NewBaseProcess("kine", nil, 0)
		if bp.name != "kine" {
			t.Errorf("name = %q, want %q", bp.name, "kine")
		}
		if bp.log == nil {
			t.Fatal("expected non-nil logger")
		}
		if bp.IsStarted() {
			t.Error("new process should not be started")
		}
	})

	t.Run("panics on empty name", func(t *testing.T) {
		t.Parallel()
		defer func() {
			r := recover()
			if r == nil {
				t.Fatal("expected panic for empty name")
			}
			msg, ok := r.(string)
			if !ok {
				t.Fatalf("expected string panic, got %T", r)
			}
			if msg != "k8senv: process name must not be empty" {
				t.Errorf("panic message = %q, want %q", msg, "k8senv: process name must not be empty")
			}
		}()
		NewBaseProcess("", nil, 0)
	})
}

func TestBaseProcess_StopWhenNotStarted(t *testing.T) {
	t.Parallel()

	bp := NewBaseProcess("test", nil, 0)
	if err := bp.Stop(time.Second); err != nil {
		t.Fatalf("Stop on unstarted process should return nil, got %v", err)
	}
}

func TestBaseProcess_StopWhenCmdProcessIsNil(t *testing.T) {
	t.Parallel()

	// Simulate a state where cmd is set but cmd.Process is nil — this can
	// occur if the OS failed to assign a process after cmd.Start was called
	// (e.g., a partial failure between log file setup and actual launch).
	// Stop must not dereference cmd.Process in this case.
	bp := NewBaseProcess("test", nil, 0)
	bp.cmd = &exec.Cmd{} // cmd is non-nil, but cmd.Process is nil
	if err := bp.Stop(time.Second); err != nil {
		t.Fatalf("Stop with nil cmd.Process should return nil, got %v", err)
	}
	if bp.IsStarted() {
		t.Error("IsStarted should return false after Stop clears cmd")
	}
}

func TestBaseProcess_CloseWhenNotStarted(t *testing.T) {
	t.Parallel()

	bp := NewBaseProcess("test", nil, 0)
	// Close on unstarted process should not panic.
	bp.Close()
}

func TestBaseProcess_Exited(t *testing.T) {
	t.Parallel()

	bp := NewBaseProcess("test", nil, 0)
	if bp.Exited() != nil {
		t.Error("Exited should return nil for unstarted process")
	}
}

func TestLogFiles_Paths(t *testing.T) {
	t.Parallel()

	t.Run("stdout path", func(t *testing.T) {
		t.Parallel()
		lf := LogFiles{dataDir: "/tmp/k8senv/inst-1", stdoutName: "kine-stdout.log"}
		want := "/tmp/k8senv/inst-1/kine-stdout.log"
		if got := lf.StdoutPath(); got != want {
			t.Errorf("StdoutPath() = %q, want %q", got, want)
		}
	})

	t.Run("stderr path", func(t *testing.T) {
		t.Parallel()
		lf := LogFiles{dataDir: "/tmp/k8senv/inst-1", stderrName: "kine-stderr.log"}
		want := "/tmp/k8senv/inst-1/kine-stderr.log"
		if got := lf.StderrPath(); got != want {
			t.Errorf("StderrPath() = %q, want %q", got, want)
		}
	})
}

func TestLogFiles_CloseNilHandles(t *testing.T) {
	t.Parallel()

	// Close with nil file handles should not panic.
	lf := LogFiles{}
	lf.Close()
}

func TestStopCloseAndNil(t *testing.T) {
	t.Parallel()

	t.Run("nil pointer returns nil", func(t *testing.T) {
		t.Parallel()
		err := StopCloseAndNil[*fakeStoppable](nil, time.Second)
		if err != nil {
			t.Fatalf("expected nil, got %v", err)
		}
	})

	t.Run("nil value returns nil", func(t *testing.T) {
		t.Parallel()
		var p *fakeStoppable
		err := StopCloseAndNil(&p, time.Second)
		if err != nil {
			t.Fatalf("expected nil, got %v", err)
		}
	})

	t.Run("calls stop and close then nils", func(t *testing.T) {
		t.Parallel()
		f := &fakeStoppable{}
		p := f
		err := StopCloseAndNil(&p, 5*time.Second)
		assertStopCloseSuccess(t, f, p, err, 5*time.Second)
	})

	t.Run("close and nil on stop error", func(t *testing.T) {
		t.Parallel()
		f := &fakeStoppable{stopErr: errors.New("stop failed")}
		p := f
		err := StopCloseAndNil(&p, time.Second)
		assertStopCloseError(t, f, p, err, "stop failed")
	})
}

// assertStopCloseSuccess verifies StopCloseAndNil behavior on the success path:
// no error, pointer nilled, Stop and Close called with correct timeout.
func assertStopCloseSuccess(t *testing.T, f *fakeStoppable, p *fakeStoppable, err error, wantTimeout time.Duration) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p != nil {
		t.Error("pointer should be nil after StopCloseAndNil")
	}
	if !f.stopped {
		t.Error("Stop should have been called")
	}
	if !f.closed {
		t.Error("Close should have been called")
	}
	if f.stopTimeout != wantTimeout {
		t.Errorf("Stop timeout = %v, want %v", f.stopTimeout, wantTimeout)
	}
}

// assertStopCloseError verifies StopCloseAndNil behavior on the error path:
// error returned with expected message, pointer nilled, Close still called.
func assertStopCloseError(t *testing.T, f *fakeStoppable, p *fakeStoppable, err error, wantMsg string) {
	t.Helper()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err.Error() != wantMsg {
		t.Errorf("error = %q, want %q", err.Error(), wantMsg)
	}
	if p != nil {
		t.Error("pointer should be nil even when Stop fails")
	}
	if !f.closed {
		t.Error("Close should be called even when Stop fails")
	}
}

// fakeStoppable is a test double for the Stoppable interface.
type fakeStoppable struct {
	stopped     bool
	closed      bool
	stopErr     error
	stopTimeout time.Duration
}

func (f *fakeStoppable) Stop(timeout time.Duration) error {
	f.stopped = true
	f.stopTimeout = timeout
	return f.stopErr
}

func (f *fakeStoppable) Close() {
	f.closed = true
}

func TestSetupAndStart_NilCmd(t *testing.T) {
	t.Parallel()

	bp := NewBaseProcess("test-proc", nil, 0)
	err := bp.SetupAndStart(nil, "/tmp/data")
	if err == nil {
		t.Fatal("expected error for nil cmd, got nil")
	}
	if !errors.Is(err, ErrNilCmd) {
		t.Errorf("expected ErrNilCmd, got: %v", err)
	}
}

func TestSetupAndStart_EmptyPath(t *testing.T) {
	t.Parallel()

	bp := NewBaseProcess("test-proc", nil, 0)
	cmd := &exec.Cmd{} // Path is empty by default
	err := bp.SetupAndStart(cmd, "/tmp/data")
	if err == nil {
		t.Fatal("expected error for empty Path, got nil")
	}
	if !errors.Is(err, ErrEmptyCmdPath) {
		t.Errorf("expected ErrEmptyCmdPath, got: %v", err)
	}
}

func TestSetupAndStart_EmptyDataDir(t *testing.T) {
	t.Parallel()

	bp := NewBaseProcess("test-proc", nil, 0)
	cmd := &exec.Cmd{Path: "/usr/bin/sleep"}
	err := bp.SetupAndStart(cmd, "")
	if err == nil {
		t.Fatal("expected error for empty dataDir, got nil")
	}
	if !errors.Is(err, ErrEmptyDataDir) {
		t.Errorf("expected ErrEmptyDataDir, got: %v", err)
	}
}

func TestSetupAndStart_AlreadyStarted(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	bp := NewBaseProcess("test-proc", nil, 0)

	// Start a real process so bp.cmd becomes non-nil.
	cmd := exec.Command("sleep", "10")
	if err := bp.SetupAndStart(cmd, dataDir); err != nil {
		t.Fatalf("first SetupAndStart failed: %v", err)
	}
	t.Cleanup(func() {
		_ = bp.Stop(5 * time.Second)
		bp.Close()
	})

	// Attempt a second start while the process is still running.
	cmd2 := exec.Command("sleep", "10")
	err := bp.SetupAndStart(cmd2, dataDir)
	if err == nil {
		t.Fatal("expected error for already started process, got nil")
	}
	if !errors.Is(err, ErrAlreadyStarted) {
		t.Fatalf("expected ErrAlreadyStarted, got: %v", err)
	}
}

// makeSignalExitError creates an *exec.ExitError with the given signal.
// It uses a real process to generate an authentic WaitStatus.
// Calls t.Fatalf if the process cannot be started, signaled, or does not
// produce an ExitError, since all conditions indicate a broken test environment.
func makeSignalExitError(tb testing.TB, sig syscall.Signal) *exec.ExitError {
	tb.Helper()

	cmd := exec.Command("sleep", "60")
	if err := cmd.Start(); err != nil {
		tb.Fatalf("test setup: start sleep: %v", err)
	}

	if err := cmd.Process.Signal(sig); err != nil {
		// Kill the process to avoid leaking it, then fail.
		_ = cmd.Process.Kill() // best-effort cleanup
		tb.Fatalf("test setup: signal process with %v: %v", sig, err)
	}

	err := cmd.Wait()

	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		tb.Fatalf("test setup: expected *exec.ExitError from signaled process, got %v", err)
	}

	return exitErr
}
