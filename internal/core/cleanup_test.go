package core

import (
	"testing"
)

// TestSystemNamespacesCount is a canary test that detects when entries are
// added to or removed from the systemNamespaces array without updating
// dependent code (isSystemNamespace, waitForSystemNamespaces, etc.).
//
// If this test fails, you changed the systemNamespaces array. You must also:
//  1. Update the hard-coded count in waitForSystemNamespaces's log message
//     ("all 4 system namespaces") if the total changes
//  2. Update expectedCount below to match the new count
func TestSystemNamespacesCount(t *testing.T) {
	t.Parallel()
	const want = 4 // update this if you add/remove system namespaces
	if got := len(systemNamespaces); got != want {
		t.Errorf("systemNamespaces count = %d, want %d; update this test and dependent code", got, want)
	}
}
