package crdcache

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
)

// TestApplyMissingKindErrSubstring verifies that the upstream
// runtime.missingKindErr error message still contains the substring
// used by isMissingKindDecodeError for its string-based fallback.
//
// The upstream missingKindErr type is unexported and its Error() method
// produces: "Object 'Kind' is missing in '<data>'". We match against the
// prefix "Object 'Kind' is missing" because the suffix varies by input.
//
// If this test fails, the upstream error message format has changed and
// missingKindErrSubstring must be updated.
func TestApplyMissingKindErrSubstring(t *testing.T) {
	t.Parallel()
	err := runtime.NewMissingKindErr("test-data")

	if !strings.Contains(err.Error(), missingKindErrSubstring) {
		t.Fatalf(
			"upstream missingKindErr message %q no longer contains expected substring %q; update missingKindErrSubstring",
			err.Error(),
			missingKindErrSubstring,
		)
	}
}

// TestApplyIsMissingKindDecodeError exercises the isMissingKindDecodeError
// function against error types that arise from the upstream Kubernetes
// YAML/JSON decode pipeline.
//
// String-based detection context: the upstream k8s libraries wrap
// missingKindErr through multiple paths without implementing Unwrap:
//   - YAML path: YAMLSyntaxError{err: missingKindErr} (no Unwrap)
//   - JSON path: fmt.Errorf("...%s", missingKindErr) (no %w verb)
//
// This makes runtime.IsMissingKind fail on wrapped errors, requiring a
// fallback to string matching on missingKindErrSubstring.
func TestApplyIsMissingKindDecodeError(t *testing.T) {
	t.Parallel()
	// Note: isMissingKindDecodeError does not handle nil errors.
	// The caller (applyDocument) only invokes it when err != nil.
	tests := map[string]struct {
		err  error
		want bool
	}{
		"direct missingKindErr via NewMissingKindErr": {
			err:  runtime.NewMissingKindErr("test-data"),
			want: true,
		},
		"wrapped without Unwrap (simulates YAMLSyntaxError)": {
			// YAMLSyntaxError embeds the error in a struct field
			// and delegates Error() to the inner error, but does
			// not implement Unwrap. This means runtime.IsMissingKind
			// cannot reach the inner *missingKindErr via type assertion.
			err:  noUnwrapError{inner: runtime.NewMissingKindErr("yaml-input")},
			want: true,
		},
		"wrapped via fmt.Errorf without %w (simulates JSON path)": {
			// The JSON decode path wraps missingKindErr using string
			// formatting (not %w), embedding the message but losing
			// the type information entirely.
			//nolint:errorlint // intentionally using %s to simulate upstream behavior that loses type info
			err:  fmt.Errorf("error unmarshaling JSON: %s", runtime.NewMissingKindErr("json-input")),
			want: true,
		},
		"wrapped via fmt.Errorf with %w (preserves type)": {
			// When properly wrapped with %w, runtime.IsMissingKind
			// can unwrap and find the type. This path currently works
			// via the typed check, not the string fallback.
			err:  fmt.Errorf("decode error: %w", runtime.NewMissingKindErr("wrapped")),
			want: true,
		},
		"unrelated error": {
			err:  errors.New("connection refused"),
			want: false,
		},
		"error containing partial substring": {
			// An error that contains "Kind" but not the full substring
			// should not match.
			err:  errors.New("missing Kind field in resource"),
			want: false,
		},
		"error containing exact substring in different context": {
			// An error from a different source that happens to contain
			// the same substring should still be detected. This is an
			// accepted trade-off of string-based detection.
			err:  fmt.Errorf("validation failed: %s", missingKindErrSubstring),
			want: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := isMissingKindDecodeError(tc.err)
			if got != tc.want {
				t.Errorf("isMissingKindDecodeError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// TestApplyMissingKindErrSubstringValue documents the exact substring being
// matched. If this test fails, someone changed the constant without updating
// the tests.
func TestApplyMissingKindErrSubstringValue(t *testing.T) {
	t.Parallel()
	const expected = "Object 'Kind' is missing"
	if missingKindErrSubstring != expected {
		t.Errorf("missingKindErrSubstring = %q, want %q", missingKindErrSubstring, expected)
	}
}

// TestApplyRuntimeIsMissingKindTypedCheck verifies that runtime.IsMissingKind
// works for direct missingKindErr instances but fails for wrapped instances
// without Unwrap. This documents why the string-based fallback exists.
func TestApplyRuntimeIsMissingKindTypedCheck(t *testing.T) {
	t.Parallel()
	t.Run("direct error passes typed check", func(t *testing.T) {
		t.Parallel()
		err := runtime.NewMissingKindErr("test")
		if !runtime.IsMissingKind(err) {
			t.Fatal("runtime.IsMissingKind should return true for direct missingKindErr")
		}
	})

	t.Run("wrapped without Unwrap fails typed check", func(t *testing.T) {
		t.Parallel()
		inner := runtime.NewMissingKindErr("test")
		wrapped := noUnwrapError{inner: inner}

		if runtime.IsMissingKind(wrapped) {
			t.Fatal(
				"runtime.IsMissingKind should return false for wrapped error without Unwrap; if this starts passing, the string-based fallback may no longer be needed",
			)
		}
	})

	t.Run("our function catches what runtime.IsMissingKind misses", func(t *testing.T) {
		t.Parallel()
		inner := runtime.NewMissingKindErr("test")
		wrapped := noUnwrapError{inner: inner}

		if runtime.IsMissingKind(wrapped) {
			t.Skip("runtime.IsMissingKind now handles this case; string fallback may be removable")
		}
		if !isMissingKindDecodeError(wrapped) {
			t.Fatal("isMissingKindDecodeError should catch wrapped missingKindErr via string fallback")
		}
	})
}

// noUnwrapError simulates upstream wrappers like YAMLSyntaxError that embed
// an inner error and delegate Error() but do not implement Unwrap().
type noUnwrapError struct {
	inner error
}

func (e noUnwrapError) Error() string {
	return e.inner.Error()
}
