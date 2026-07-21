package tui

import (
	"bytes"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"
)

// waitForContains blocks until the program output contains every substring.
func waitForContains(t *testing.T, tm *teatest.TestModel, subs ...string) {
	t.Helper()
	teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
		for _, s := range subs {
			if !bytes.Contains(b, []byte(s)) {
				return false
			}
		}
		return true
	}, teatest.WithDuration(3*time.Second))
}

// newProgram spins a teatest program over the fake engine at a large size.
func newProgram(t *testing.T) *teatest.TestModel {
	t.Helper()
	m := New(fakeEngine{}, time.Hour) // slow tick; we drive keys explicitly
	return teatest.NewTestModel(t, m, teatest.WithInitialTermSize(120, 40))
}

// TestInitialOverview: the initial frame shows the ALL overview listing every
// stack, tagged by kind icons rather than group headers.
func TestInitialOverview(t *testing.T) {
	tm := newProgram(t)
	waitForContains(t, tm,
		"blog", "api", "redis", "n8n", "kazi-proxy",
		"Overview",
		kindGlyph(selStack), kindGlyph(selDiscovered), kindGlyph(selUnmanaged), kindGlyph(selSystem),
	)
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	tm.WaitFinished(t, teatest.WithFinalTimeout(3*time.Second))
}

// TestSelectStackServicesTab: descending onto "blog" shows the Services tab
// with its service rows.
func TestSelectStackServicesTab(t *testing.T) {
	tm := newProgram(t)
	waitForContains(t, tm, "blog")
	// j -> first stack (blog), l -> focus detail.
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	waitForContains(t, tm, "Services", "SERVICE", "web", "db", "healthy")
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	tm.WaitFinished(t, teatest.WithFinalTimeout(3*time.Second))
}

// TestURLsTab: [ ] to the URLs tab shows resolved endpoints for blog.
func TestURLsTab(t *testing.T) {
	tm := newProgram(t)
	waitForContains(t, tm, "blog")
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")}) // blog
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")}) // focus detail
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("]")}) // Logs
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("]")}) // Env
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("]")}) // URLs
	waitForContains(t, tm, "URLs", "https://blog.localhost")
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	tm.WaitFinished(t, teatest.WithFinalTimeout(3*time.Second))
}

// TestLogsStream: the Logs tab tails the live stream for the selected stack.
func TestLogsStream(t *testing.T) {
	tm := newProgram(t)
	waitForContains(t, tm, "blog")
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")}) // blog
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")}) // focus detail
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("]")}) // Logs
	waitForContains(t, tm, "GET / 200", "ready to accept connections")
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	tm.WaitFinished(t, teatest.WithFinalTimeout(3*time.Second))
}

// TestStatsTabStream: the Stats tab streams live per-service resource samples
// for the selected stack.
func TestStatsTabStream(t *testing.T) {
	tm := newProgram(t)
	waitForContains(t, tm, "blog")
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")}) // blog
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")}) // focus detail
	for range 5 { // Services → … → Stats
		tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("]")})
	}
	waitForContains(t, tm, "Stats", "web", "PIDs 14", "db", "PIDs 20")
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	tm.WaitFinished(t, teatest.WithFinalTimeout(3*time.Second))
}

// The ALL host overview (CPU/Mem/Disk graphs + aggregate line) is covered by
// direct-render unit tests in stats_test.go; a live teatest is avoided because
// teatest's virtual terminal clips the lower overview rows unreliably (the
// feature itself renders fine in a real terminal).

// TestStatusBar: the status bar reflects runtime, proxy and gc count.
func TestStatusBar(t *testing.T) {
	tm := newProgram(t)
	waitForContains(t, tm, "runtime:", "proxy:", "gc:2")
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	tm.WaitFinished(t, teatest.WithFinalTimeout(3*time.Second))
}

// TestHelpOverlay: ? shows the keymap overlay.
func TestHelpOverlay(t *testing.T) {
	tm := newProgram(t)
	waitForContains(t, tm, "blog")
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	waitForContains(t, tm, "keymap", "quit", "focus")
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	tm.WaitFinished(t, teatest.WithFinalTimeout(3*time.Second))
}

// TestKeybarContextual: selecting a running stack shows its action keys;
// selecting the system stack never shows rm.
func TestKeybarContextual(t *testing.T) {
	m := loaded(t)
	// Move to blog (running registered) and render the keybar.
	m = press(m, keyRunes("j"))
	bar := m.keybarLine()
	for _, want := range []string{"menu", "logs", "remove"} {
		if !strings.Contains(bar, want) {
			t.Fatalf("running-stack keybar missing %q: %s", want, bar)
		}
	}
	// Jump to the last selectable row (kazi-proxy is the system stack, last group).
	m = press(m, keyRunes("G"))
	if m.selectedRow().selKind != selSystem {
		t.Fatalf("expected system selection at bottom, got %v", m.selectedRow().selKind)
	}
	sysBar := m.keybarLine()
	if strings.Contains(sysBar, "delete") {
		t.Fatalf("system keybar must not contain delete: %s", sysBar)
	}
	if !strings.Contains(sysBar, "trust") {
		t.Fatalf("system keybar should contain trust: %s", sysBar)
	}
}
