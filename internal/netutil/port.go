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
	log   *slog.Logger
}

// NewPortRegistry creates a new PortRegistry ready for use.
// If logger is nil, slog.Default() is used as a fallback.
func NewPortRegistry(logger *slog.Logger) *PortRegistry {
	if logger == nil {
		logger = slog.Default()
	}
	return &PortRegistry{
		ports: make(map[int]struct{}),
		log:   logger,
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
func (r *PortRegistry) Release(port int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.ports, port)
}

// getFreePortFromKernel asks the kernel for a free port, skipping any ports
// already in the registry. On success it returns an open [net.TCPListener] that
// the caller must close when the port is no longer needed to be held open. The
// port is also registered in the registry; the caller must call [PortRegistry.Release]
// separately to free it from the registry.
func (r *PortRegistry) getFreePortFromKernel() (*net.TCPListener, int, error) {
	addr, err := net.ResolveTCPAddr("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, 0, fmt.Errorf("resolve tcp address: %w", err)
	}

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
		r.log.Debug("port already in registry, retrying", "port", tcpAddr.Port)
		_ = l.Close()
	}
	return nil, 0, fmt.Errorf("allocate unique port: exhausted %d attempts", maxPortRetries)
}

// AllocatePortPair allocates two distinct free ports.
//
// Both listeners are held open simultaneously before either is closed,
// guaranteeing the kernel assigns different ports. Ports are registered in the
// registry to prevent duplicate allocation across concurrent callers. Callers
// must call Release for each port when no longer needed.
func (r *PortRegistry) AllocatePortPair() (port1, port2 int, err error) {
	l1, p1, err := r.getFreePortFromKernel()
	if err != nil {
		return 0, 0, fmt.Errorf("allocate first port: %w", err)
	}

	l2, p2, err := r.getFreePortFromKernel()
	if err != nil {
		// Close the listener BEFORE releasing the port from the registry.
		// This prevents a TOCTOU race where another goroutine allocates the
		// same port while the listener still holds it.
		if closeErr := l1.Close(); closeErr != nil {
			r.log.Warn("close listener after port allocation", "port", p1, "error", closeErr)
		}
		r.Release(p1)
		return 0, 0, fmt.Errorf("allocate second port: %w", err)
	}

	// Success path: close both listeners. Order does not matter here since
	// both ports remain registered and protected from reallocation.
	if closeErr := l1.Close(); closeErr != nil {
		r.log.Warn("close listener after port allocation", "port", p1, "error", closeErr)
	}
	if closeErr := l2.Close(); closeErr != nil {
		r.log.Warn("close listener after port allocation", "port", p2, "error", closeErr)
	}

	return p1, p2, nil
}
