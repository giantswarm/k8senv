package k8senv

import (
	"log/slog"

	"github.com/giantswarm/k8senv/internal/core"
)

// SetLogger replaces the package-level logger used by k8senv.
// This allows applications to integrate k8senv logging with their own
// logging infrastructure. The provided logger should already have any
// desired attributes; k8senv will not add additional attributes.
//
// If l is nil, the logger resets to the default: slog.Default() with
// "component" attribute, re-derived on the next Logger() call and then
// cached. Call SetLogger(nil) after slog.SetDefault() to pick up changes.
//
// SetLogger is safe to call concurrently with other k8senv operations.
//
// Example:
//
//	k8senv.SetLogger(myLogger.With("component", "k8senv"))
func SetLogger(l *slog.Logger) {
	core.SetLogger(l)
}
