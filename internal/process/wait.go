package process

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
)

// Sentinel errors returned by WaitReady for invalid configuration and
// process lifecycle conditions. Callers can match these with errors.Is
// through wrapped error chains.
var (
	// ErrIntervalNotPositive indicates a non-positive poll interval.
	ErrIntervalNotPositive = errors.New("interval must be positive")

	// ErrTimeoutNotPositive indicates a non-positive timeout.
	ErrTimeoutNotPositive = errors.New("timeout must be positive")

	// ErrProcessExited indicates the process exited before becoming ready.
	ErrProcessExited = errors.New("process exited before becoming ready")
)

// ReadinessCheck is a function that checks if a process is ready.
// The context is canceled when the polling loop times out or the caller
// cancels, allowing checks (e.g., HTTP requests) to exit promptly.
// The attempt parameter is 1-based (first call receives attempt=1).
// It returns true when ready, false to continue polling.
// The error return is for fatal errors that should abort polling.
type ReadinessCheck func(ctx context.Context, attempt int) (ready bool, err error)

// WaitReadyConfig configures the wait behavior.
type WaitReadyConfig struct {
	Interval      time.Duration   // Poll interval
	Timeout       time.Duration   // Overall timeout
	Name          string          // For logging (e.g., "kine", "apiserver")
	Port          int             // For logging context
	Logger        *slog.Logger    // Optional logger (defaults to slog.Default())
	ProcessExited <-chan struct{} // If non-nil, abort immediately when closed (process died)
}

// WaitReady polls until the check function returns true or timeout.
// The check function is called repeatedly until it returns true (ready)
// or returns a non-nil error (fatal, abort polling).
func WaitReady(ctx context.Context, cfg WaitReadyConfig, check ReadinessCheck) error {
	if cfg.Name == "" {
		return errors.New("wait ready: name must not be empty")
	}
	if cfg.Interval <= 0 {
		return fmt.Errorf("wait for %s: %w", cfg.Name, ErrIntervalNotPositive)
	}
	if cfg.Timeout <= 0 {
		return fmt.Errorf("wait for %s: %w", cfg.Name, ErrTimeoutNotPositive)
	}

	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}

	// attempt is safe to increment without synchronization because
	// PollUntilContextTimeout invokes the condition function sequentially:
	// each call completes before the next is scheduled. The closure is
	// never called concurrently with itself.
	attempt := 0
	if err := wait.PollUntilContextTimeout(ctx, cfg.Interval, cfg.Timeout, true,
		func(pollCtx context.Context) (bool, error) {
			// Check if the process has exited before attempting the
			// readiness check. This avoids polling for the full timeout
			// when a process dies immediately (e.g., port bind failure).
			if cfg.ProcessExited != nil {
				select {
				case <-cfg.ProcessExited:
					return false, fmt.Errorf("process %s: %w", cfg.Name, ErrProcessExited)
				default:
				}
			}

			attempt++
			ready, err := check(pollCtx, attempt)
			if err != nil {
				// Fatal error - abort polling
				return false, err
			}
			if ready {
				log.Debug("wait succeeded", "name", cfg.Name, "port", cfg.Port, "attempt", attempt)
			}
			return ready, nil
		}); err != nil {
		return fmt.Errorf("wait for %s readiness on port %d: %w", cfg.Name, cfg.Port, err)
	}
	return nil
}
