package tui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// loaded returns a model with the fake engine's snapshot applied (rows built),
// ready to drive motion keys against without a live ticker.
func loaded(t *testing.T) Model {
	t.Helper()
	m := New(fakeEngine{}, time.Second)
	// Apply the snapshot the Init command would have produced.
	msg := snapshotCmd(m.eng)()
	nm, _ := m.Update(msg)
	return nm.(Model)
}

func keyRunes(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func special(t tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: t} }

func press(m Model, k tea.KeyMsg) Model {
	nm, _ := m.Update(k)
	return nm.(Model)
}

func TestInitialSelectionIsAll(t *testing.T) {
	m := loaded(t)
	r := m.selectedRow()
	if r == nil || r.kind != rowAll {
		t.Fatalf("initial selection = %+v, want ALL", r)
	}
}

func TestMotionDownUp(t *testing.T) {
	m := loaded(t)
	start := m.sel
	m = press(m, keyRunes("j"))
	if m.sel <= start {
		t.Fatalf("j did not move selection down: %d -> %d", start, m.sel)
	}
	if m.rows[m.sel].kind == rowHeader {
		t.Fatal("j landed on a header row")
	}
	afterJ := m.sel
	m = press(m, keyRunes("k"))
	if m.sel >= afterJ {
		t.Fatalf("k did not move selection up: %d -> %d", afterJ, m.sel)
	}
}

func TestArrowsAliasJK(t *testing.T) {
	byJK := press(press(loaded(t), keyRunes("j")), keyRunes("j"))
	byArrows := press(press(loaded(t), special(tea.KeyDown)), special(tea.KeyDown))
	if byJK.sel != byArrows.sel {
		t.Fatalf("arrows (%d) do not alias j/k (%d)", byArrows.sel, byJK.sel)
	}
}

func TestJumpTopBottom(t *testing.T) {
	m := loaded(t)
	m = press(m, keyRunes("G"))
	last := m.sel
	if m.rows[last].kind == rowHeader {
		t.Fatal("G landed on a header")
	}
	// G should be at the final selectable row.
	for i := last + 1; i < len(m.rows); i++ {
		if m.rows[i].kind != rowHeader {
			t.Fatalf("G not at bottom: selectable row exists at %d", i)
		}
	}
	m = press(m, keyRunes("g"))
	if m.selectedRow().kind != rowAll {
		t.Fatalf("g did not jump to top (ALL), got %+v", m.selectedRow())
	}
}

func TestFocusSwitch(t *testing.T) {
	m := loaded(t)
	if m.focus != focusSidebar {
		t.Fatal("default focus should be sidebar")
	}
	m = press(m, keyRunes("l"))
	if m.focus != focusDetail {
		t.Fatal("l should focus detail")
	}
	m = press(m, keyRunes("h"))
	if m.focus != focusSidebar {
		t.Fatal("h should focus sidebar")
	}
}

func TestTabTogglesMode(t *testing.T) {
	m := loaded(t)
	if m.mode != modeStacks {
		t.Fatal("default mode should be Stacks")
	}
	m = press(m, special(tea.KeyTab))
	if m.mode != modeCatalog {
		t.Fatal("Tab should switch to Catalog")
	}
	m = press(m, special(tea.KeyTab))
	if m.mode != modeStacks {
		t.Fatal("Tab should switch back to Stacks")
	}
	// Numeric mode keys.
	m = press(m, keyRunes("2"))
	if m.mode != modeCatalog {
		t.Fatal("2 should select Catalog")
	}
	m = press(m, keyRunes("1"))
	if m.mode != modeStacks {
		t.Fatal("1 should select Stacks")
	}
}

func TestTabCyclingInDetail(t *testing.T) {
	m := loaded(t)
	// Move onto a stack and focus detail.
	m = press(m, keyRunes("j")) // onto first stack
	m = press(m, keyRunes("l")) // focus detail
	if m.tab != tabServices {
		t.Fatalf("default tab should be Services, got %v", m.tab)
	}
	m = press(m, keyRunes("]"))
	if m.tab != tabLogs {
		t.Fatalf("] should move to Logs, got %v", m.tab)
	}
	m = press(m, keyRunes("["))
	if m.tab != tabServices {
		t.Fatalf("[ should move back to Services, got %v", m.tab)
	}
}

func TestQuit(t *testing.T) {
	m := loaded(t)
	_, cmd := m.Update(keyRunes("q"))
	if cmd == nil {
		t.Fatal("q should return a quit command")
	}
	if msg := cmd(); msg == nil {
		t.Fatal("quit command should produce a message")
	}
}

func TestErrMsgFlagsStaleKeepsFrame(t *testing.T) {
	m := loaded(t)
	rowsBefore := len(m.rows)
	nm, _ := m.Update(errMsg{err: context_error()})
	m = nm.(Model)
	if !m.stale {
		t.Fatal("errMsg should set stale")
	}
	if len(m.rows) != rowsBefore {
		t.Fatal("errMsg should not drop the last good frame")
	}
}

func context_error() error { return errStub{} }

type errStub struct{}

func (errStub) Error() string { return "boom" }
