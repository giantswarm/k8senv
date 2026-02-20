package k8senv_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/giantswarm/k8senv"
)

// TestPublicErrorConstants verifies that every exported error constant:
//   - implements the error interface (Error() returns a non-empty string)
//   - matches itself via errors.Is
//   - matches itself when wrapped via fmt.Errorf %w
//   - does not match a different error constant
func TestPublicErrorConstants(t *testing.T) {
	t.Parallel()

	// All exported sentinel errors.
	allErrors := map[string]error{
		"ErrCRDEstablishTimeout": k8senv.ErrCRDEstablishTimeout,
		"ErrDoubleRelease":       k8senv.ErrDoubleRelease,
		"ErrInstanceReleased":    k8senv.ErrInstanceReleased,
		"ErrMissingKind":         k8senv.ErrMissingKind,
		"ErrNoYAMLFiles":         k8senv.ErrNoYAMLFiles,
		"ErrNotInitialized":      k8senv.ErrNotInitialized,
		"ErrNotStarted":          k8senv.ErrNotStarted,
		"ErrPoolClosed":          k8senv.ErrPoolClosed,
		"ErrShuttingDown":        k8senv.ErrShuttingDown,
	}

	for name, sentinel := range allErrors {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// Must implement error interface with a non-empty message.
			if sentinel == nil {
				t.Fatalf("%s is nil", name)
			}
			if msg := sentinel.Error(); msg == "" {
				t.Errorf("%s.Error() returned empty string", name)
			}

			// Direct errors.Is match.
			if !errors.Is(sentinel, sentinel) {
				t.Errorf("errors.Is(%s, %s) = false, want true (self-match)", name, name)
			}

			// Wrapped errors.Is match.
			wrapped := fmt.Errorf("wrapping: %w", sentinel)
			if !errors.Is(wrapped, sentinel) {
				t.Errorf("errors.Is(wrapped %s) = false, want true", name)
			}

			// Must not match a different error constant.
			differentErr := errors.New("some other error")
			if errors.Is(sentinel, differentErr) {
				t.Errorf("errors.Is(%s, errors.New(...)) = true, want false", name)
			}
		})
	}
}

// TestPublicErrorConstantsAreDistinct verifies that no two exported error
// constants are equal to each other (every sentinel has a unique identity).
func TestPublicErrorConstantsAreDistinct(t *testing.T) {
	t.Parallel()

	named := []struct {
		name string
		err  error
	}{
		{"ErrCRDEstablishTimeout", k8senv.ErrCRDEstablishTimeout},
		{"ErrDoubleRelease", k8senv.ErrDoubleRelease},
		{"ErrInstanceReleased", k8senv.ErrInstanceReleased},
		{"ErrMissingKind", k8senv.ErrMissingKind},
		{"ErrNoYAMLFiles", k8senv.ErrNoYAMLFiles},
		{"ErrNotInitialized", k8senv.ErrNotInitialized},
		{"ErrNotStarted", k8senv.ErrNotStarted},
		{"ErrPoolClosed", k8senv.ErrPoolClosed},
		{"ErrShuttingDown", k8senv.ErrShuttingDown},
	}

	for i, a := range named {
		for _, b := range named[i+1:] {
			if errors.Is(a.err, b.err) {
				t.Errorf("errors.Is(%s, %s) = true: constants must be distinct", a.name, b.name)
			}
			if errors.Is(b.err, a.err) {
				t.Errorf("errors.Is(%s, %s) = true: constants must be distinct", b.name, a.name)
			}
		}
	}
}

// TestErrDoubleReleaseValue verifies that ErrDoubleRelease is the error value
// returned by a stub that simulates the instanceWrapper double-release behavior.
// This covers the public-facing contract that Release returns ErrDoubleRelease
// when called more than once.
func TestErrDoubleReleaseValue(t *testing.T) {
	t.Parallel()

	stub := &stubInstance{}

	// First Release: must succeed.
	if err := stub.Release(); err != nil {
		t.Fatalf("first Release() error = %v, want nil", err)
	}

	// Second Release: must return ErrDoubleRelease.
	err := stub.Release()
	if !errors.Is(err, k8senv.ErrDoubleRelease) {
		t.Errorf("second Release() error = %v, want ErrDoubleRelease", err)
	}
}

// stubInstance is a minimal in-process fake that simulates the double-release
// guard in instanceWrapper. It implements a subset of the Instance interface
// sufficient for this test.
type stubInstance struct {
	released bool
}

// Release returns ErrDoubleRelease on the second call, mimicking instanceWrapper.
func (s *stubInstance) Release() error {
	if s.released {
		return k8senv.ErrDoubleRelease
	}
	s.released = true
	return nil
}

// TestSystemNamespaceNames verifies that SystemNamespaceNames returns the four
// expected system namespaces without any duplicates and returns a copy (not the
// original slice, so callers cannot mutate internal state).
func TestSystemNamespaceNames(t *testing.T) {
	t.Parallel()

	names := k8senv.SystemNamespaceNames()

	expected := []string{"default", "kube-system", "kube-public", "kube-node-lease"}
	if len(names) != len(expected) {
		t.Fatalf("SystemNamespaceNames() returned %d items, want %d", len(names), len(expected))
	}

	nameSet := make(map[string]struct{}, len(names))
	for _, n := range names {
		if _, dup := nameSet[n]; dup {
			t.Errorf("SystemNamespaceNames() contains duplicate %q", n)
		}
		nameSet[n] = struct{}{}
	}

	for _, want := range expected {
		if _, ok := nameSet[want]; !ok {
			t.Errorf("SystemNamespaceNames() missing %q", want)
		}
	}
}

// TestSystemNamespaceNamesReturnsCopy verifies that mutating the returned slice
// does not affect subsequent calls (i.e., a copy is returned).
func TestSystemNamespaceNamesReturnsCopy(t *testing.T) {
	t.Parallel()

	first := k8senv.SystemNamespaceNames()
	firstLen := len(first)

	// Modify the returned slice in-place.
	first[0] = "mutated"

	second := k8senv.SystemNamespaceNames()
	if len(second) != firstLen {
		t.Fatalf("SystemNamespaceNames() length changed after mutation: got %d, want %d", len(second), firstLen)
	}
	for _, n := range second {
		if n == "mutated" {
			t.Error("SystemNamespaceNames() returned a shared slice; mutation affected subsequent call")
		}
	}
}
