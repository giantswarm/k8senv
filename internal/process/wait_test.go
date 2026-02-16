package process

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestWaitReady_ZeroInterval(t *testing.T) {
	err := WaitReady(context.Background(), WaitReadyConfig{
		Interval: 0,
		Timeout:  5 * time.Second,
		Name:     "test-proc",
		Port:     12345,
	}, func(_ context.Context, _ int) (bool, error) {
		t.Fatal("check should not be called with zero interval")
		return false, nil
	})
	if err == nil {
		t.Fatal("expected error for zero interval, got nil")
	}
	if !strings.Contains(err.Error(), "interval must be positive") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestWaitReady_NegativeInterval(t *testing.T) {
	err := WaitReady(context.Background(), WaitReadyConfig{
		Interval: -1 * time.Second,
		Timeout:  5 * time.Second,
		Name:     "test-proc",
		Port:     12345,
	}, func(_ context.Context, _ int) (bool, error) {
		t.Fatal("check should not be called with negative interval")
		return false, nil
	})
	if err == nil {
		t.Fatal("expected error for negative interval, got nil")
	}
	if !strings.Contains(err.Error(), "interval must be positive") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestWaitReady_ZeroTimeout(t *testing.T) {
	err := WaitReady(context.Background(), WaitReadyConfig{
		Interval: 100 * time.Millisecond,
		Timeout:  0,
		Name:     "test-proc",
		Port:     12345,
	}, func(_ context.Context, _ int) (bool, error) {
		t.Fatal("check should not be called with zero timeout")
		return false, nil
	})
	if err == nil {
		t.Fatal("expected error for zero timeout, got nil")
	}
	if !strings.Contains(err.Error(), "timeout must be positive") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestWaitReady_NegativeTimeout(t *testing.T) {
	err := WaitReady(context.Background(), WaitReadyConfig{
		Interval: 100 * time.Millisecond,
		Timeout:  -1 * time.Second,
		Name:     "test-proc",
		Port:     12345,
	}, func(_ context.Context, _ int) (bool, error) {
		t.Fatal("check should not be called with negative timeout")
		return false, nil
	})
	if err == nil {
		t.Fatal("expected error for negative timeout, got nil")
	}
	if !strings.Contains(err.Error(), "timeout must be positive") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestWaitReady_ProcessExited(t *testing.T) {
	t.Parallel()

	// Pre-close the channel to simulate a process that has already exited.
	exited := make(chan struct{})
	close(exited)

	start := time.Now()
	err := WaitReady(context.Background(), WaitReadyConfig{
		Interval:      100 * time.Millisecond,
		Timeout:       10 * time.Second,
		Name:          "test-proc",
		Port:          12345,
		ProcessExited: exited,
	}, func(_ context.Context, _ int) (bool, error) {
		// Should never be called because the exited check fires first.
		t.Fatal("readiness check should not have been called")
		return false, nil
	})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "process test-proc exited before becoming ready") {
		t.Fatalf("unexpected error message: %v", err)
	}
	// The function should return almost immediately, well under 1 second.
	if elapsed > time.Second {
		t.Fatalf("expected fast abort, took %v", elapsed)
	}
}

func TestWaitReady_NilProcessExited(t *testing.T) {
	t.Parallel()

	// When ProcessExited is nil, WaitReady should behave normally
	// (backwards compatible).
	err := WaitReady(context.Background(), WaitReadyConfig{
		Interval: 10 * time.Millisecond,
		Timeout:  5 * time.Second,
		Name:     "test-proc",
		Port:     12345,
	}, func(_ context.Context, _ int) (bool, error) {
		// Succeed on first attempt.
		return true, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
