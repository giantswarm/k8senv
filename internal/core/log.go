package core

import (
	"log/slog"
	"sync/atomic"
)

// logger is the package-level logger used by k8senv, stored as an atomic pointer
// to allow safe concurrent reads and writes. Named "logger" instead of "log" to
// avoid shadowing the stdlib "log" package.
//
// A nil value means no custom logger has been set; Logger() will fall back to
// a cached default derived from slog.Default().
var logger atomic.Pointer[slog.Logger]

// defaultLogger caches the default-derived logger (slog.Default() with the
// k8senv component attribute) so it is not re-created on every Logger() call.
// This eliminates per-call allocations and reduces GC pressure. The tradeoff
// is that if slog.SetDefault() is called after the first Logger() call, the
// cached logger will not reflect the change. This is acceptable for a testing
// framework. Calling SetLogger(nil) clears this cache, allowing the next
// Logger() call to pick up a new slog.Default().
var defaultLogger atomic.Pointer[slog.Logger]

// Logger returns the current package-level logger. If no custom logger has been
// set via SetLogger, it returns a cached logger derived from slog.Default()
// with the k8senv component attribute. The default logger is created once and
// cached for subsequent calls. It is safe to call from multiple goroutines.
func Logger() *slog.Logger {
	if l := logger.Load(); l != nil {
		return l
	}
	if l := defaultLogger.Load(); l != nil {
		return l
	}
	l := newDefaultLogger()
	// Use CompareAndSwap to avoid overwriting a concurrently cached value.
	// If another goroutine already stored a logger, use theirs.
	if defaultLogger.CompareAndSwap(nil, l) {
		return l
	}
	// Re-load the winner's value. If it's nil (e.g., concurrent SetLogger
	// cleared it between our CAS and this load), fall back to our locally
	// created logger so we never return nil.
	if l2 := defaultLogger.Load(); l2 != nil {
		return l2
	}
	return l
}

// newDefaultLogger creates the default logger with the k8senv component attribute.
func newDefaultLogger() *slog.Logger {
	return slog.Default().With("component", "k8senv")
}

// SetLogger replaces the package-level logger used by k8senv.
// If l is nil, the logger resets to the default: slog.Default() with
// "component" attribute, re-derived on the next Logger() call and then cached.
//
// SetLogger is safe to call concurrently with other k8senv operations.
func SetLogger(l *slog.Logger) {
	logger.Store(l)
	// Clear the cached default so the next Logger() call re-derives it from
	// slog.Default(). This allows callers to pick up slog.SetDefault() changes
	// by calling SetLogger(nil).
	defaultLogger.Store(nil)
}
