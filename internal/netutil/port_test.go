package netutil

import (
	"sync"
	"testing"
)

func TestNewPortRegistry(t *testing.T) {
	t.Parallel()

	t.Run("nil logger uses default", func(t *testing.T) {
		r := NewPortRegistry(nil)
		if r == nil {
			t.Fatal("expected non-nil registry")
		}
		// Verify the registry is functional by reserving and releasing a port.
		if !r.reserve(8080) {
			t.Fatal("expected reserve to succeed on new registry")
		}
		r.Release(8080)
	})
}

func TestPortRegistry_reserve(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		setup      func(r *PortRegistry)
		port       int
		wantOK     bool
		wantLocked bool // whether the port should be reserved after the call
	}{
		"reserve new port": {
			setup:      func(_ *PortRegistry) {},
			port:       8080,
			wantOK:     true,
			wantLocked: true,
		},
		"reserve duplicate port": {
			setup: func(r *PortRegistry) {
				r.reserve(9090)
			},
			port:       9090,
			wantOK:     false,
			wantLocked: true,
		},
		"reserve different ports": {
			setup: func(r *PortRegistry) {
				r.reserve(8080)
			},
			port:       9090,
			wantOK:     true,
			wantLocked: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			r := NewPortRegistry(nil)
			tc.setup(r)

			got := r.reserve(tc.port)
			if got != tc.wantOK {
				t.Errorf("reserve(%d) = %v, want %v", tc.port, got, tc.wantOK)
			}
			// Verify the port is reserved by attempting to reserve it again.
			if tc.wantLocked {
				if r.reserve(tc.port) {
					t.Errorf("port %d should be reserved, but second reserve succeeded", tc.port)
				}
			}
		})
	}
}

func TestPortRegistry_Release(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		setup         func(r *PortRegistry)
		port          int
		wantAvailable bool // whether the port should be available after release
		otherPort     int  // another port that should remain reserved (0 means none)
		otherReserved bool // whether otherPort should remain reserved
	}{
		"release existing port": {
			setup: func(r *PortRegistry) {
				r.reserve(8080)
			},
			port:          8080,
			wantAvailable: true,
		},
		"release non-existent port": {
			setup:         func(_ *PortRegistry) {},
			port:          8080,
			wantAvailable: true, // port was never reserved, so reserve should succeed
		},
		"release one of multiple": {
			setup: func(r *PortRegistry) {
				r.reserve(8080)
				r.reserve(9090)
			},
			port:          8080,
			wantAvailable: true,
			otherPort:     9090,
			otherReserved: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			r := NewPortRegistry(nil)
			tc.setup(r)

			r.Release(tc.port)

			// Verify the released port is now available by reserving it.
			if tc.wantAvailable {
				if !r.reserve(tc.port) {
					t.Errorf("port %d should be available after release, but reserve failed", tc.port)
				}
				r.Release(tc.port) // clean up
			}

			// Verify that other ports remain reserved.
			if tc.otherPort != 0 && tc.otherReserved {
				if r.reserve(tc.otherPort) {
					t.Errorf("port %d should still be reserved, but reserve succeeded", tc.otherPort)
				}
			}
		})
	}
}

func TestPortRegistry_reserveReleaseCycle(t *testing.T) {
	t.Parallel()

	r := NewPortRegistry(nil)

	if !r.reserve(8080) {
		t.Fatal("first reserve should succeed")
	}

	if r.reserve(8080) {
		t.Fatal("duplicate reserve should fail")
	}

	r.Release(8080)
	if !r.reserve(8080) {
		t.Fatal("reserve after release should succeed")
	}
}

func TestPortRegistry_ConcurrentReserve(t *testing.T) {
	t.Parallel()

	r := NewPortRegistry(nil)
	const goroutines = 50

	var wg sync.WaitGroup
	reserved := make(chan int, goroutines)

	for i := range goroutines {
		port := 10000 + i
		wg.Go(func() {
			if r.reserve(port) {
				reserved <- port
			}
		})
	}

	wg.Wait()
	close(reserved)

	count := 0
	for range reserved {
		count++
	}
	if count != goroutines {
		t.Errorf("expected %d reservations, got %d", goroutines, count)
	}
}

func TestPortRegistry_ConcurrentRelease(t *testing.T) {
	t.Parallel()

	r := NewPortRegistry(nil)
	const goroutines = 50

	// Pre-populate ports using the public reserve method.
	for i := range goroutines {
		if !r.reserve(10000 + i) {
			t.Fatalf("setup: failed to reserve port %d", 10000+i)
		}
	}

	var wg sync.WaitGroup
	for i := range goroutines {
		port := 10000 + i
		wg.Go(func() {
			r.Release(port)
		})
	}
	wg.Wait()

	// Verify all ports are available again by reserving them.
	for i := range goroutines {
		port := 10000 + i
		if !r.reserve(port) {
			t.Errorf("port %d should be available after release, but reserve failed", port)
		}
	}
}

func TestPortRegistry_ConcurrentDuplicateReserve(t *testing.T) {
	t.Parallel()

	r := NewPortRegistry(nil)
	const goroutines = 100
	const targetPort = 12345

	var wg sync.WaitGroup
	successes := make(chan bool, goroutines)

	for range goroutines {
		wg.Go(func() {
			successes <- r.reserve(targetPort)
		})
	}

	wg.Wait()
	close(successes)

	successCount := 0
	for ok := range successes {
		if ok {
			successCount++
		}
	}
	if successCount != 1 {
		t.Errorf("expected exactly 1 successful reserve, got %d", successCount)
	}
}

func TestPortRegistry_AllocatePortPair(t *testing.T) {
	t.Parallel()

	r := NewPortRegistry(nil)

	p1, p2, err := r.AllocatePortPair()
	if err != nil {
		t.Fatalf("AllocatePortPair() error: %v", err)
	}

	if p1 == 0 {
		t.Error("port1 should be non-zero")
	}
	if p2 == 0 {
		t.Error("port2 should be non-zero")
	}
	if p1 == p2 {
		t.Errorf("ports should be different: port1=%d, port2=%d", p1, p2)
	}

	// Verify both ports are registered by attempting to reserve them again.
	if r.reserve(p1) {
		t.Errorf("port1 %d should already be registered, but reserve succeeded", p1)
	}
	if r.reserve(p2) {
		t.Errorf("port2 %d should already be registered, but reserve succeeded", p2)
	}

	// Release and verify both ports become available again.
	r.Release(p1)
	r.Release(p2)

	if !r.reserve(p1) {
		t.Errorf("port1 %d should be available after release, but reserve failed", p1)
	}
	if !r.reserve(p2) {
		t.Errorf("port2 %d should be available after release, but reserve failed", p2)
	}
}

func TestPortRegistry_AllocateMultiplePairs(t *testing.T) {
	t.Parallel()

	r := NewPortRegistry(nil)

	seen := make(map[int]bool)
	const pairs = 5

	for i := range pairs {
		p1, p2, err := r.AllocatePortPair()
		if err != nil {
			t.Fatalf("pair %d: AllocatePortPair() error: %v", i, err)
		}
		if seen[p1] {
			t.Errorf("pair %d: port1 %d already seen", i, p1)
		}
		if seen[p2] {
			t.Errorf("pair %d: port2 %d already seen", i, p2)
		}
		seen[p1] = true
		seen[p2] = true
	}

	for port := range seen {
		r.Release(port)
	}
}
