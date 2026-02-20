package core

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/giantswarm/k8senv/internal/netutil"
)

// Compile-time check: fakeReleaser must satisfy InstanceReleaser.
var _ InstanceReleaser = (*fakeReleaser)(nil)

// validInstanceConfig returns a minimal InstanceConfig that passes Validate.
func validInstanceConfig() InstanceConfig {
	return InstanceConfig{
		StartTimeout:        5 * time.Minute,
		StopTimeout:         10 * time.Second,
		CleanupTimeout:      30 * time.Second,
		MaxStartRetries:     defaultMaxStartRetries,
		KineBinary:          "kine",
		KubeAPIServerBinary: "kube-apiserver",
		ReleaseStrategy:     ReleaseRestart,
	}
}

// fakeReleaser is a minimal InstanceReleaser for construction-only tests.
// It does not track calls — only used so NewInstance does not panic on nil.
type fakeReleaser struct{}

func (f *fakeReleaser) ReleaseToPool(_ *Instance, _ uint64) bool { return true }
func (f *fakeReleaser) ReleaseFailed(_ *Instance, _ uint64)      {}

// newTestInstance creates an Instance with valid construction params.
func newTestInstance(t *testing.T) *Instance {
	t.Helper()
	ports := netutil.NewPortRegistry(nil)
	return NewInstance(NewInstanceParams{
		ID:       "test-inst",
		DataDir:  t.TempDir(),
		Releaser: &fakeReleaser{},
		Ports:    ports,
		Config:   validInstanceConfig(),
	})
}

// requirePanicContains calls fn and verifies it panics with a message
// containing wantSubstr.
func requirePanicContains(t *testing.T, fn func(), wantSubstr string) {
	t.Helper()

	var recovered string
	func() {
		defer func() {
			if r := recover(); r != nil {
				recovered = fmt.Sprint(r)
			}
		}()
		fn()
	}()

	if recovered == "" {
		t.Fatal("expected panic, got none")
	}

	if !strings.Contains(recovered, wantSubstr) {
		t.Errorf("panic message %q does not contain %q", recovered, wantSubstr)
	}
}

// TestNewInstancePanics verifies that NewInstance panics on invalid params.
func TestNewInstancePanics(t *testing.T) {
	t.Parallel()

	ports := netutil.NewPortRegistry(nil)
	cfg := validInstanceConfig()

	tests := map[string]struct {
		fn      func()
		wantMsg string
	}{
		"empty ID": {
			fn: func() {
				NewInstance(NewInstanceParams{
					ID:       "",
					DataDir:  "/tmp/test",
					Releaser: &fakeReleaser{},
					Ports:    ports,
					Config:   cfg,
				})
			},
			wantMsg: "k8senv: instance id must not be empty",
		},
		"empty DataDir": {
			fn: func() {
				NewInstance(NewInstanceParams{
					ID:       "inst-1",
					DataDir:  "",
					Releaser: &fakeReleaser{},
					Ports:    ports,
					Config:   cfg,
				})
			},
			wantMsg: "k8senv: instance data dir must not be empty",
		},
		"nil Releaser": {
			fn: func() {
				NewInstance(NewInstanceParams{
					ID:       "inst-1",
					DataDir:  "/tmp/test",
					Releaser: nil,
					Ports:    ports,
					Config:   cfg,
				})
			},
			wantMsg: "k8senv: instance releaser must not be nil",
		},
		"nil Ports": {
			fn: func() {
				NewInstance(NewInstanceParams{
					ID:       "inst-1",
					DataDir:  "/tmp/test",
					Releaser: &fakeReleaser{},
					Ports:    nil,
					Config:   cfg,
				})
			},
			wantMsg: "k8senv: instance port registry must not be nil",
		},
		"invalid config": {
			fn: func() {
				NewInstance(NewInstanceParams{
					ID:       "inst-1",
					DataDir:  "/tmp/test",
					Releaser: &fakeReleaser{},
					Ports:    ports,
					Config:   InstanceConfig{}, // zero value fails Validate
				})
			},
			wantMsg: "k8senv: invalid instance config",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			requirePanicContains(t, tc.fn, tc.wantMsg)
		})
	}
}

// TestInstanceID verifies that ID returns the value given at construction.
func TestInstanceID(t *testing.T) {
	t.Parallel()

	ports := netutil.NewPortRegistry(nil)
	inst := NewInstance(NewInstanceParams{
		ID:       "my-unique-id",
		DataDir:  t.TempDir(),
		Releaser: &fakeReleaser{},
		Ports:    ports,
		Config:   validInstanceConfig(),
	})

	if got := inst.ID(); got != "my-unique-id" {
		t.Errorf("ID() = %q, want %q", got, "my-unique-id")
	}
}

// TestInstanceIsStartedInitiallyFalse verifies that a freshly constructed
// Instance reports IsStarted() == false before any Start call.
func TestInstanceIsStartedInitiallyFalse(t *testing.T) {
	t.Parallel()

	inst := newTestInstance(t)
	if inst.IsStarted() {
		t.Error("IsStarted() = true on freshly created instance, want false")
	}
}

// TestInstanceIsBusyInitiallyFalse verifies that a freshly created Instance
// is not busy (generation == 0, which is even).
func TestInstanceIsBusyInitiallyFalse(t *testing.T) {
	t.Parallel()

	inst := newTestInstance(t)
	if inst.IsBusy() {
		t.Error("IsBusy() = true on freshly created instance, want false")
	}
}

// TestInstanceMarkAcquiredAndIsBusy verifies the generation counter progression:
// markAcquired sets gen to 1 (odd = busy), tryRelease advances to 2 (even = free).
func TestInstanceMarkAcquiredAndIsBusy(t *testing.T) {
	t.Parallel()

	inst := newTestInstance(t)

	// Initially free (gen=0, even).
	if inst.IsBusy() {
		t.Fatal("initial IsBusy() should be false")
	}

	// Acquire: gen advances 0 → 1 (odd = busy).
	token := inst.markAcquired()
	if token != 1 {
		t.Errorf("first markAcquired token = %d, want 1", token)
	}
	if !inst.IsBusy() {
		t.Error("IsBusy() = false after markAcquired, want true")
	}

	// Release: gen advances 1 → 2 (even = free).
	if !inst.tryRelease(token) {
		t.Fatal("tryRelease with current token should return true")
	}
	if inst.IsBusy() {
		t.Error("IsBusy() = true after tryRelease, want false")
	}
}

// TestInstanceMarkAcquiredMonotonic verifies that successive acquire/release
// cycles produce monotonically increasing tokens and that each token is unique.
func TestInstanceMarkAcquiredMonotonic(t *testing.T) {
	t.Parallel()

	inst := newTestInstance(t)

	for cycle := range 5 {
		token := inst.markAcquired()

		// Token must be odd (acquired state).
		if token%2 != 1 {
			t.Errorf("cycle %d: token %d is even (want odd)", cycle, token)
		}

		// Token must equal 2*cycle+1 (0-indexed: 1, 3, 5, 7, 9).
		want := uint64(2*cycle + 1)
		if token != want {
			t.Errorf("cycle %d: token = %d, want %d", cycle, token, want)
		}

		if !inst.tryRelease(token) {
			t.Fatalf("cycle %d: tryRelease failed", cycle)
		}
	}
}

// TestInstanceTryReleaseStaleToken verifies that tryRelease returns false when
// called with a token from a prior acquisition (stale token = double-release scenario).
func TestInstanceTryReleaseStaleToken(t *testing.T) {
	t.Parallel()

	inst := newTestInstance(t)

	// First acquire/release cycle: token = 1.
	firstToken := inst.markAcquired()
	if !inst.tryRelease(firstToken) {
		t.Fatal("first tryRelease failed unexpectedly")
	}

	// Second acquire/release cycle: token = 3.
	secondToken := inst.markAcquired()
	if secondToken == firstToken {
		t.Fatalf("second token %d equals first token (tokens must be unique)", secondToken)
	}

	// Attempting to release with the stale first token must return false.
	if inst.tryRelease(firstToken) {
		t.Error("tryRelease with stale token returned true, want false")
	}

	// Cleanup: release with the valid second token.
	if !inst.tryRelease(secondToken) {
		t.Error("tryRelease with current token failed after stale attempt")
	}
}

// TestInstanceIsCurrentToken verifies the non-consuming generation check used
// inside Release before performing cleanup side effects.
func TestInstanceIsCurrentToken(t *testing.T) {
	t.Parallel()

	inst := newTestInstance(t)

	// Before acquisition, gen=0. Token 0 is the initial value.
	if !inst.isCurrentToken(0) {
		t.Error("isCurrentToken(0) should be true before first acquire")
	}

	// Acquire: gen = 1. Token 1 is current; 0 is stale.
	token := inst.markAcquired()
	if !inst.isCurrentToken(token) {
		t.Error("isCurrentToken(token) should be true after markAcquired")
	}
	if inst.isCurrentToken(0) {
		t.Error("isCurrentToken(0) should be false after markAcquired")
	}

	// Release: gen = 2. Token 1 is now stale.
	inst.tryRelease(token)
	if inst.isCurrentToken(token) {
		t.Error("isCurrentToken(token) should be false after tryRelease")
	}
}

// TestInstanceErrInitiallyNil verifies that Err returns nil on a fresh instance.
func TestInstanceErrInitiallyNil(t *testing.T) {
	t.Parallel()

	inst := newTestInstance(t)
	if err := inst.Err(); err != nil {
		t.Errorf("Err() = %v on fresh instance, want nil", err)
	}
}

// TestInstanceSetErrAndErr verifies that setErr stores the error and Err retrieves it.
func TestInstanceSetErrAndErr(t *testing.T) {
	t.Parallel()

	inst := newTestInstance(t)

	want := errors.New("test error")
	inst.setErr(want)

	got := inst.Err()
	if got == nil {
		t.Fatal("Err() = nil after setErr, want non-nil")
	}
	if !errors.Is(got, want) {
		t.Errorf("Err() = %v, want %v", got, want)
	}
}

// TestInstanceSetErrOverwrite verifies that a second setErr call replaces
// the previously stored error.
func TestInstanceSetErrOverwrite(t *testing.T) {
	t.Parallel()

	inst := newTestInstance(t)

	first := errors.New("first error")
	second := errors.New("second error")

	inst.setErr(first)
	inst.setErr(second)

	got := inst.Err()
	if !errors.Is(got, second) {
		t.Errorf("Err() = %v after second setErr, want %v", got, second)
	}
}

// TestInstanceConfigReturnsErrInstanceReleasedWhenFree verifies that Config
// returns ErrInstanceReleased when the instance is not acquired (gen is even).
func TestInstanceConfigReturnsErrInstanceReleasedWhenFree(t *testing.T) {
	t.Parallel()

	inst := newTestInstance(t)
	// Instance is free (IsBusy=false, gen=0).

	_, err := inst.Config()
	if !errors.Is(err, ErrInstanceReleased) {
		t.Errorf("Config() error = %v, want ErrInstanceReleased", err)
	}
}

// TestInstanceConfigReturnsErrNotStartedWhenBusyButNotStarted verifies that
// Config returns ErrNotStarted when the instance is acquired but not yet started.
func TestInstanceConfigReturnsErrNotStartedWhenBusyButNotStarted(t *testing.T) {
	t.Parallel()

	inst := newTestInstance(t)
	inst.markAcquired() // gen = 1 (busy), but started = false

	_, err := inst.Config()
	if !errors.Is(err, ErrNotStarted) {
		t.Errorf("Config() error = %v, want ErrNotStarted", err)
	}
}
