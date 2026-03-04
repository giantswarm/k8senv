package netutil

import (
	"fmt"
	"log/slog"
	"net"
	"sync"
)

// maxPortRetries is the maximum number of attempts to find a port not already
// in the registry. This guards against pathological cases.
const maxPortRetries = 20

// loopbackAddr is reused across all port allocation calls.
// net.ListenTCP does not mutate the address, so this is safe for concurrent use.
var loopbackAddr = &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)}

// PortRegistry tracks ports currently reserved by this process to prevent
// the TOCTOU race where two concurrent AllocatePortPair calls receive the
// same port from the kernel (because the first caller closed its listener
// before the second caller opened theirs).
//
// The singleton Manager creates one PortRegistry and shares it via dependency
// injection with all instances and temporary stacks (e.g., CRD cache creation).
type PortRegistry struct {
	mu    sync.Mutex
	ports map[int]struct{}
}

// NewPortRegistry creates a new PortRegistry ready for use.
func NewPortRegistry() *PortRegistry {
	return &PortRegistry{
		ports: make(map[int]struct{}),
	}
}

// reserve attempts to register a port in the registry.
// Returns true if the port was successfully reserved, false if already taken.
func (r *PortRegistry) reserve(port int) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.ports[port]; ok {
		return false
	}
	r.ports[port] = struct{}{}
	return true
}

// Release removes a port from the registry, allowing it to be reused.
// It logs a warning if the port was not previously reserved.
func (r *PortRegistry) Release(port int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.ports[port]; !ok {
		slog.Default().Warn("releasing port that was not reserved", "port", port)
		return
	}
	delete(r.ports, port)
}

// getFreePortFromKernel asks the kernel for a free port, skipping any ports
// already in the registry. On success it returns the assigned port number.
// The port is registered in the registry; the caller must call
// [PortRegistry.Release] to free it.
func (r *PortRegistry) getFreePortFromKernel() (int, error) {
	for range maxPortRetries {
		l, err := net.ListenTCP("tcp", loopbackAddr)
		if err != nil {
			return 0, fmt.Errorf("listen on tcp address: %w", err)
		}
		tcpAddr, ok := l.Addr().(*net.TCPAddr)
		if !ok {
			_ = l.Close()
			return 0, fmt.Errorf("unexpected address type: %T", l.Addr())
		}
		port := tcpAddr.Port
		if r.reserve(port) {
			_ = l.Close()
			return port, nil
		}
		// Port already in registry, close and retry to get a different one.
		_ = l.Close()
		slog.Default().Debug("port already in registry, retrying", "port", port)
	}
	r.mu.Lock()
	n := len(r.ports)
	r.mu.Unlock()
	return 0, fmt.Errorf(
		"allocate unique port: exhausted %d attempts (%d ports in registry)",
		maxPortRetries, n,
	)
}

// AllocatePortPair allocates two distinct free ports.
//
// Ports are registered in the registry to prevent duplicate allocation across
// concurrent callers. Callers must call Release for each port when no longer
// needed.
func (r *PortRegistry) AllocatePortPair() (port1, port2 int, err error) {
	p1, err := r.getFreePortFromKernel()
	if err != nil {
		return 0, 0, fmt.Errorf("allocate first port: %w", err)
	}

	p2, err := r.getFreePortFromKernel()
	if err != nil {
		r.Release(p1)
		return 0, 0, fmt.Errorf("allocate second port: %w", err)
	}

	return p1, p2, nil
}
