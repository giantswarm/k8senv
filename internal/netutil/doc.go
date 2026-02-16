// Package netutil provides network utility functions for k8senv.
// Its central type, PortRegistry, allocates pairs of ephemeral ports by binding
// both listeners simultaneously to guarantee distinctness, and tracks reserved
// ports across the process to prevent duplicate allocation from the TOCTOU race
// between concurrent callers.
package netutil
