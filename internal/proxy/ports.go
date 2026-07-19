package proxy

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/thapakazi/kazi/internal/store"
	"gopkg.in/yaml.v3"
)

// Allocation records a single host-port assignment for a stack service.
type Allocation struct {
	Stack         string `yaml:"stack"`
	Service       string `yaml:"service"`
	ContainerPort int    `yaml:"containerPort"`
	HostPort      int    `yaml:"hostPort"`
}

// PortState holds all current port allocations; persisted to
// <store.Root()>/state/ports.yaml.
type PortState struct {
	Allocations []Allocation `yaml:"allocations"`
}

func statePath() string {
	return filepath.Join(store.Root(), "state", "ports.yaml")
}

// LoadPorts reads state from disk. If the file is absent, an empty state is
// returned without error.
func LoadPorts() (*PortState, error) {
	b, err := os.ReadFile(statePath())
	if os.IsNotExist(err) {
		return &PortState{}, nil
	}
	if err != nil {
		return nil, err
	}
	var s PortState
	if err := yaml.Unmarshal(b, &s); err != nil {
		return nil, fmt.Errorf("parsing ports.yaml: %w", err)
	}
	return &s, nil
}

// Save writes the current state to disk, creating parent directories as needed.
func (s *PortState) Save() error {
	dir := filepath.Dir(statePath())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b, err := yaml.Marshal(s)
	if err != nil {
		return err
	}
	return os.WriteFile(statePath(), b, 0o644)
}

// Lookup returns the allocation for (stack, service) if one exists.
func (s *PortState) Lookup(stack, service string) (Allocation, bool) {
	for _, a := range s.Allocations {
		if a.Stack == stack && a.Service == service {
			return a, true
		}
	}
	return Allocation{}, false
}

// portFree reports whether the port is available to bind on the host.
// We probe both 0.0.0.0 and 127.0.0.1 so that a listener bound to either
// interface is detected as busy.
//
// The probes are SEQUENTIAL: ln1 (:p) is fully closed before ln2
// (127.0.0.1:p) is attempted. Holding both at once is wrong on Linux, where
// binding 127.0.0.1:p while 0.0.0.0:p is held fails with EADDRINUSE — which
// made every port look busy under Linux CI. Sequential probing is correct on
// both Linux and darwin; the tiny TOCTOU window is acceptable for a
// best-effort availability check.
func portFree(p int) bool {
	ln1, err := net.Listen("tcp", fmt.Sprintf(":%d", p))
	if err != nil {
		return false
	}
	ln1.Close()
	// Also probe loopback to catch listeners bound to 127.0.0.1 only.
	ln2, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p))
	if err != nil {
		return false
	}
	ln2.Close()
	return true
}

// allocatedPorts returns the set of all host ports already allocated.
func (s *PortState) allocatedPorts() map[int]bool {
	m := make(map[int]bool, len(s.Allocations))
	for _, a := range s.Allocations {
		m[a.HostPort] = true
	}
	return m
}

// Allocate assigns a host port for (stack, service, containerPort).
//
// Idempotent: if an identical allocation already exists it is returned
// unchanged. If pinned==0 the first free port in [lo, hi] is chosen,
// skipping allocated and probe-busy ports. A pinned port is used as-is
// after conflict and host-busy checks.
func (s *PortState) Allocate(stack, service string, containerPort, pinned, lo, hi int) (int, error) {
	// Look for an existing allocation for this exact key.
	for _, a := range s.Allocations {
		if a.Stack == stack && a.Service == service && a.ContainerPort == containerPort {
			if pinned == 0 || a.HostPort == pinned {
				// Idempotent: same pin (or auto) — return existing.
				return a.HostPort, nil
			}
			// Caller wants to re-pin to a different port; fall through to
			// conflict / busy checks then update.
			break
		}
	}

	if pinned != 0 {
		// Check: is this pinned port already taken by a different key?
		for _, a := range s.Allocations {
			if a.HostPort == pinned {
				if a.Stack == stack && a.Service == service && a.ContainerPort == containerPort {
					// Same key — idempotent.
					return pinned, nil
				}
				return 0, fmt.Errorf("port %d is already allocated to %s/%s", pinned, a.Stack, a.Service)
			}
		}
		// Check: is the pinned port busy on the host?
		if !portFree(pinned) {
			return 0, fmt.Errorf("port %d is already in use on the host", pinned)
		}
		// Record / update.
		s.upsert(stack, service, containerPort, pinned)
		if err := s.Save(); err != nil {
			return 0, err
		}
		return pinned, nil
	}

	// Auto-allocation: pick first free port in [lo, hi] not already allocated
	// and not probe-busy.
	allocated := s.allocatedPorts()
	rangeStr := fmt.Sprintf("%d-%d", lo, hi)
	for p := lo; p <= hi; p++ {
		if allocated[p] {
			continue
		}
		if !portFree(p) {
			continue
		}
		s.upsert(stack, service, containerPort, p)
		if err := s.Save(); err != nil {
			return 0, err
		}
		return p, nil
	}
	return 0, fmt.Errorf("port range %s exhausted; widen spec.ports.range in config.yaml", rangeStr)
}

// upsert adds or replaces an allocation for (stack, service, containerPort).
func (s *PortState) upsert(stack, service string, containerPort, hostPort int) {
	for i, a := range s.Allocations {
		if a.Stack == stack && a.Service == service && a.ContainerPort == containerPort {
			s.Allocations[i].HostPort = hostPort
			return
		}
	}
	s.Allocations = append(s.Allocations, Allocation{
		Stack:         stack,
		Service:       service,
		ContainerPort: containerPort,
		HostPort:      hostPort,
	})
}

// Free removes the allocation for (stack, service). Returns true if something
// was freed; saves state on success.
func (s *PortState) Free(stack, service string) bool {
	for i, a := range s.Allocations {
		if a.Stack == stack && a.Service == service {
			s.Allocations = append(s.Allocations[:i], s.Allocations[i+1:]...)
			_ = s.Save()
			return true
		}
	}
	return false
}

// FreeStack removes all allocations for the given stack and saves.
func (s *PortState) FreeStack(stack string) {
	var kept []Allocation
	for _, a := range s.Allocations {
		if a.Stack != stack {
			kept = append(kept, a)
		}
	}
	s.Allocations = kept
	_ = s.Save()
}

// Services returns all allocations for the given stack.
func (s *PortState) Services(stack string) []Allocation {
	var out []Allocation
	for _, a := range s.Allocations {
		if a.Stack == stack {
			out = append(out, a)
		}
	}
	return out
}

// ParseRange parses a "lo-hi" port range string, e.g. "42000-42999".
func ParseRange(r string) (lo, hi int, err error) {
	parts := strings.SplitN(r, "-", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return 0, 0, fmt.Errorf("invalid port range %q: expected \"lo-hi\"", r)
	}
	lo, err = strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, fmt.Errorf("invalid port range %q: %w", r, err)
	}
	hi, err = strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, fmt.Errorf("invalid port range %q: %w", r, err)
	}
	if lo >= hi {
		return 0, 0, fmt.Errorf("invalid port range %q: lo must be less than hi", r)
	}
	if lo < 1 || hi > 65535 {
		return 0, 0, fmt.Errorf("invalid port range %q: ports must be within 1-65535", r)
	}
	return lo, hi, nil
}
