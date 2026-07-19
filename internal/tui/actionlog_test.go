package tui

import (
	"strings"
	"testing"
)

// TestActionLogPersistAndLoad: a finished action's output is appended to the log
// file and the tail loads back as panel history.
func TestActionLogPersistAndLoad(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())

	// Persist one action's output.
	cmd := appendActionLogCmd("up blog ✓", []string{"[+] Running 1/1", "Container blog-1  Started"})
	if cmd == nil {
		t.Fatal("append cmd should not be nil for non-empty output")
	}
	cmd() // writes the file

	// An empty action is not persisted.
	if appendActionLogCmd("noop", nil) != nil {
		t.Fatal("empty output should produce no append cmd")
	}

	// Load the tail back.
	msg, ok := loadActionHistoryCmd()().(actionHistoryMsg)
	if !ok {
		t.Fatal("loadActionHistoryCmd should return actionHistoryMsg")
	}
	joined := strings.Join(msg.lines, "\n")
	for _, want := range []string{"up blog ✓", "Container blog-1  Started"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("loaded history missing %q: %q", want, joined)
		}
	}
}

// TestActionHistoryPopulatesPanel: startup history fills the panel until an
// action runs.
func TestActionHistoryPopulatesPanel(t *testing.T) {
	m := loaded(t)
	nm, _ := m.Update(actionHistoryMsg{lines: []string{"=== old run ===", "Container x Started"}})
	m = nm.(Model)
	if m.actionTitle == "" {
		t.Fatal("history should make the Action panel available")
	}
	if len(m.actionLines) != 2 {
		t.Fatalf("history lines not loaded: %v", m.actionLines)
	}
	// A live action replaces the history view.
	nm, _ = m.Update(actionStreamMsg{action: "restart", stack: "blog"})
	m = nm.(Model)
	if m.actionTitle != "restart blog" || len(m.actionLines) != 0 {
		t.Fatalf("live action should replace history, got title=%q lines=%v", m.actionTitle, m.actionLines)
	}
}
