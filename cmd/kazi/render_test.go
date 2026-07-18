package main

import (
	"testing"

	"github.com/thapakazi/kazi/internal/engine"
)

func TestStatusCell(t *testing.T) {
	if got := statusCell(engine.StackInfo{Running: 3, Total: 3}); got != "running 3/3" {
		t.Errorf("got %q", got)
	}
	if got := statusCell(engine.StackInfo{Running: 1, Total: 2}); got != "running 1/2" {
		t.Errorf("got %q", got)
	}
	if got := statusCell(engine.StackInfo{Running: 0, Total: 2}); got != "stopped" {
		t.Errorf("got %q", got)
	}
	if got := statusCell(engine.StackInfo{}); got != "stopped" {
		t.Errorf("got %q", got)
	}
}
