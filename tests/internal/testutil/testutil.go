//go:build integration

// Package testutil provides shared helpers for integration test packages.
package testutil

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/giantswarm/k8senv"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// SystemNamespaces returns a set of namespace names created by kube-apiserver
// that must survive cleanup. The authoritative source is
// k8senv.SystemNamespaceNames; this function converts the slice to a map for
// O(1) lookup convenience. Each call returns a fresh map that callers may
// modify freely.
func SystemNamespaces() map[string]struct{} {
	names := k8senv.SystemNamespaceNames()
	m := make(map[string]struct{}, len(names))
	for _, n := range names {
		m[n] = struct{}{}
	}

	return m
}

// nameCounter is an atomic counter used by UniqueName to generate resource
// names that are unique across parallel test goroutines.
var nameCounter atomic.Int64

// UniqueName returns a resource name that is unique across all parallel tests.
// It combines the given prefix with a monotonically increasing counter value.
// Use it for any Kubernetes resource name: namespaces, ConfigMaps, etc.
func UniqueName(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, nameCounter.Add(1))
}

// CreateNamespace creates a namespace with the given name and fails the test on error.
func CreateNamespace(ctx context.Context, t *testing.T, client kubernetes.Interface, name string) {
	t.Helper()

	ns := &v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: name},
	}
	if _, err := client.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create namespace %s: %v", name, err)
	}
}

// TestParallel returns the effective -test.parallel value for the current test
// binary. This mirrors Go's own default: if the flag is unset or unparseable,
// it falls back to GOMAXPROCS.
func TestParallel() int {
	f := flag.Lookup("test.parallel")
	if f == nil {
		n := runtime.GOMAXPROCS(0)
		slog.Info("test.parallel flag not found, falling back to GOMAXPROCS", "parallel", n)

		return n
	}

	n, err := strconv.Atoi(f.Value.String())
	if err != nil || n < 1 {
		fallback := runtime.GOMAXPROCS(0)
		slog.Warn("test.parallel flag unparseable, falling back to GOMAXPROCS",
			"raw", f.Value.String(), "error", err, "parallel", fallback)

		return fallback
	}

	slog.Info("using test.parallel flag value", "parallel", n)

	return n
}

// AcquireWithClient acquires an instance, gets its config, and creates a kubernetes client.
// Returns the instance and client. The caller is responsible for releasing the instance.
//
//nolint:ireturn // Test helper returns Instance and kubernetes.Interface matching the public API.
func AcquireWithClient(ctx context.Context, t *testing.T, mgr k8senv.Manager) (k8senv.Instance, kubernetes.Interface) {
	t.Helper()

	inst, err := mgr.Acquire(ctx)
	if err != nil {
		t.Fatalf("Failed to acquire instance: %v", err)
	}

	cfg, err := inst.Config()
	if err != nil {
		if relErr := inst.Release(); relErr != nil {
			t.Logf("release error: %v", relErr)
		}
		t.Fatalf("Failed to get config: %v", err)
	}

	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		if relErr := inst.Release(); relErr != nil {
			t.Logf("release error: %v", relErr)
		}
		t.Fatalf("Failed to create client: %v", err)
	}

	return inst, client
}

// AcquireWithGuardedRelease acquires an instance and client, then registers a
// deferred safety-net release that only fires if the caller has not already
// released the instance explicitly. It returns the instance, client, and a
// release function. Calling the release function performs the explicit release
// and disarms the safety net; subsequent calls to the release function are
// no-ops. The test fails immediately if the explicit release returns an error.
//
//nolint:ireturn // Test helper returns Instance and kubernetes.Interface matching the public API.
func AcquireWithGuardedRelease(
	ctx context.Context,
	t *testing.T,
	mgr k8senv.Manager,
) (k8senv.Instance, kubernetes.Interface, func()) {
	t.Helper()

	inst, client := AcquireWithClient(ctx, t, mgr)

	var releaseOnce sync.Once
	doRelease := func() {
		if err := inst.Release(); err != nil {
			t.Errorf("Release() failed: %v", err)
		}
	}
	t.Cleanup(func() { releaseOnce.Do(doRelease) })

	release := func() {
		t.Helper()
		releaseOnce.Do(doRelease)
	}

	return inst, client, release
}

// SetupTestLogging configures slog based on the K8SENV_LOG_LEVEL environment variable.
// This only affects test runs - the library itself inherits the application's logging config.
func SetupTestLogging() {
	levelStr := os.Getenv("K8SENV_LOG_LEVEL")
	if levelStr == "" {
		levelStr = "INFO"
	}

	var level slog.Level
	if err := level.UnmarshalText([]byte(levelStr)); err != nil {
		level = slog.LevelInfo
	}

	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})
	slog.SetDefault(slog.New(handler))

	k8senv.SetLogger(slog.Default().With("component", "k8senv"))
}

// RequireBinariesOrExit checks that kine and kube-apiserver are available,
// exiting the process (via os.Exit) if not. This is used in TestMain where
// *testing.T is not available.
func RequireBinariesOrExit() {
	for _, bin := range []struct {
		name string
		hint string
	}{
		{"kine", "Install kine: go install github.com/k3s-io/kine/cmd/kine@latest"},
		{"kube-apiserver", "Download from: https://dl.k8s.io/v1.35.0/bin/linux/amd64/kube-apiserver"},
	} {
		if _, err := exec.LookPath(bin.name); err != nil {
			fmt.Fprintf(os.Stderr, "%s binary not found in PATH\n%s\n", bin.name, bin.hint)
			os.Exit(1)
		}
		cmd := exec.Command(bin.name, "--version") //nolint:gosec // G204: binary names are hardcoded constants
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "%s binary exists but not working properly: %v\n", bin.name, err)
			os.Exit(1)
		}
	}
}

// RunTestMain sets up signal handling for graceful shutdown, runs all tests,
// then performs cleanup (shutdown + temp dir removal). Returns the exit code.
func RunTestMain(m *testing.M, mgr k8senv.Manager, tmpDir string) int {
	sigCh := make(chan os.Signal, 1)
	done := make(chan struct{})
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		select {
		case sig := <-sigCh:
			signal.Stop(sigCh) // Restore default handler so a second signal force-kills
			fmt.Fprintf(os.Stderr, "\nReceived %s, shutting down...\n", sig)
			if err := mgr.Shutdown(); err != nil {
				fmt.Fprintf(os.Stderr, "Shutdown error: %v\n", err)
			}
			_ = os.RemoveAll(tmpDir)
			os.Exit(1)
		case <-done:
			return
		}
	}()

	code := m.Run()

	signal.Stop(sigCh)
	close(done)
	if err := mgr.Shutdown(); err != nil {
		fmt.Fprintf(os.Stderr, "Shutdown error: %v\n", err)
	}
	_ = os.RemoveAll(tmpDir)

	return code
}

// SetupAndRun handles the standard TestMain boilerplate: flag parsing, logging
// setup, binary checks, temp dir creation, manager creation with
// WithBaseDataDir and WithAcquireTimeout prepended, initialization, test
// execution, and cleanup. The created manager is assigned to *mgr so tests can
// reference it. This function calls os.Exit and never returns.
//
//nolint:gocritic // ptrToRefParam: pointer-to-interface needed to assign the created manager back to the caller's variable.
func SetupAndRun(m *testing.M, mgr *k8senv.Manager, prefix string, opts ...k8senv.ManagerOption) {
	SetupAndRunWithHook(m, mgr, prefix, nil, opts...)
}

// SetupHook is called after temp dir creation, allowing custom setup that
// depends on the temp dir path. It returns additional manager options.
type SetupHook func(tmpDir string) ([]k8senv.ManagerOption, error)

// SetupAndRunWithHook is like SetupAndRun but calls hook after temp dir
// creation, prepending the returned options before opts.
//
//nolint:gocritic // ptrToRefParam: pointer-to-interface needed to assign the created manager back to the caller's variable.
func SetupAndRunWithHook(
	m *testing.M,
	mgr *k8senv.Manager,
	prefix string,
	hook SetupHook,
	opts ...k8senv.ManagerOption,
) {
	flag.Parse()
	SetupTestLogging()
	RequireBinariesOrExit()

	tmpDir, err := os.MkdirTemp("", prefix)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create temp dir: %v\n", err)
		os.Exit(1)
	}

	baseOpts := []k8senv.ManagerOption{
		k8senv.WithBaseDataDir(tmpDir),
		k8senv.WithAcquireTimeout(5 * time.Minute),
		k8senv.WithPoolSize(TestParallel()),
	}

	if hook != nil {
		extra, hookErr := hook(tmpDir)
		if hookErr != nil {
			fmt.Fprintf(os.Stderr, "setup hook failed: %v\n", hookErr)
			os.Exit(1)
		}

		baseOpts = append(baseOpts, extra...)
	}

	baseOpts = append(baseOpts, opts...)

	created := k8senv.NewManager(baseOpts...)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	if initErr := created.Initialize(ctx); initErr != nil {
		cancel()
		fmt.Fprintf(os.Stderr, "Initialize failed: %v\n", initErr)
		os.Exit(1)
	}

	cancel()

	*mgr = created

	os.Exit(RunTestMain(m, created, tmpDir))
}
