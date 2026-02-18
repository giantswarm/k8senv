# Troubleshooting

This guide covers common issues and debugging techniques for k8senv.

## Tests Failing

### Binary Not Found

**Symptom**: Tests exit with error about missing binaries.

**Solution**: Install the required binaries:

```bash
# Install kine
go install github.com/k3s-io/kine/cmd/kine@latest

# Install kube-apiserver (Linux amd64)
curl -LO https://dl.k8s.io/v1.35.0/bin/linux/amd64/kube-apiserver
chmod +x kube-apiserver
sudo mv kube-apiserver /usr/local/bin/
```

Verify installation:
```bash
which kine
which kube-apiserver
kine --version
kube-apiserver --version
```

### Binary Not Working

**Symptom**: Binary exists but `--version` fails.

**Solution**: Check binary permissions and architecture:

```bash
file $(which kine)
file $(which kube-apiserver)
```

Ensure the binary matches your system architecture (amd64 vs arm64).

## Acquisition Timeout

### Slow Startup

**Symptom**: First test times out, subsequent tests pass.

**Cause**: Instance startup taking longer than expected.

**Solution**:

1. Check system resources (CPU, memory, disk I/O)
2. Increase timeout for first acquisition
3. Check logs for startup errors

## Port Conflicts

### Address Already in Use

**Symptom**: Instance fails to start with "address already in use" error.

**Cause**: Another process is using the dynamically allocated port.

**Solution**:

1. k8senv automatically retries with new ports (5 attempts by default)

2. Check for orphaned processes:
   ```bash
   pgrep -f kine
   pgrep -f kube-apiserver
   ```

3. Kill orphaned processes if needed:
   ```bash
   pkill -f "kine.*k8senv"
   pkill -f "kube-apiserver.*k8senv"
   ```

## Log File Locations

Instance logs are stored in the data directory:

```
/tmp/k8senv/inst-<id>/
├── kine.db                    # SQLite database
├── kine-stdout.log            # kine standard output
├── kine-stderr.log            # kine standard error
├── kube-apiserver-stdout.log  # kube-apiserver standard output
├── kube-apiserver-stderr.log  # kube-apiserver standard error
├── apiserver.crt              # Generated certificate
├── apiserver.key              # Generated private key
└── token-auth                 # Token authentication file
```

### Viewing Logs

```bash
# List instance directories
ls -la /tmp/k8senv/

# View kube-apiserver errors
cat /tmp/k8senv/inst-*/kube-apiserver-stderr.log

# View kine errors
cat /tmp/k8senv/inst-*/kine-stderr.log

# Follow logs in real-time
tail -f /tmp/k8senv/inst-*/kube-apiserver-stderr.log
```

### Common Log Errors

**kine errors**:
- `database is locked`: Another process has the SQLite database open
- `permission denied`: Check directory permissions

**kube-apiserver errors**:
- `connection refused`: kine didn't start or isn't ready
- `certificate is not valid`: Certificate generation failed

## CRD Cache Issues

### Cache Not Created

**Symptom**: CRDs not available despite `WithCRDDir` being set.

**Causes**:
1. YAML files have syntax errors
2. CRD validation fails
3. Timeout during cache creation

**Solution**:

1. Validate YAML files:
   ```bash
   kubectl apply --dry-run=client -f testdata/crds/
   ```

2. Increase timeout for cache creation:
   ```go
   k8senv.WithAcquireTimeout(3*time.Minute)
   ```

3. Check logs during initialization

### Stale Cache

**Symptom**: Old CRD definitions appear despite file changes.

**Cause**: Hash collision (very rare) or file system caching.

**Solution**:

1. Clear all caches:
   ```bash
   rm /tmp/k8senv/cached-*.db
   ```

2. Ensure files are saved (not just in editor buffer)

### Cache Directory

View existing caches:

```bash
ls -la /tmp/k8senv/cached-*.db
```

## Debug Logging

### Enable Debug Output

Set the log level for tests:

```bash
K8SENV_LOG_LEVEL=DEBUG go test -tags=integration -v ./...
```

### Custom Logger

For more control, set a custom logger:

```go
import (
    "log/slog"
    "os"
    "github.com/giantswarm/k8senv"
)

func init() {
    handler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
        Level: slog.LevelDebug,
    })
    k8senv.SetLogger(slog.New(handler))
}
```

### Verbose Test Output

```bash
go test -tags=integration -v -count=1 ./...
```

The `-count=1` flag disables test caching for fresh output.

## Process Management

### Checking Running Processes

```bash
# List k8senv-related processes
ps aux | grep -E "(kine|kube-apiserver)" | grep k8senv

# Count instances
pgrep -c -f "kine.*k8senv"
pgrep -c -f "kube-apiserver.*k8senv"
```

### Cleaning Up After Crashes

If tests crash without proper cleanup:

```bash
# Kill all k8senv processes
pkill -f "kine.*k8senv"
pkill -f "kube-apiserver.*k8senv"

# Remove data directories
rm -rf /tmp/k8senv/inst-*

# Keep caches (optional - remove if issues persist)
# rm /tmp/k8senv/cached-*.db
```

### CI Environment Isolation

Use a unique base directory in CI to prevent conflicts between parallel jobs:

```go
k8senv.WithBaseDataDir(fmt.Sprintf("/tmp/k8senv-%s", os.Getenv("CI_JOB_ID")))
```

## Common Patterns

### Graceful Skip in Local Development

```go
func TestMain(m *testing.M) {
    ctx := context.Background()

    mgr := k8senv.NewManager()
    if err := mgr.Initialize(ctx); err != nil {
        // In CI, fail hard; locally, allow graceful skip
        if os.Getenv("CI") != "" {
            panic(fmt.Sprintf("Failed to initialize k8senv: %v", err))
        }
        fmt.Printf("Skipping integration tests: %v\n", err)
        os.Exit(0)
    }

    // ...
}
```

### Checking Instance Health

```go
inst, err := mgr.Acquire(ctx)
if err != nil {
    t.Fatal(err)
}

// Instances are always started after a successful Acquire.
// Verify by getting the config and making an API call:
cfg, err := inst.Config()
if err != nil {
    t.Fatalf("Instance not usable: %v", err)
}

client, err := kubernetes.NewForConfig(cfg)
if err != nil {
    t.Fatal(err)
}

_, err = client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
if err != nil {
    t.Fatalf("API server not responding: %v", err)
}
```

## Getting Help

If you encounter issues not covered here:

1. Check the [GitHub issues](https://github.com/giantswarm/k8senv/issues)
2. Review instance logs in `/tmp/k8senv/inst-*/`
3. Enable debug logging and capture output
4. Open a new issue with:
   - Go version
   - kine version
   - kube-apiserver version
   - Relevant log output
   - Minimal reproduction case

## Related

- [Configuration](configuration.md) - All options with defaults and examples
- [Getting Started](../tutorials/getting-started.md) - Installation and prerequisites
- [Data Flow](data-flow.md) - Understanding the request lifecycle
