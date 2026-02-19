package k8senv_test

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
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
		switch {
		case shouldPanic && r == nil:
			t.Fatal("expected panic but didn't get one")
		case !shouldPanic && r != nil:
			t.Fatalf("unexpected panic: %v", r)
		case shouldPanic:
			if msg := fmt.Sprint(r); msg != wantMsg {
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
			name:     "zero",
			panics:   true,
			panicMsg: "k8senv: acquire timeout must be greater than 0, got 0s",
			fn:       func() { k8senv.WithAcquireTimeout(0) },
		},
		{
			name:     "negative",
			panics:   true,
			panicMsg: "k8senv: acquire timeout must be greater than 0, got -1s",
			fn:       func() { k8senv.WithAcquireTimeout(-1 * time.Second) },
		},
		{name: "valid", fn: func() { k8senv.WithAcquireTimeout(1 * time.Second) }},
	})
}

func TestWithKineBinaryPanicsOnEmpty(t *testing.T) {
	t.Parallel()
	runPanicTests(t, []panicTestCase{
		{
			name:     "empty",
			panics:   true,
			panicMsg: "k8senv: kine binary path must not be empty",
			fn:       func() { k8senv.WithKineBinary("") },
		},
		{name: "valid", fn: func() { k8senv.WithKineBinary("/usr/local/bin/kine") }},
	})
}

func TestWithPoolSizePanicsOnInvalid(t *testing.T) {
	t.Parallel()
	runPanicTests(t, []panicTestCase{
		{
			name:     "negative",
			panics:   true,
			panicMsg: "k8senv: pool size must not be negative, got -1",
			fn:       func() { k8senv.WithPoolSize(-1) },
		},
		{name: "zero_unlimited", fn: func() { k8senv.WithPoolSize(0) }},
		{name: "valid", fn: func() { k8senv.WithPoolSize(5) }},
	})
}

func TestWithKubeAPIServerBinaryPanicsOnEmpty(t *testing.T) {
	t.Parallel()
	runPanicTests(t, []panicTestCase{
		{
			name:     "empty",
			panics:   true,
			panicMsg: "k8senv: kube-apiserver binary path must not be empty",
			fn:       func() { k8senv.WithKubeAPIServerBinary("") },
		},
		{name: "valid", fn: func() { k8senv.WithKubeAPIServerBinary("/usr/local/bin/kube-apiserver") }},
	})
}

func TestWithReleaseStrategyPanicsOnInvalid(t *testing.T) {
	t.Parallel()
	runPanicTests(t, []panicTestCase{
		{
			name:     "negative",
			panics:   true,
			panicMsg: "k8senv: invalid release strategy: ReleaseStrategy(-1)",
			fn:       func() { k8senv.WithReleaseStrategy(k8senv.ReleaseStrategy(-1)) },
		},
		{
			name:     "out_of_range",
			panics:   true,
			panicMsg: "k8senv: invalid release strategy: ReleaseStrategy(99)",
			fn:       func() { k8senv.WithReleaseStrategy(k8senv.ReleaseStrategy(99)) },
		},
		{name: "restart", fn: func() { k8senv.WithReleaseStrategy(k8senv.ReleaseRestart) }},
		{name: "clean", fn: func() { k8senv.WithReleaseStrategy(k8senv.ReleaseClean) }},
		{name: "none", fn: func() { k8senv.WithReleaseStrategy(k8senv.ReleaseNone) }},
	})
}

func TestWithCleanupTimeoutPanicsOnInvalid(t *testing.T) {
	t.Parallel()
	runPanicTests(t, []panicTestCase{
		{
			name:     "zero",
			panics:   true,
			panicMsg: "k8senv: cleanup timeout must be greater than 0, got 0s",
			fn:       func() { k8senv.WithCleanupTimeout(0) },
		},
		{
			name:     "negative",
			panics:   true,
			panicMsg: "k8senv: cleanup timeout must be greater than 0, got -1s",
			fn:       func() { k8senv.WithCleanupTimeout(-1 * time.Second) },
		},
		{name: "valid", fn: func() { k8senv.WithCleanupTimeout(30 * time.Second) }},
	})
}

func TestWithShutdownDrainTimeoutPanicsOnInvalid(t *testing.T) {
	t.Parallel()
	runPanicTests(t, []panicTestCase{
		{
			name:     "zero",
			panics:   true,
			panicMsg: "k8senv: shutdown drain timeout must be greater than 0, got 0s",
			fn:       func() { k8senv.WithShutdownDrainTimeout(0) },
		},
		{
			name:     "negative",
			panics:   true,
			panicMsg: "k8senv: shutdown drain timeout must be greater than 0, got -1s",
			fn:       func() { k8senv.WithShutdownDrainTimeout(-1 * time.Second) },
		},
		{name: "valid", fn: func() { k8senv.WithShutdownDrainTimeout(1 * time.Minute) }},
	})
}

func TestWithInstanceStartTimeoutPanicsOnInvalid(t *testing.T) {
	t.Parallel()
	runPanicTests(t, []panicTestCase{
		{
			name:     "zero",
			panics:   true,
			panicMsg: "k8senv: instance start timeout must be greater than 0, got 0s",
			fn:       func() { k8senv.WithInstanceStartTimeout(0) },
		},
		{
			name:     "negative",
			panics:   true,
			panicMsg: "k8senv: instance start timeout must be greater than 0, got -1s",
			fn:       func() { k8senv.WithInstanceStartTimeout(-1 * time.Second) },
		},
		{name: "valid", fn: func() { k8senv.WithInstanceStartTimeout(5 * time.Minute) }},
	})
}

func TestWithInstanceStopTimeoutPanicsOnInvalid(t *testing.T) {
	t.Parallel()
	runPanicTests(t, []panicTestCase{
		{
			name:     "zero",
			panics:   true,
			panicMsg: "k8senv: instance stop timeout must be greater than 0, got 0s",
			fn:       func() { k8senv.WithInstanceStopTimeout(0) },
		},
		{
			name:     "negative",
			panics:   true,
			panicMsg: "k8senv: instance stop timeout must be greater than 0, got -1s",
			fn:       func() { k8senv.WithInstanceStopTimeout(-1 * time.Second) },
		},
		{name: "valid", fn: func() { k8senv.WithInstanceStopTimeout(10 * time.Second) }},
	})
}

func TestWithCRDCacheTimeoutPanicsOnInvalid(t *testing.T) {
	t.Parallel()
	runPanicTests(t, []panicTestCase{
		{
			name:     "zero",
			panics:   true,
			panicMsg: "k8senv: CRD cache timeout must be greater than 0, got 0s",
			fn:       func() { k8senv.WithCRDCacheTimeout(0) },
		},
		{
			name:     "negative",
			panics:   true,
			panicMsg: "k8senv: CRD cache timeout must be greater than 0, got -1s",
			fn:       func() { k8senv.WithCRDCacheTimeout(-1 * time.Second) },
		},
		{name: "valid", fn: func() { k8senv.WithCRDCacheTimeout(10 * time.Minute) }},
	})
}

func TestWithEmptyStringOptionsPanic(t *testing.T) {
	t.Parallel()
	runPanicTests(t, []panicTestCase{
		{
			name:     "prepopulateDB",
			panics:   true,
			panicMsg: "k8senv: prepopulate DB path must not be empty",
			fn:       func() { k8senv.WithPrepopulateDB("") },
		},
		{
			name:     "crdDir",
			panics:   true,
			panicMsg: "k8senv: CRD directory path must not be empty",
			fn:       func() { k8senv.WithCRDDir("") },
		},
		{
			name:     "baseDataDir",
			panics:   true,
			panicMsg: "k8senv: base data directory must not be empty",
			fn:       func() { k8senv.WithBaseDataDir("") },
		},
	})
}

func TestOptionApplicationDefaults(t *testing.T) {
	t.Parallel()

	got := k8senv.ApplyOptionsForTesting()
	want := k8senv.ConfigSnapshot{
		PoolSize:             k8senv.DefaultPoolSize,
		ReleaseStrategy:      k8senv.DefaultReleaseStrategy,
		KineBinary:           k8senv.DefaultKineBinary,
		KubeAPIServerBinary:  k8senv.DefaultKubeAPIServerBinary,
		AcquireTimeout:       k8senv.DefaultAcquireTimeout,
		BaseDataDir:          filepath.Join(os.TempDir(), k8senv.DefaultBaseDataDirName),
		CRDCacheTimeout:      k8senv.DefaultCRDCacheTimeout,
		InstanceStartTimeout: k8senv.DefaultInstanceStartTimeout,
		InstanceStopTimeout:  k8senv.DefaultInstanceStopTimeout,
		CleanupTimeout:       k8senv.DefaultCleanupTimeout,
		ShutdownDrainTimeout: k8senv.DefaultShutdownDrainTimeout,
	}

	if got != want {
		t.Errorf("ApplyOptionsForTesting() =\n  %+v\nwant\n  %+v", got, want)
	}
}

func TestOptionApplicationOverrides(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		opt    k8senv.ManagerOption
		verify func(t *testing.T, snap k8senv.ConfigSnapshot)
	}{
		{
			name: "WithPoolSize",
			opt:  k8senv.WithPoolSize(8),
			verify: func(t *testing.T, snap k8senv.ConfigSnapshot) {
				t.Helper()
				if snap.PoolSize != 8 {
					t.Errorf("PoolSize = %d, want 8", snap.PoolSize)
				}
			},
		},
		{
			name: "WithPoolSize_zero_unlimited",
			opt:  k8senv.WithPoolSize(0),
			verify: func(t *testing.T, snap k8senv.ConfigSnapshot) {
				t.Helper()
				if snap.PoolSize != 0 {
					t.Errorf("PoolSize = %d, want 0", snap.PoolSize)
				}
			},
		},
		{
			name: "WithReleaseStrategy_clean",
			opt:  k8senv.WithReleaseStrategy(k8senv.ReleaseClean),
			verify: func(t *testing.T, snap k8senv.ConfigSnapshot) {
				t.Helper()
				if snap.ReleaseStrategy != k8senv.ReleaseClean {
					t.Errorf("ReleaseStrategy = %v, want ReleaseClean", snap.ReleaseStrategy)
				}
			},
		},
		{
			name: "WithReleaseStrategy_none",
			opt:  k8senv.WithReleaseStrategy(k8senv.ReleaseNone),
			verify: func(t *testing.T, snap k8senv.ConfigSnapshot) {
				t.Helper()
				if snap.ReleaseStrategy != k8senv.ReleaseNone {
					t.Errorf("ReleaseStrategy = %v, want ReleaseNone", snap.ReleaseStrategy)
				}
			},
		},
		{
			name: "WithKineBinary",
			opt:  k8senv.WithKineBinary("/custom/kine"),
			verify: func(t *testing.T, snap k8senv.ConfigSnapshot) {
				t.Helper()
				if snap.KineBinary != "/custom/kine" {
					t.Errorf("KineBinary = %q, want %q", snap.KineBinary, "/custom/kine")
				}
			},
		},
		{
			name: "WithKubeAPIServerBinary",
			opt:  k8senv.WithKubeAPIServerBinary("/custom/kube-apiserver"),
			verify: func(t *testing.T, snap k8senv.ConfigSnapshot) {
				t.Helper()
				if snap.KubeAPIServerBinary != "/custom/kube-apiserver" {
					t.Errorf("KubeAPIServerBinary = %q, want %q", snap.KubeAPIServerBinary, "/custom/kube-apiserver")
				}
			},
		},
		{
			name: "WithAcquireTimeout",
			opt:  k8senv.WithAcquireTimeout(2 * time.Minute),
			verify: func(t *testing.T, snap k8senv.ConfigSnapshot) {
				t.Helper()
				if snap.AcquireTimeout != 2*time.Minute {
					t.Errorf("AcquireTimeout = %v, want 2m", snap.AcquireTimeout)
				}
			},
		},
		{
			name: "WithPrepopulateDB",
			opt:  k8senv.WithPrepopulateDB("/data/crds.db"),
			verify: func(t *testing.T, snap k8senv.ConfigSnapshot) {
				t.Helper()
				if snap.PrepopulateDBPath != "/data/crds.db" {
					t.Errorf("PrepopulateDBPath = %q, want %q", snap.PrepopulateDBPath, "/data/crds.db")
				}
			},
		},
		{
			name: "WithCRDDir",
			opt:  k8senv.WithCRDDir("/testdata/crds"),
			verify: func(t *testing.T, snap k8senv.ConfigSnapshot) {
				t.Helper()
				if snap.CRDDir != "/testdata/crds" {
					t.Errorf("CRDDir = %q, want %q", snap.CRDDir, "/testdata/crds")
				}
			},
		},
		{
			name: "WithBaseDataDir",
			opt:  k8senv.WithBaseDataDir("/custom/data"),
			verify: func(t *testing.T, snap k8senv.ConfigSnapshot) {
				t.Helper()
				if snap.BaseDataDir != "/custom/data" {
					t.Errorf("BaseDataDir = %q, want %q", snap.BaseDataDir, "/custom/data")
				}
			},
		},
		{
			name: "WithCRDCacheTimeout",
			opt:  k8senv.WithCRDCacheTimeout(10 * time.Minute),
			verify: func(t *testing.T, snap k8senv.ConfigSnapshot) {
				t.Helper()
				if snap.CRDCacheTimeout != 10*time.Minute {
					t.Errorf("CRDCacheTimeout = %v, want 10m", snap.CRDCacheTimeout)
				}
			},
		},
		{
			name: "WithInstanceStartTimeout",
			opt:  k8senv.WithInstanceStartTimeout(3 * time.Minute),
			verify: func(t *testing.T, snap k8senv.ConfigSnapshot) {
				t.Helper()
				if snap.InstanceStartTimeout != 3*time.Minute {
					t.Errorf("InstanceStartTimeout = %v, want 3m", snap.InstanceStartTimeout)
				}
			},
		},
		{
			name: "WithInstanceStopTimeout",
			opt:  k8senv.WithInstanceStopTimeout(30 * time.Second),
			verify: func(t *testing.T, snap k8senv.ConfigSnapshot) {
				t.Helper()
				if snap.InstanceStopTimeout != 30*time.Second {
					t.Errorf("InstanceStopTimeout = %v, want 30s", snap.InstanceStopTimeout)
				}
			},
		},
		{
			name: "WithCleanupTimeout",
			opt:  k8senv.WithCleanupTimeout(1 * time.Minute),
			verify: func(t *testing.T, snap k8senv.ConfigSnapshot) {
				t.Helper()
				if snap.CleanupTimeout != 1*time.Minute {
					t.Errorf("CleanupTimeout = %v, want 1m", snap.CleanupTimeout)
				}
			},
		},
		{
			name: "WithShutdownDrainTimeout",
			opt:  k8senv.WithShutdownDrainTimeout(2 * time.Minute),
			verify: func(t *testing.T, snap k8senv.ConfigSnapshot) {
				t.Helper()
				if snap.ShutdownDrainTimeout != 2*time.Minute {
					t.Errorf("ShutdownDrainTimeout = %v, want 2m", snap.ShutdownDrainTimeout)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			snap := k8senv.ApplyOptionsForTesting(tc.opt)
			tc.verify(t, snap)
		})
	}
}

func TestOptionApplicationMultipleOptions(t *testing.T) {
	t.Parallel()

	got := k8senv.ApplyOptionsForTesting(
		k8senv.WithPoolSize(2),
		k8senv.WithReleaseStrategy(k8senv.ReleaseClean),
		k8senv.WithKineBinary("/opt/kine"),
		k8senv.WithKubeAPIServerBinary("/opt/kube-apiserver"),
		k8senv.WithAcquireTimeout(1*time.Minute),
		k8senv.WithBaseDataDir("/tmp/custom-k8senv"),
		k8senv.WithCleanupTimeout(45*time.Second),
	)
	want := k8senv.ConfigSnapshot{
		PoolSize:             2,
		ReleaseStrategy:      k8senv.ReleaseClean,
		KineBinary:           "/opt/kine",
		KubeAPIServerBinary:  "/opt/kube-apiserver",
		AcquireTimeout:       1 * time.Minute,
		BaseDataDir:          "/tmp/custom-k8senv",
		CleanupTimeout:       45 * time.Second,
		CRDCacheTimeout:      k8senv.DefaultCRDCacheTimeout,
		InstanceStartTimeout: k8senv.DefaultInstanceStartTimeout,
		InstanceStopTimeout:  k8senv.DefaultInstanceStopTimeout,
		ShutdownDrainTimeout: k8senv.DefaultShutdownDrainTimeout,
	}

	if got != want {
		t.Errorf("ApplyOptionsForTesting() =\n  %+v\nwant\n  %+v", got, want)
	}
}

func TestOptionApplicationLastWriteWins(t *testing.T) {
	t.Parallel()

	snap := k8senv.ApplyOptionsForTesting(
		k8senv.WithPoolSize(2),
		k8senv.WithPoolSize(8),
	)

	if snap.PoolSize != 8 {
		t.Errorf("PoolSize = %d, want 8 (last write wins)", snap.PoolSize)
	}
}

// TestConfigSnapshotFieldCount is a canary test that detects when
// core.ManagerConfig fields are added without updating ConfigSnapshot and
// ApplyOptionsForTesting. ConfigSnapshot must mirror every ManagerConfig field
// so that option tests exercise the full configuration surface.
//
// If this test fails, a field was added to core.ManagerConfig. You must also:
//  1. Add the field to ConfigSnapshot in export_test.go
//  2. Copy the field in ApplyOptionsForTesting in export_test.go
//  3. Update expectedFields below to match the new count
func TestConfigSnapshotFieldCount(t *testing.T) {
	t.Parallel()

	// ConfigSnapshot must have 13 fields, matching core.ManagerConfig (see
	// TestManagerConfigFieldCount in internal/core/config_test.go).
	const expectedFields = 13

	actual := reflect.TypeFor[k8senv.ConfigSnapshot]().NumField()
	if actual != expectedFields {
		t.Errorf("ConfigSnapshot has %d fields, expected %d; "+
			"if you added a field to core.ManagerConfig, also update "+
			"ConfigSnapshot and ApplyOptionsForTesting in export_test.go",
			actual, expectedFields)
	}
}

// TestConfigDiffsCoversAllFields is a canary test that detects when a field is
// added to core.ManagerConfig without a corresponding entry in configDiffs.
// It constructs two configs that differ on every field and verifies the number
// of reported diffs equals the total field count.
//
// If this test fails, a field was added to core.ManagerConfig. You must also:
//  1. Add a diff* call for the new field in configDiffs (k8senv.go)
//  2. No constant update needed -- the test derives the expected count via reflection
func TestConfigDiffsCoversAllFields(t *testing.T) {
	t.Parallel()

	// "stored" uses defaults (no options). "incoming" overrides every field
	// to a non-default value so that configDiffs reports a diff for each one.
	incomingOpts := []k8senv.ManagerOption{
		k8senv.WithPoolSize(999),
		k8senv.WithReleaseStrategy(k8senv.ReleaseClean),
		k8senv.WithKineBinary("/canary/kine"),
		k8senv.WithKubeAPIServerBinary("/canary/kube-apiserver"),
		k8senv.WithAcquireTimeout(999 * time.Hour),
		k8senv.WithPrepopulateDB("/canary/prepopulate.db"),
		k8senv.WithBaseDataDir("/canary/data"),
		k8senv.WithCRDDir("/canary/crds"),
		k8senv.WithCRDCacheTimeout(999 * time.Hour),
		k8senv.WithInstanceStartTimeout(999 * time.Hour),
		k8senv.WithInstanceStopTimeout(999 * time.Hour),
		k8senv.WithCleanupTimeout(999 * time.Hour),
		k8senv.WithShutdownDrainTimeout(999 * time.Hour),
	}

	diffs := k8senv.ConfigDiffsForTesting(nil, incomingOpts)
	wantCount := k8senv.ManagerConfigFieldCount()

	if len(diffs) != wantCount {
		t.Errorf("configDiffs reported %d diffs, want %d (one per ManagerConfig field); "+
			"if you added a field to core.ManagerConfig, also add a diff entry in configDiffs (k8senv.go)\n"+
			"  reported diffs: %v", len(diffs), wantCount, diffs)
	}
}
