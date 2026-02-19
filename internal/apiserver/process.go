package apiserver

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/giantswarm/k8senv/internal/fileutil"
	"github.com/giantswarm/k8senv/internal/process"
	"golang.org/x/sync/errgroup"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// Compile-time interface satisfaction check.
var _ process.Stoppable = (*Process)(nil)

// Config holds the configuration for a kube-apiserver process.
type Config struct {
	Binary         string // Path to kube-apiserver binary (default: "kube-apiserver")
	DataDir        string // Working directory for logs and config files
	Port           int    // Secure port
	EtcdEndpoint   string // Kine/etcd endpoint URL (e.g., "http://127.0.0.1:2379")
	KubeconfigPath string // Output path for kubeconfig file

	// Logger (optional, defaults to slog.Default())
	Logger *slog.Logger
}

// testAuthToken is the static bearer token for kube-apiserver authentication
// in the test environment. Safe because the API server binds to 127.0.0.1 only
// and uses ephemeral self-signed certificates. Do NOT reuse in production or
// network-accessible environments.
const testAuthToken = "test-token"

// healthCheckTimeout is the per-request timeout for the HTTP client used
// to poll the kube-apiserver /livez endpoint during readiness checks.
const healthCheckTimeout = 5 * time.Second

// readinessPollInterval is the interval between consecutive /livez health
// check requests when waiting for the kube-apiserver to become ready.
// 100ms is used (rather than 10ms) because each attempt requires a full
// HTTPS round-trip with TLS handshake, and apiserver typically takes 3-5s
// to start. A fixed short interval is preferred over adaptive backoff since
// this is a test framework where instances must start as quickly as possible.
const readinessPollInterval = 100 * time.Millisecond

// authConfigYAML is the AuthenticationConfiguration that allows anonymous
// access to health check endpoints. Extracted as a constant so that
// indentation changes during refactoring cannot silently break the YAML.
const authConfigYAML = `apiVersion: apiserver.config.k8s.io/v1
kind: AuthenticationConfiguration
anonymous:
  enabled: true
  conditions:
  - path: /livez
  - path: /readyz
  - path: /healthz
`

// Process manages a kube-apiserver process lifecycle.
type Process struct {
	config Config
	base   process.BaseProcess
}

// validate checks that all required Config fields are set and returns an error
// describing the first missing or invalid field.
func (c Config) validate() error {
	if c.Binary == "" {
		return errors.New("binary path must not be empty")
	}
	if c.DataDir == "" {
		return errors.New("data dir must not be empty")
	}
	if c.Port <= 0 || c.Port > 65535 {
		return errors.New("port must be between 1 and 65535")
	}
	if c.EtcdEndpoint == "" {
		return errors.New("etcd endpoint must not be empty")
	}
	if c.KubeconfigPath == "" {
		return errors.New("kubeconfig path must not be empty")
	}
	return nil
}

// New creates a new kube-apiserver Process with the given configuration.
// It returns an error if any required field is missing or invalid.
func New(cfg Config) (*Process, error) {
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("invalid apiserver config: %w", err)
	}
	return &Process{
		config: cfg,
		base:   process.NewBaseProcess("kube-apiserver", cfg.Logger),
	}, nil
}

// startFiles holds the file paths produced by parallel file preparation.
// Each field is written by exactly one goroutine, making data ownership
// explicit and preventing accidental races if new preparation steps are added.
type startFiles struct {
	tokenFilePath  string // Written by the writeTokenFile goroutine.
	certDir        string // Written by the setupCertsAndKeys goroutine.
	saKeyPath      string // Written by the setupCertsAndKeys goroutine.
	authConfigPath string // Written by the writeAuthConfig goroutine.
}

// Start launches the kube-apiserver process. It prepares token, certificate,
// and auth config files in parallel before starting the process.
func (p *Process) Start(ctx context.Context) error {
	if p.base.IsStarted() {
		return process.ErrAlreadyStarted
	}

	dir := p.config.DataDir

	files, err := p.prepareFiles(dir)
	if err != nil {
		return fmt.Errorf("start apiserver: %w", err)
	}

	args := p.buildArgs(files.certDir, files.authConfigPath, files.tokenFilePath, files.saKeyPath)

	cmd := exec.CommandContext(ctx, p.config.Binary, args...)
	if err := p.base.SetupAndStart(cmd, dir); err != nil {
		return fmt.Errorf("setup and start apiserver process: %w", err)
	}
	return nil
}

// prepareFiles creates token, certificate, and auth config files in parallel.
// Each goroutine writes to a distinct field of the result struct, making data
// ownership explicit. Concurrent writes to different struct fields are safe in
// Go, and g.Wait() provides the happens-before guarantee that all writes are
// visible to the caller.
func (p *Process) prepareFiles(dir string) (startFiles, error) {
	var files startFiles
	var g errgroup.Group

	g.Go(func() error {
		path, err := p.writeTokenFile(dir)
		if err != nil {
			return err
		}
		files.tokenFilePath = path
		return nil
	})
	g.Go(func() error {
		certDir, saKeyPath, err := p.setupCertsAndKeys(dir)
		if err != nil {
			return err
		}
		files.certDir = certDir
		files.saKeyPath = saKeyPath
		return nil
	})
	g.Go(func() error {
		path, err := p.writeAuthConfig(dir)
		if err != nil {
			return err
		}
		files.authConfigPath = path
		return nil
	})

	if err := g.Wait(); err != nil {
		return startFiles{}, fmt.Errorf("prepare apiserver files: %w", err)
	}
	return files, nil
}

// writeTokenFile creates the token authentication file.
// Format: token,user,uid,"group1,group2"
// The system:masters group has cluster-admin rights by default in RBAC.
func (p *Process) writeTokenFile(dir string) (string, error) {
	tokenFilePath := filepath.Join(dir, "token.csv")
	if err := os.WriteFile(
		tokenFilePath,
		[]byte(testAuthToken+",admin,admin,\"system:masters\"\n"),
		0o600,
	); err != nil {
		return "", fmt.Errorf("create token file: %w", err)
	}
	return tokenFilePath, nil
}

// setupCertsAndKeys creates the certificate directory and generates an ECDSA
// P-256 key pair for service account signing. ECDSA P-256 is used instead of
// RSA 2048 for faster key generation (<1ms vs ~50-200ms). It returns the cert
// directory path and the service account key file path.
func (p *Process) setupCertsAndKeys(dir string) (certDir string, saKeyPath string, err error) {
	certDir = filepath.Join(dir, "certs")
	if err := fileutil.EnsureDir(certDir); err != nil {
		return "", "", fmt.Errorf("create cert dir: %w", err)
	}

	saKeyPath = filepath.Join(certDir, "sa.key")
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", fmt.Errorf("generate service account key: %w", err)
	}

	ecBytes, err := x509.MarshalECPrivateKey(privateKey)
	if err != nil {
		return "", "", fmt.Errorf("marshal service account key: %w", err)
	}

	privateKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: ecBytes,
	})
	if err := os.WriteFile(saKeyPath, privateKeyPEM, 0o600); err != nil {
		return "", "", fmt.Errorf("write service account key: %w", err)
	}
	return certDir, saKeyPath, nil
}

// writeAuthConfig creates the AuthenticationConfiguration YAML file that
// allows anonymous access to health check endpoints (/livez, /readyz, /healthz).
func (p *Process) writeAuthConfig(dir string) (string, error) {
	authConfigPath := filepath.Join(dir, "auth-config.yaml")
	if err := os.WriteFile(authConfigPath, []byte(authConfigYAML), 0o600); err != nil {
		return "", fmt.Errorf("create auth config file: %w", err)
	}
	return authConfigPath, nil
}

// buildArgs assembles the kube-apiserver command-line arguments.
func (p *Process) buildArgs(certDir, authConfigPath, tokenFilePath, saKeyPath string) []string {
	return []string{
		// Storage
		"--etcd-servers=" + p.config.EtcdEndpoint,

		// API Server binding
		"--bind-address=127.0.0.1",
		fmt.Sprintf("--secure-port=%d", p.config.Port),

		// TLS (self-signed for testing)
		"--cert-dir=" + certDir,

		// Authentication - use AuthenticationConfiguration for anonymous health endpoints
		"--authentication-config=" + authConfigPath,
		"--token-auth-file=" + tokenFilePath,
		// Use AlwaysAllow for faster startup - RBAC bootstrap is slow (~6s)
		// Tests using token auth with system:masters group still work correctly
		"--authorization-mode=AlwaysAllow",

		// Service account configuration (required)
		"--service-account-key-file=" + saKeyPath,
		"--service-account-signing-key-file=" + saKeyPath,
		"--service-account-issuer=https://kubernetes.default.svc",

		// Required service configuration
		"--service-cluster-ip-range=10.96.0.0/12",

		// Disable admission plugins for simplicity
		"--disable-admission-plugins=ServiceAccount",

		// Disable the watch cache so all reads go directly to kine/SQLite.
		// The watch cache is designed to reduce load on remote etcd clusters,
		// but k8senv uses local SQLite where the benefit is negligible. More
		// importantly, the cache can serve stale List results for a few
		// milliseconds after a write, which causes namespace cleanup in
		// Release() to miss recently-created namespaces under high concurrency.
		"--watch-cache=false",

		// Logging
		"--v=2",
	}
}

// WaitReady polls the /livez endpoint until it returns 200.
func (p *Process) WaitReady(ctx context.Context, timeout time.Duration) error {
	httpClient := &http.Client{
		Transport: &http.Transport{
			// InsecureSkipVerify is safe here because:
			// 1. This is a testing framework - not production code
			// 2. The API server uses ephemeral self-signed certificates generated at startup
			// 3. We only connect to localhost (127.0.0.1) - no network exposure
			// 4. The certificates are not known ahead of time and have no CA to verify against
			//nolint:gosec // G402: InsecureSkipVerify is appropriate for testing framework
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},

			// DisableKeepAlives ensures each health-check request opens a fresh
			// connection that is closed immediately after the response is read.
			// Without this, the transport accumulates idle connections across
			// rapid polling attempts (every readinessPollInterval), especially when early attempts
			// fail due to the server not yet listening.
			DisableKeepAlives: true,
		},
		Timeout: healthCheckTimeout,
	}
	defer httpClient.CloseIdleConnections()

	// Use /livez instead of /readyz to avoid waiting for slow post-start hooks
	// /livez only checks if the server process is alive, not if all controllers are ready
	// This significantly reduces startup time from ~10s to ~3-4s
	healthURL := fmt.Sprintf("https://127.0.0.1:%d/livez", p.config.Port)

	log := p.base.Logger()
	if err := process.WaitReady(ctx, process.WaitReadyConfig{
		Interval:      readinessPollInterval,
		Timeout:       timeout,
		Name:          "apiserver",
		Port:          p.config.Port,
		Logger:        log,
		ProcessExited: p.base.Exited(),
	}, func(checkCtx context.Context, attempt int) (bool, error) {
		req, err := http.NewRequestWithContext(checkCtx, http.MethodGet, healthURL, http.NoBody)
		if err != nil {
			return false, fmt.Errorf("create health check request: %w", err)
		}
		resp, err := httpClient.Do(req)
		if err != nil {
			if log.Enabled(checkCtx, slog.LevelDebug) {
				log.Debug("waitForAPIServer attempt", "port", p.config.Port, "attempt", attempt, "error", err)
			}
			return false, nil
		}
		// Drain and close the response body so the underlying connection
		// is properly released back to the transport.
		defer func() {
			_, _ = io.Copy(io.Discard, resp.Body) // best-effort drain
			_ = resp.Body.Close()
		}()

		if resp.StatusCode == http.StatusOK {
			return true, nil
		}
		if log.Enabled(checkCtx, slog.LevelDebug) {
			log.Debug("waitForAPIServer attempt", "port", p.config.Port, "attempt", attempt, "status", resp.StatusCode)
		}
		return false, nil
	}); err != nil {
		return fmt.Errorf("apiserver not ready: %w", err)
	}
	return nil
}

// WriteKubeconfig generates a kubeconfig file for connecting to this API server.
func (p *Process) WriteKubeconfig() error {
	apiServerURL := fmt.Sprintf("https://127.0.0.1:%d", p.config.Port)

	config := clientcmdapi.Config{
		Clusters: map[string]*clientcmdapi.Cluster{
			"default": {
				Server: apiServerURL,
				// InsecureSkipTLSVerify is safe here because:
				// 1. This is a testing framework - not production code
				// 2. The API server uses ephemeral self-signed certificates generated at startup
				// 3. We only connect to localhost (127.0.0.1) - no network exposure
				// 4. The certificates are not known ahead of time and have no CA to verify against
				InsecureSkipTLSVerify: true,
			},
		},
		Contexts: map[string]*clientcmdapi.Context{
			"default": {
				Cluster:  "default",
				AuthInfo: "default",
			},
		},
		CurrentContext: "default",
		AuthInfos: map[string]*clientcmdapi.AuthInfo{
			"default": {
				Token: testAuthToken,
			},
		},
	}

	if err := clientcmd.WriteToFile(config, p.config.KubeconfigPath); err != nil {
		return fmt.Errorf("write kubeconfig to %s: %w", p.config.KubeconfigPath, err)
	}
	return nil
}

// Stop terminates the kube-apiserver process with the given timeout.
func (p *Process) Stop(timeout time.Duration) error {
	return p.base.Stop(timeout)
}

// Close releases log file handles held by the process.
func (p *Process) Close() {
	p.base.Close()
}
