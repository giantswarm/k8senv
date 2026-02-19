package k8senv_test

import (
	"fmt"
	"os"
	"path/filepath"
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

	snap := k8senv.ApplyOptionsForTesting()
	wantBaseDir := filepath.Join(os.TempDir(), k8senv.DefaultBaseDataDirName)

	if snap.PoolSize != k8senv.DefaultPoolSize {
		t.Errorf("PoolSize = %d, want %d", snap.PoolSize, k8senv.DefaultPoolSize)
	}
	if snap.ReleaseStrategy != k8senv.DefaultReleaseStrategy {
		t.Errorf("ReleaseStrategy = %v, want %v", snap.ReleaseStrategy, k8senv.DefaultReleaseStrategy)
	}
	if snap.KineBinary != k8senv.DefaultKineBinary {
		t.Errorf("KineBinary = %q, want %q", snap.KineBinary, k8senv.DefaultKineBinary)
	}
	if snap.KubeAPIServerBinary != k8senv.DefaultKubeAPIServerBinary {
		t.Errorf("KubeAPIServerBinary = %q, want %q", snap.KubeAPIServerBinary, k8senv.DefaultKubeAPIServerBinary)
	}
	if snap.AcquireTimeout != k8senv.DefaultAcquireTimeout {
		t.Errorf("AcquireTimeout = %v, want %v", snap.AcquireTimeout, k8senv.DefaultAcquireTimeout)
	}
	if snap.BaseDataDir != wantBaseDir {
		t.Errorf("BaseDataDir = %q, want %q", snap.BaseDataDir, wantBaseDir)
	}
	if snap.CRDCacheTimeout != k8senv.DefaultCRDCacheTimeout {
		t.Errorf("CRDCacheTimeout = %v, want %v", snap.CRDCacheTimeout, k8senv.DefaultCRDCacheTimeout)
	}
	if snap.InstanceStartTimeout != k8senv.DefaultInstanceStartTimeout {
		t.Errorf("InstanceStartTimeout = %v, want %v", snap.InstanceStartTimeout, k8senv.DefaultInstanceStartTimeout)
	}
	if snap.InstanceStopTimeout != k8senv.DefaultInstanceStopTimeout {
		t.Errorf("InstanceStopTimeout = %v, want %v", snap.InstanceStopTimeout, k8senv.DefaultInstanceStopTimeout)
	}
	if snap.CleanupTimeout != k8senv.DefaultCleanupTimeout {
		t.Errorf("CleanupTimeout = %v, want %v", snap.CleanupTimeout, k8senv.DefaultCleanupTimeout)
	}
	if snap.ShutdownDrainTimeout != k8senv.DefaultShutdownDrainTimeout {
		t.Errorf("ShutdownDrainTimeout = %v, want %v", snap.ShutdownDrainTimeout, k8senv.DefaultShutdownDrainTimeout)
	}
	if snap.DefaultDBPath != "" {
		t.Errorf("DefaultDBPath = %q, want empty", snap.DefaultDBPath)
	}
	if snap.CRDDir != "" {
		t.Errorf("CRDDir = %q, want empty", snap.CRDDir)
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
				if snap.DefaultDBPath != "/data/crds.db" {
					t.Errorf("DefaultDBPath = %q, want %q", snap.DefaultDBPath, "/data/crds.db")
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

	snap := k8senv.ApplyOptionsForTesting(
		k8senv.WithPoolSize(2),
		k8senv.WithReleaseStrategy(k8senv.ReleaseClean),
		k8senv.WithKineBinary("/opt/kine"),
		k8senv.WithKubeAPIServerBinary("/opt/kube-apiserver"),
		k8senv.WithAcquireTimeout(1*time.Minute),
		k8senv.WithBaseDataDir("/tmp/custom-k8senv"),
		k8senv.WithCleanupTimeout(45*time.Second),
	)

	if snap.PoolSize != 2 {
		t.Errorf("PoolSize = %d, want 2", snap.PoolSize)
	}
	if snap.ReleaseStrategy != k8senv.ReleaseClean {
		t.Errorf("ReleaseStrategy = %v, want ReleaseClean", snap.ReleaseStrategy)
	}
	if snap.KineBinary != "/opt/kine" {
		t.Errorf("KineBinary = %q, want %q", snap.KineBinary, "/opt/kine")
	}
	if snap.KubeAPIServerBinary != "/opt/kube-apiserver" {
		t.Errorf("KubeAPIServerBinary = %q, want %q", snap.KubeAPIServerBinary, "/opt/kube-apiserver")
	}
	if snap.AcquireTimeout != 1*time.Minute {
		t.Errorf("AcquireTimeout = %v, want 1m", snap.AcquireTimeout)
	}
	if snap.BaseDataDir != "/tmp/custom-k8senv" {
		t.Errorf("BaseDataDir = %q, want %q", snap.BaseDataDir, "/tmp/custom-k8senv")
	}
	if snap.CleanupTimeout != 45*time.Second {
		t.Errorf("CleanupTimeout = %v, want 45s", snap.CleanupTimeout)
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
