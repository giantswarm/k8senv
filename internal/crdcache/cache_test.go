package crdcache

import (
	"log/slog"
	"testing"
	"time"

	"github.com/giantswarm/k8senv/internal/netutil"
	"github.com/giantswarm/k8senv/internal/process"
)

func TestConfig_validate(t *testing.T) {
	t.Parallel()

	validConfig := func() Config {
		return Config{
			CRDDir:              "/some/crd/dir",
			CacheDir:            "/some/cache/dir",
			KineBinary:          "kine",
			KubeAPIServerBinary: "kube-apiserver",
			Timeout:             5 * time.Minute,
			PortRegistry:        netutil.NewPortRegistry(nil),
		}
	}

	tests := map[string]struct {
		modify  func(c *Config)
		wantErr bool
	}{
		"valid config": {
			modify:  func(_ *Config) {},
			wantErr: false,
		},
		"empty CRDDir": {
			modify:  func(c *Config) { c.CRDDir = "" },
			wantErr: true,
		},
		"empty CacheDir": {
			modify:  func(c *Config) { c.CacheDir = "" },
			wantErr: true,
		},
		"empty KineBinary": {
			modify:  func(c *Config) { c.KineBinary = "" },
			wantErr: true,
		},
		"empty KubeAPIServerBinary": {
			modify:  func(c *Config) { c.KubeAPIServerBinary = "" },
			wantErr: true,
		},
		"zero Timeout": {
			modify:  func(c *Config) { c.Timeout = 0 },
			wantErr: true,
		},
		"negative Timeout": {
			modify:  func(c *Config) { c.Timeout = -1 * time.Second },
			wantErr: true,
		},
		"nil PortRegistry": {
			modify:  func(c *Config) { c.PortRegistry = nil },
			wantErr: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			cfg := validConfig()
			tc.modify(&cfg)

			err := cfg.validate()
			if tc.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestConfig_logger(t *testing.T) {
	t.Parallel()

	t.Run("returns configured logger", func(t *testing.T) {
		t.Parallel()

		custom := slog.New(slog.NewTextHandler(nil, nil))
		cfg := Config{Logger: custom}

		if cfg.logger() != custom {
			t.Error("expected configured logger to be returned")
		}
	})

	t.Run("nil logger returns default", func(t *testing.T) {
		t.Parallel()

		cfg := Config{}

		got := cfg.logger()
		if got == nil {
			t.Fatal("expected non-nil logger")
		}
	})
}

func TestConfig_stopTimeout(t *testing.T) {
	t.Parallel()

	t.Run("returns configured timeout", func(t *testing.T) {
		t.Parallel()

		cfg := Config{StopTimeout: 30 * time.Second}

		if got := cfg.stopTimeout(); got != 30*time.Second {
			t.Errorf("stopTimeout() = %v, want %v", got, 30*time.Second)
		}
	})

	t.Run("zero returns default", func(t *testing.T) {
		t.Parallel()

		cfg := Config{}

		got := cfg.stopTimeout()
		want := process.DefaultStopTimeout
		if got != want {
			t.Errorf("stopTimeout() = %v, want %v", got, want)
		}
	})

	t.Run("negative returns default", func(t *testing.T) {
		t.Parallel()

		cfg := Config{StopTimeout: -1 * time.Second}

		got := cfg.stopTimeout()
		want := process.DefaultStopTimeout
		if got != want {
			t.Errorf("stopTimeout() = %v, want %v", got, want)
		}
	})
}
