package k8senv

// ResetForTesting resets the singleton manager state so that the next
// call to NewManager creates a fresh instance. This is exported only
// for use in test packages (package k8senv_test).
func ResetForTesting() { resetForTesting() }
