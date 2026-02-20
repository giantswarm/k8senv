package k8senv_test

import (
	"testing"

	"github.com/giantswarm/k8senv"
)

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
