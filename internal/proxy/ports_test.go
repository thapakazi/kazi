package proxy

import (
	"net"
	"strings"
	"testing"
)

func TestAllocateStableAndDistinct(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	s, err := LoadPorts()
	if err != nil {
		t.Fatal(err)
	}
	p1, err := s.Allocate("blog", "postgres", 5432, 0, 42000, 42010)
	if err != nil || p1 < 42000 || p1 > 42010 {
		t.Fatalf("p1=%d err=%v", p1, err)
	}
	p2, _ := s.Allocate("shop", "postgres", 5432, 0, 42000, 42010)
	if p2 == p1 {
		t.Error("distinct keys must get distinct ports")
	}
	// stability: same key returns same port, across reload
	again, _ := s.Allocate("blog", "postgres", 5432, 0, 42000, 42010)
	if again != p1 {
		t.Errorf("not stable: %d vs %d", again, p1)
	}
	s2, _ := LoadPorts()
	persisted, _ := s2.Allocate("blog", "postgres", 5432, 0, 42000, 42010)
	if persisted != p1 {
		t.Errorf("not persisted: %d vs %d", persisted, p1)
	}
}

func TestAllocatePinnedAndConflict(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	s, _ := LoadPorts()
	p, err := s.Allocate("blog", "postgres", 5432, 42005, 42000, 42010)
	if err != nil || p != 42005 {
		t.Fatalf("p=%d err=%v", p, err)
	}
	_, err = s.Allocate("shop", "redis", 6379, 42005, 42000, 42010)
	if err == nil || !strings.Contains(err.Error(), "already allocated to blog/postgres") {
		t.Errorf("want allocation conflict, got %v", err)
	}
}

func TestAllocatePinnedHostBusy(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	busy := ln.Addr().(*net.TCPAddr).Port
	s, _ := LoadPorts()
	if _, err := s.Allocate("blog", "postgres", 5432, busy, busy, busy); err == nil ||
		!strings.Contains(err.Error(), "already in use on the host") {
		t.Errorf("want host-busy error, got %v", err)
	}
}

func TestAllocateExhaustion(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	s, _ := LoadPorts()
	s.Allocate("a", "x", 1, 0, 42000, 42001)
	s.Allocate("a", "y", 2, 0, 42000, 42001)
	_, err := s.Allocate("a", "z", 3, 0, 42000, 42001)
	if err == nil || !strings.Contains(err.Error(), "exhausted") {
		t.Errorf("want exhaustion, got %v", err)
	}
}

func TestFreeAndFreeStack(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	s, _ := LoadPorts()
	s.Allocate("blog", "postgres", 5432, 0, 42000, 42010)
	s.Allocate("blog", "redis", 6379, 0, 42000, 42010)
	if !s.Free("blog", "postgres") {
		t.Error("Free should report true")
	}
	if _, ok := s.Lookup("blog", "postgres"); ok {
		t.Error("still allocated after Free")
	}
	s.FreeStack("blog")
	if _, ok := s.Lookup("blog", "redis"); ok {
		t.Error("still allocated after FreeStack")
	}
}

func TestParseRange(t *testing.T) {
	lo, hi, err := ParseRange("42000-42999")
	if err != nil || lo != 42000 || hi != 42999 {
		t.Errorf("lo=%d hi=%d err=%v", lo, hi, err)
	}
	for _, bad := range []string{"", "x-y", "42000", "42999-42000"} {
		if _, _, err := ParseRange(bad); err == nil {
			t.Errorf("ParseRange(%q) should fail", bad)
		}
	}
}
