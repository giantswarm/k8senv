package k8senv_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/giantswarm/k8senv"
)

// panicTestCase defines a test case for option validation panic tests.
type panicTestCase struct {
	name     string
	panics   bool
	panicMsg string
	fn       func()
}

// requirePanics calls fn and verifies it panics (or not) with the expected message.
func requirePanics(t *testing.T, shouldPanic bool, wantMsg string, fn func()) {
	t.Helper()
	defer func() {
		r := recover()
		if shouldPanic && r == nil {
			t.Fatal("expected panic but didn't get one")
		}
		if !shouldPanic && r != nil {
			t.Fatalf("unexpected panic: %v", r)
		}
		if shouldPanic && r != nil {
			msg := fmt.Sprint(r)
			if msg != wantMsg {
				t.Fatalf("expected panic message %q, got %q", wantMsg, msg)
			}
		}
	}()
	fn()
}

// runPanicTests runs a slice of panic test cases using requirePanics.
func runPanicTests(t *testing.T, tests []panicTestCase) {
	t.Helper()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			requirePanics(t, tt.panics, tt.panicMsg, tt.fn)
		})
	}
}

func TestWithAcquireTimeoutPanicsOnInvalid(t *testing.T) {
	t.Parallel()
	runPanicTests(t, []panicTestCase{
		{
			"zero",
			true,
			"k8senv: acquire timeout must be greater than 0, got 0s",
			func() { k8senv.WithAcquireTimeout(0) },
		},
		{
			"negative",
			true,
			"k8senv: acquire timeout must be greater than 0, got -1s",
			func() { k8senv.WithAcquireTimeout(-1 * time.Second) },
		},
		{"valid", false, "", func() { k8senv.WithAcquireTimeout(1 * time.Second) }},
	})
}

func TestWithKineBinaryPanicsOnEmpty(t *testing.T) {
	t.Parallel()
	runPanicTests(t, []panicTestCase{
		{"empty", true, "k8senv: kine binary path must not be empty", func() { k8senv.WithKineBinary("") }},
		{"valid", false, "", func() { k8senv.WithKineBinary("/usr/local/bin/kine") }},
	})
}

func TestWithPoolSizePanicsOnInvalid(t *testing.T) {
	t.Parallel()
	runPanicTests(t, []panicTestCase{
		{"negative", true, "k8senv: pool size must not be negative, got -1", func() { k8senv.WithPoolSize(-1) }},
		{"zero_unlimited", false, "", func() { k8senv.WithPoolSize(0) }},
		{"valid", false, "", func() { k8senv.WithPoolSize(5) }},
	})
}

func TestWithKubeAPIServerBinaryPanicsOnEmpty(t *testing.T) {
	t.Parallel()
	runPanicTests(t, []panicTestCase{
		{
			"empty",
			true,
			"k8senv: kube-apiserver binary path must not be empty",
			func() { k8senv.WithKubeAPIServerBinary("") },
		},
		{"valid", false, "", func() { k8senv.WithKubeAPIServerBinary("/usr/local/bin/kube-apiserver") }},
	})
}

func TestWithReleaseStrategyPanicsOnInvalid(t *testing.T) {
	t.Parallel()
	runPanicTests(t, []panicTestCase{
		{
			"negative",
			true,
			"k8senv: invalid release strategy: ReleaseStrategy(-1)",
			func() { k8senv.WithReleaseStrategy(k8senv.ReleaseStrategy(-1)) },
		},
		{
			"out_of_range",
			true,
			"k8senv: invalid release strategy: ReleaseStrategy(99)",
			func() { k8senv.WithReleaseStrategy(k8senv.ReleaseStrategy(99)) },
		},
		{"restart", false, "", func() { k8senv.WithReleaseStrategy(k8senv.ReleaseRestart) }},
		{"clean", false, "", func() { k8senv.WithReleaseStrategy(k8senv.ReleaseClean) }},
		{"none", false, "", func() { k8senv.WithReleaseStrategy(k8senv.ReleaseNone) }},
	})
}

func TestWithCleanupTimeoutPanicsOnInvalid(t *testing.T) {
	t.Parallel()
	runPanicTests(t, []panicTestCase{
		{
			"zero",
			true,
			"k8senv: cleanup timeout must be greater than 0, got 0s",
			func() { k8senv.WithCleanupTimeout(0) },
		},
		{
			"negative",
			true,
			"k8senv: cleanup timeout must be greater than 0, got -1s",
			func() { k8senv.WithCleanupTimeout(-1 * time.Second) },
		},
		{"valid", false, "", func() { k8senv.WithCleanupTimeout(30 * time.Second) }},
	})
}

func TestWithShutdownDrainTimeoutPanicsOnInvalid(t *testing.T) {
	t.Parallel()
	runPanicTests(t, []panicTestCase{
		{
			"zero",
			true,
			"k8senv: shutdown drain timeout must be greater than 0, got 0s",
			func() { k8senv.WithShutdownDrainTimeout(0) },
		},
		{
			"negative",
			true,
			"k8senv: shutdown drain timeout must be greater than 0, got -1s",
			func() { k8senv.WithShutdownDrainTimeout(-1 * time.Second) },
		},
		{"valid", false, "", func() { k8senv.WithShutdownDrainTimeout(1 * time.Minute) }},
	})
}

func TestWithInstanceStartTimeoutPanicsOnInvalid(t *testing.T) {
	t.Parallel()
	runPanicTests(t, []panicTestCase{
		{
			"zero",
			true,
			"k8senv: instance start timeout must be greater than 0, got 0s",
			func() { k8senv.WithInstanceStartTimeout(0) },
		},
		{
			"negative",
			true,
			"k8senv: instance start timeout must be greater than 0, got -1s",
			func() { k8senv.WithInstanceStartTimeout(-1 * time.Second) },
		},
		{"valid", false, "", func() { k8senv.WithInstanceStartTimeout(5 * time.Minute) }},
	})
}

func TestWithInstanceStopTimeoutPanicsOnInvalid(t *testing.T) {
	t.Parallel()
	runPanicTests(t, []panicTestCase{
		{
			"zero",
			true,
			"k8senv: instance stop timeout must be greater than 0, got 0s",
			func() { k8senv.WithInstanceStopTimeout(0) },
		},
		{
			"negative",
			true,
			"k8senv: instance stop timeout must be greater than 0, got -1s",
			func() { k8senv.WithInstanceStopTimeout(-1 * time.Second) },
		},
		{"valid", false, "", func() { k8senv.WithInstanceStopTimeout(10 * time.Second) }},
	})
}

func TestWithCRDCacheTimeoutPanicsOnInvalid(t *testing.T) {
	t.Parallel()
	runPanicTests(t, []panicTestCase{
		{
			"zero",
			true,
			"k8senv: CRD cache timeout must be greater than 0, got 0s",
			func() { k8senv.WithCRDCacheTimeout(0) },
		},
		{
			"negative",
			true,
			"k8senv: CRD cache timeout must be greater than 0, got -1s",
			func() { k8senv.WithCRDCacheTimeout(-1 * time.Second) },
		},
		{"valid", false, "", func() { k8senv.WithCRDCacheTimeout(10 * time.Minute) }},
	})
}

func TestWithEmptyStringOptionsPanic(t *testing.T) {
	t.Parallel()
	runPanicTests(t, []panicTestCase{
		{
			"prepopulateDB",
			true,
			"k8senv: prepopulate DB path must not be empty",
			func() { k8senv.WithPrepopulateDB("") },
		},
		{"crdDir", true, "k8senv: CRD directory path must not be empty", func() { k8senv.WithCRDDir("") }},
		{"baseDataDir", true, "k8senv: base data directory must not be empty", func() { k8senv.WithBaseDataDir("") }},
	})
}
