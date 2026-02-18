package core

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestManagerConfig_Validate(t *testing.T) {
	t.Parallel()
	validConfig := func() ManagerConfig {
		return ManagerConfig{
			KineBinary:           "kine",
			KubeAPIServerBinary:  "kube-apiserver",
			AcquireTimeout:       30 * time.Second,
			BaseDataDir:          "/tmp/k8senv",
			InstanceStartTimeout: 5 * time.Minute,
			InstanceStopTimeout:  10 * time.Second,
			CleanupTimeout:       30 * time.Second,
			CRDCacheTimeout:      5 * time.Minute,
			ShutdownDrainTimeout: 30 * time.Second,
		}
	}

	t.Run("valid config returns nil", func(t *testing.T) {
		t.Parallel()
		cfg := validConfig()
		if err := cfg.Validate(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	tests := map[string]struct {
		modify       func(c *ManagerConfig)
		wantContains string
	}{
		"empty kine binary": {
			modify:       func(c *ManagerConfig) { c.KineBinary = "" },
			wantContains: "kine binary",
		},
		"empty kube-apiserver binary": {
			modify:       func(c *ManagerConfig) { c.KubeAPIServerBinary = "" },
			wantContains: "kube-apiserver binary",
		},
		"zero acquire timeout": {
			modify:       func(c *ManagerConfig) { c.AcquireTimeout = 0 },
			wantContains: "acquire timeout",
		},
		"negative acquire timeout": {
			modify:       func(c *ManagerConfig) { c.AcquireTimeout = -1 },
			wantContains: "acquire timeout",
		},
		"empty base data dir": {
			modify:       func(c *ManagerConfig) { c.BaseDataDir = "" },
			wantContains: "base data directory",
		},
		"zero instance start timeout": {
			modify:       func(c *ManagerConfig) { c.InstanceStartTimeout = 0 },
			wantContains: "instance start timeout",
		},
		"zero instance stop timeout": {
			modify:       func(c *ManagerConfig) { c.InstanceStopTimeout = 0 },
			wantContains: "instance stop timeout",
		},
		"zero cleanup timeout": {
			modify:       func(c *ManagerConfig) { c.CleanupTimeout = 0 },
			wantContains: "cleanup timeout",
		},
		"zero CRD cache timeout": {
			modify:       func(c *ManagerConfig) { c.CRDCacheTimeout = 0 },
			wantContains: "CRD cache timeout",
		},
		"negative pool size": {
			modify:       func(c *ManagerConfig) { c.PoolSize = -1 },
			wantContains: "pool size",
		},
		"zero shutdown drain timeout": {
			modify:       func(c *ManagerConfig) { c.ShutdownDrainTimeout = 0 },
			wantContains: "shutdown drain timeout",
		},
		"invalid release strategy": {
			modify:       func(c *ManagerConfig) { c.ReleaseStrategy = ReleaseStrategy(99) },
			wantContains: "release strategy",
		},
		"invalid release strategy boundary": {
			modify:       func(c *ManagerConfig) { c.ReleaseStrategy = ReleaseStrategy(3) },
			wantContains: "release strategy",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cfg := validConfig()
			tc.modify(&cfg)

			err := cfg.Validate()
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantContains) {
				t.Errorf("error %q should contain %q", err.Error(), tc.wantContains)
			}
		})
	}

	t.Run("multiple errors joined", func(t *testing.T) {
		t.Parallel()
		cfg := ManagerConfig{PoolSize: -1} // zero values + negative pool size

		err := cfg.Validate()
		if err == nil {
			t.Fatal("expected error for zero-value config")
		}

		errMsg := err.Error()
		// Should contain errors for all invalid fields
		expectedParts := []string{
			"kine binary",
			"kube-apiserver binary",
			"acquire timeout",
			"base data directory",
			"instance start timeout",
			"instance stop timeout",
			"cleanup timeout",
			"CRD cache timeout",
			"shutdown drain timeout",
			"pool size",
		}

		for _, part := range expectedParts {
			if !strings.Contains(errMsg, part) {
				t.Errorf("error %q should contain %q", errMsg, part)
			}
		}
	})
}

func TestInstanceConfig_Validate(t *testing.T) {
	t.Parallel()
	validConfig := func() InstanceConfig {
		return InstanceConfig{
			StartTimeout:        5 * time.Minute,
			StopTimeout:         10 * time.Second,
			CleanupTimeout:      30 * time.Second,
			MaxStartRetries:     3,
			KineBinary:          "kine",
			KubeAPIServerBinary: "kube-apiserver",
		}
	}

	t.Run("valid config returns nil", func(t *testing.T) {
		t.Parallel()
		cfg := validConfig()
		if err := cfg.Validate(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	tests := map[string]struct {
		modify       func(c *InstanceConfig)
		wantContains string
	}{
		"zero start timeout": {
			modify:       func(c *InstanceConfig) { c.StartTimeout = 0 },
			wantContains: "start timeout",
		},
		"negative start timeout": {
			modify:       func(c *InstanceConfig) { c.StartTimeout = -1 },
			wantContains: "start timeout",
		},
		"zero stop timeout": {
			modify:       func(c *InstanceConfig) { c.StopTimeout = 0 },
			wantContains: "stop timeout",
		},
		"zero cleanup timeout": {
			modify:       func(c *InstanceConfig) { c.CleanupTimeout = 0 },
			wantContains: "cleanup timeout",
		},
		"zero max start retries": {
			modify:       func(c *InstanceConfig) { c.MaxStartRetries = 0 },
			wantContains: "max start retries",
		},
		"negative max start retries": {
			modify:       func(c *InstanceConfig) { c.MaxStartRetries = -1 },
			wantContains: "max start retries",
		},
		"empty kine binary": {
			modify:       func(c *InstanceConfig) { c.KineBinary = "" },
			wantContains: "kine binary",
		},
		"empty kube-apiserver binary": {
			modify:       func(c *InstanceConfig) { c.KubeAPIServerBinary = "" },
			wantContains: "kube-apiserver binary",
		},
		"invalid release strategy": {
			modify:       func(c *InstanceConfig) { c.ReleaseStrategy = ReleaseStrategy(99) },
			wantContains: "release strategy",
		},
		"invalid release strategy boundary": {
			modify:       func(c *InstanceConfig) { c.ReleaseStrategy = ReleaseStrategy(3) },
			wantContains: "release strategy",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cfg := validConfig()
			tc.modify(&cfg)

			err := cfg.Validate()
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantContains) {
				t.Errorf("error %q should contain %q", err.Error(), tc.wantContains)
			}
		})
	}

	t.Run("multiple errors joined", func(t *testing.T) {
		t.Parallel()
		cfg := InstanceConfig{} // all zero values

		err := cfg.Validate()
		if err == nil {
			t.Fatal("expected error for zero-value config")
		}

		errMsg := err.Error()
		expectedParts := []string{
			"start timeout",
			"stop timeout",
			"cleanup timeout",
			"max start retries",
			"kine binary",
			"kube-apiserver binary",
		}

		for _, part := range expectedParts {
			if !strings.Contains(errMsg, part) {
				t.Errorf("error %q should contain %q", errMsg, part)
			}
		}
	})

	t.Run("optional CachedDBPath", func(t *testing.T) {
		t.Parallel()
		// CachedDBPath is optional - empty is valid
		cfg := validConfig()
		cfg.CachedDBPath = ""
		if err := cfg.Validate(); err != nil {
			t.Fatalf("empty CachedDBPath should be valid: %v", err)
		}

		cfg.CachedDBPath = "/some/path.db"
		if err := cfg.Validate(); err != nil {
			t.Fatalf("non-empty CachedDBPath should be valid: %v", err)
		}
	})
}

// TestManagerConfigFieldCount is a canary test that detects when fields are
// added to ManagerConfig without updating the public API in the root package.
//
// If this test fails, you added a field to core.ManagerConfig. You must also:
//  1. Add a public WithXxx option function in options.go
//  2. Update expectedFields below to match the new count
func TestManagerConfigFieldCount(t *testing.T) {
	t.Parallel()
	const expectedFields = 13 // Update this when adding new fields to ManagerConfig.

	actual := reflect.TypeFor[ManagerConfig]().NumField()
	if actual != expectedFields {
		t.Errorf("ManagerConfig has %d fields, expected %d; "+
			"if you added a field, also add a WithXxx option in the root package options.go",
			actual, expectedFields)
	}
}
