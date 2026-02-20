package k8senv_test

import (
	"reflect"
	"testing"

	"github.com/giantswarm/k8senv"
)

// TestReleaseStrategyMethodCount is a canary test that detects when methods are
// added to core.ReleaseStrategy, which automatically expands the public API
// through the type alias in strategy.go.
//
// ReleaseStrategy intentionally exposes exactly two methods via the alias:
//   - IsValid() bool  — reports whether the value is a recognized strategy
//   - String() string — returns the strategy name (implements fmt.Stringer)
//
// If this test fails, a method was added to core.ReleaseStrategy. You must
// either:
//  1. Decide the new method is intentionally public and update expectedMethods
//     below to match the new count, or
//  2. Reconsider whether the method should be on core.ReleaseStrategy at all,
//     given that any method added there automatically becomes public API.
func TestReleaseStrategyMethodCount(t *testing.T) {
	t.Parallel()

	// ReleaseStrategy currently exposes exactly two methods: IsValid and String.
	// Update this constant when a method is intentionally added.
	const expectedMethods = 2

	actual := reflect.TypeFor[k8senv.ReleaseStrategy]().NumMethod()
	if actual != expectedMethods {
		t.Errorf("ReleaseStrategy has %d methods, expected %d; "+
			"methods added to core.ReleaseStrategy automatically become "+
			"public API through the type alias in strategy.go — "+
			"update expectedMethods in this test if the addition is intentional",
			actual, expectedMethods)
	}
}

// TestReleaseStrategyMethodNames verifies that the two expected methods exist
// on ReleaseStrategy with their exact names. This catches renames in addition
// to additions; the compile-time check in strategy.go catches removals.
func TestReleaseStrategyMethodNames(t *testing.T) {
	t.Parallel()

	want := map[string]bool{
		"IsValid": true,
		"String":  true,
	}

	typ := reflect.TypeFor[k8senv.ReleaseStrategy]()
	for i := range typ.NumMethod() {
		name := typ.Method(i).Name
		if !want[name] {
			t.Errorf("unexpected method %q on ReleaseStrategy; "+
				"new methods on core.ReleaseStrategy automatically become "+
				"public API through the type alias in strategy.go",
				name)
		}
		delete(want, name)
	}

	for name := range want {
		t.Errorf("expected method %q not found on ReleaseStrategy", name)
	}
}
