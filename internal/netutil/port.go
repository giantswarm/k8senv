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
// already in the registry. On success it returns an open [net.TCPListener] and
// the assigned port number. The port is registered in the registry; the caller
// must call [PortRegistry.Release] to free it. The caller should close the
// listener when it is no longer needed (typically immediately, since the registry
// entry prevents duplicate allocation).
func (r *PortRegistry) getFreePortFromKernel() (*net.TCPListener, int, error) {
	addr := &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)}

	for range maxPortRetries {
		l, err := net.ListenTCP("tcp", addr)
		if err != nil {
			return nil, 0, fmt.Errorf("listen on tcp address: %w", err)
		}
		tcpAddr, ok := l.Addr().(*net.TCPAddr)
		if !ok {
			_ = l.Close()
			return nil, 0, fmt.Errorf("unexpected address type: %T", l.Addr())
		}
		if r.reserve(tcpAddr.Port) {
			return l, tcpAddr.Port, nil
		}
		// Port already in registry, close and retry to get a different one.
		slog.Default().Debug("port already in registry, retrying", "port", tcpAddr.Port)
		_ = l.Close()
	}
	r.mu.Lock()
	n := len(r.ports)
	r.mu.Unlock()
	return nil, 0, fmt.Errorf(
		"allocate unique port: exhausted %d attempts (%d ports in registry)",
		maxPortRetries, n,
	)
}

// AllocatePortPair allocates two distinct free ports.
//
// Ports are registered in the registry to prevent duplicate allocation across
// concurrent callers. Each listener is closed as soon as the port is registered,
// since the registry entry (not the open listener) is the TOCTOU guard. Callers
// must call Release for each port when no longer needed.
func (r *PortRegistry) AllocatePortPair() (port1, port2 int, err error) {
	l1, p1, err := r.getFreePortFromKernel()
	if err != nil {
		return 0, 0, fmt.Errorf("allocate first port: %w", err)
	}
	closeListener(l1, p1)

	l2, p2, err := r.getFreePortFromKernel()
	if err != nil {
		r.Release(p1)
		return 0, 0, fmt.Errorf("allocate second port: %w", err)
	}
	closeListener(l2, p2)

	return p1, p2, nil
}

// closeListener closes a TCP listener and logs a warning on failure.
func closeListener(l *net.TCPListener, port int) {
	if err := l.Close(); err != nil {
		slog.Default().Warn("close listener after port allocation", "port", port, "error", err)
	}
}
