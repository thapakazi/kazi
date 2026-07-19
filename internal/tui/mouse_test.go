package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// sized returns a loaded model resized to w×h (window-size applied).
func sized(t *testing.T, w, h int) Model {
	t.Helper()
	m := loaded(t)
	nm, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return nm.(Model)
}

func leftClick(m Model, x, y int) Model {
	nm, _ := m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: x, Y: y})
	return nm.(Model)
}

// firstStackRow returns the index of the first selectable stack row.
func firstStackRow(m Model) int {
	for i, r := range m.rows {
		if r.kind == rowStack {
			return i
		}
	}
	return -1
}

// TestFullScreenFillsFrame: View exactly fills the terminal — one line per row,
// every line padded to the full width.
func TestFullScreenFillsFrame(t *testing.T) {
	const w, h = 100, 30
	m := sized(t, w, h)
	lines := strings.Split(m.View(), "\n")
	if len(lines) != h {
		t.Fatalf("expected %d rows, got %d", h, len(lines))
	}
	for i, ln := range lines {
		if got := lipgloss.Width(ln); got != w {
			t.Fatalf("row %d width = %d, want %d: %q", i, got, w, ln)
		}
	}
}

// TestMouseClickSelectsRow: clicking a sidebar stack row selects it and focuses
// the sidebar.
func TestMouseClickSelectsRow(t *testing.T) {
	m := sized(t, 100, 30)
	idx := firstStackRow(m)
	if idx < 0 {
		t.Fatal("no stack row in fixture")
	}
	m = leftClick(m, 3, bodyTop+idx) // x inside sidebar, y on the row
	if m.sel != idx {
		t.Fatalf("click selected row %d, want %d", m.sel, idx)
	}
	if m.focus != focusSidebar {
		t.Fatalf("click should focus sidebar, got %v", m.focus)
	}
}

// TestMouseClickBelowRowsInert: clicking past the last row leaves selection.
func TestMouseClickBelowRowsInert(t *testing.T) {
	m := sized(t, 100, 30)
	before := m.sel
	m = leftClick(m, 3, bodyTop+len(m.rows)+2) // empty area below the list
	if m.sel != before {
		t.Fatalf("click in empty area moved selection to %d (was %d)", m.sel, before)
	}
}

// TestMouseWheelScrolls: wheel down advances the sidebar selection.
func TestMouseWheelScrolls(t *testing.T) {
	m := sized(t, 100, 30)
	before := m.sel
	nm, _ := m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown})
	m = nm.(Model)
	if m.sel == before {
		t.Fatalf("wheel down did not move selection from %d", before)
	}
	nm, _ = m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelUp})
	if nm.(Model).sel != before {
		t.Fatalf("wheel up did not return to %d, got %d", before, nm.(Model).sel)
	}
}

// TestMouseClickTab: clicking the URLs tab in the detail header switches tabs
// and focuses the detail pane.
func TestMouseClickTab(t *testing.T) {
	m := sized(t, 120, 30)
	// Select a stack so the detail shows tabs.
	idx := firstStackRow(m)
	m = leftClick(m, 3, bodyTop+idx)
	_, segs := m.tabSegs()
	var target segment
	for _, s := range segs {
		if s.id == "tab:"+string(rune('0'+int(tabURLs))) {
			target = s
		}
	}
	if target.id == "" {
		t.Fatal("URLs tab span not found")
	}
	x := detailContentX0 + (target.start+target.end)/2
	m = leftClick(m, x, bodyTop) // tab header is the first detail row
	if m.tab != tabURLs {
		t.Fatalf("tab click set tab %v, want URLs", m.tab)
	}
	if m.focus != focusDetail {
		t.Fatalf("tab click should focus detail, got %v", m.focus)
	}
}

// TestMouseClickKeybarMode: clicking the mode segment in the keybar toggles mode.
func TestMouseClickKeybarMode(t *testing.T) {
	m := sized(t, 100, 30)
	if m.mode != modeStacks {
		t.Fatalf("expected initial modeStacks, got %v", m.mode)
	}
	_, segs := m.keybarSegs()
	var mode segment
	for _, s := range segs {
		if s.id == "mode" {
			mode = s
		}
	}
	if mode.id == "" {
		t.Fatal("mode segment not found in keybar")
	}
	x := (mode.start + mode.end) / 2
	m = leftClick(m, x, m.height-keybarH)
	if m.mode != modeCatalog {
		t.Fatalf("keybar mode click did not toggle to Catalog, got %v", m.mode)
	}
}
