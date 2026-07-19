package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// TestModalOverlayFloats: an open modal floats over the dashboard — the status
// bar and sidebar stay visible behind it — and the frame keeps its dimensions.
func TestModalOverlayFloats(t *testing.T) {
	const w, h = 120, 40
	m := sized(t, w, h)
	m = selectStack(t, m, "blog")
	m = press(m, keyRunes("s")) // open the stack actions menu
	if !m.modal.active {
		t.Fatal("s should open the menu")
	}
	view := m.View()

	// The popup is present...
	if !strings.Contains(view, "actions") {
		t.Fatal("modal content missing from frame")
	}
	// ...and the dashboard shows through (status bar not blanked).
	if !strings.Contains(view, "runtime:") {
		t.Fatal("status bar should remain visible behind the modal")
	}
	// A sidebar row outside the centered box should still be visible.
	if !strings.Contains(view, "kazi-proxy") {
		t.Fatal("sidebar should remain visible behind the modal")
	}

	// Dimensions are preserved: one line per row, each full width.
	lines := strings.Split(view, "\n")
	if len(lines) != h {
		t.Fatalf("overlay changed row count: got %d, want %d", len(lines), h)
	}
	for i, ln := range lines {
		if got := lipgloss.Width(ln); got != w {
			t.Fatalf("overlay row %d width = %d, want %d", i, got, w)
		}
	}
}
