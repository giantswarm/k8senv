// Package process provides utilities for managing external process lifecycle.
//
// It defines BaseProcess for common process start/stop behavior, the Stoppable
// interface, StopCloseAndNil for atomic cleanup, and WaitReady for polling-based
// readiness checks.
package process
