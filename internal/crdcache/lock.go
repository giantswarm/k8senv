package crdcache

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/gofrs/flock"
)

// fileLockRetryInterval is the interval between consecutive attempts to
// acquire the CRD cache file lock. 50ms balances responsiveness (low wait
// after the holder releases) against CPU overhead from busy-polling.
const fileLockRetryInterval = 50 * time.Millisecond

// acquireFileLock acquires an exclusive lock on the given file path.
// It respects context cancellation and returns early if the context is canceled.
// Lock acquisition is retried at fileLockRetryInterval until successful or context is done.
func acquireFileLock(ctx context.Context, lockPath string) (*flock.Flock, error) {
	fl := flock.New(lockPath)

	locked, err := fl.TryLockContext(ctx, fileLockRetryInterval)
	if err != nil {
		return nil, fmt.Errorf("acquiring file lock %s: %w", lockPath, err)
	}

	if !locked {
		// Defensive: TryLockContext should return an error when it fails,
		// but handle the case where it returns (false, nil) unexpectedly.
		if ctx.Err() != nil {
			return nil, fmt.Errorf("acquiring file lock %s: %w", lockPath, ctx.Err())
		}

		return nil, fmt.Errorf("acquiring file lock %s: lock not acquired", lockPath)
	}

	return fl, nil
}

// releaseFileLock releases the file lock and closes the file descriptor.
// The lock file is intentionally left on disk to avoid a race where removing
// it could invalidate a lock concurrently acquired by another process.
// Close() calls Unlock() internally, so no explicit Unlock is needed.
// Errors are logged at debug level to aid troubleshooting; this is
// best-effort cleanup so errors are not returned.
func releaseFileLock(logger *slog.Logger, fl *flock.Flock) {
	if fl != nil {
		if err := fl.Close(); err != nil {
			logger.Debug("failed to release file lock", "path", fl.Path(), "err", err)
		}
	}
}
