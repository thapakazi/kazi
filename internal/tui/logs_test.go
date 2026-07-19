package tui

import (
	"context"
	"fmt"
	"io"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/thapakazi/kazi/internal/engine"
)

// recordEngine wraps the fake engine to capture the opts the Logs tab passes to
// LogStream when tail/since change (t/s restart the stream).
type recordEngine struct {
	fakeEngine
	lastOpts engine.LogStreamOpts
	calls    int
}

func (r *recordEngine) LogStream(ctx context.Context, name, service string, opts engine.LogStreamOpts) (io.ReadCloser, context.CancelFunc, error) {
	r.lastOpts = opts
	r.calls++
	return r.fakeEngine.LogStream(ctx, name, service, opts)
}

// logsModel builds a model parked on the Logs tab (detail focused) for stack
// "blog", pre-seeded with lines — the state the log keys operate over.
func logsModel(lines []string) Model {
	m := New(fakeEngine{}, time.Second)
	m.mode = modeStacks
	m.focus = focusDetail
	m.tab = tabLogs
	m.logStack = "blog"
	m.logStreaming = true
	m.logLines = lines
	return m
}

func logLinesN(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("line %d", i)
	}
	return out
}

func TestFollowToggleFreezesViewWhileBuffering(t *testing.T) {
	m := logsModel(logLinesN(50))
	if !m.logFollow {
		t.Fatal("follow should default on")
	}
	m = press(m, keyRunes("f"))
	if m.logFollow {
		t.Fatal("f should pause follow")
	}
	topBefore := m.logTop()
	// The stream keeps buffering while paused; the view stays put.
	nm, _ := m.Update(logLineMsg{stack: "blog", line: "fresh line"})
	m = nm.(Model)
	if len(m.logLines) != 51 {
		t.Fatalf("buffer should still grow while paused: %d", len(m.logLines))
	}
	if m.logTop() != topBefore {
		t.Fatalf("paused view moved: top %d -> %d", topBefore, m.logTop())
	}
	m = press(m, keyRunes("f"))
	if !m.logFollow {
		t.Fatal("f should resume follow")
	}
}

func TestTailCycleRestartsStreamWithOpts(t *testing.T) {
	m := logsModel(logLinesN(10))
	rec := &recordEngine{}
	m.eng = rec
	nm, cmd := m.Update(keyRunes("t"))
	m = nm.(Model)
	if m.logTail != "1000" {
		t.Fatalf("t should cycle tail 500 -> 1000, got %q", m.logTail)
	}
	if cmd == nil {
		t.Fatal("t should restart the stream (non-nil cmd)")
	}
	cmd() // run the restart so the engine records the opts
	if rec.lastOpts.Tail != "1000" {
		t.Fatalf("restart opts.Tail = %q, want 1000", rec.lastOpts.Tail)
	}
}

func TestSinceCycleMapsAllToEmpty(t *testing.T) {
	m := logsModel(logLinesN(10))
	rec := &recordEngine{}
	m.eng = rec
	// Default since is "all"; s wraps the ladder to "1m".
	nm, cmd := m.Update(keyRunes("s"))
	m = nm.(Model)
	if m.logSince != "1m" {
		t.Fatalf("s should cycle since all -> 1m, got %q", m.logSince)
	}
	cmd()
	if rec.lastOpts.Since != "1m" {
		t.Fatalf("restart opts.Since = %q, want 1m", rec.lastOpts.Since)
	}
	// "all" maps to no --since.
	m.logSince = "all"
	if got := m.logStreamOpts().Since; got != "" {
		t.Fatalf("since \"all\" should map to empty, got %q", got)
	}
}

func TestLogSearchMatchesAndCycle(t *testing.T) {
	m := logsModel([]string{
		"GET / 200",
		"db ERROR could not connect",
		"GET /health 200",
		"ERROR again",
	})
	m = press(m, keyRunes("/"))
	if !m.logSearching {
		t.Fatal("/ should enter log-search input")
	}
	m = press(m, keyRunes("err")) // case-insensitive
	idx := m.logMatchIndices()
	if len(idx) != 2 {
		t.Fatalf("matches = %v, want 2 (indices 1 and 3)", idx)
	}
	m = press(m, special(tea.KeyEnter)) // lock the search
	if m.logSearching {
		t.Fatal("Enter should lock the search")
	}
	if m.logMatchCur != 0 {
		t.Fatalf("locked match cursor = %d, want 0", m.logMatchCur)
	}
	m = press(m, keyRunes("n"))
	if m.logMatchCur != 1 {
		t.Fatalf("n -> cursor %d, want 1", m.logMatchCur)
	}
	m = press(m, keyRunes("n")) // wraps
	if m.logMatchCur != 0 {
		t.Fatalf("n wrap -> cursor %d, want 0", m.logMatchCur)
	}
	m = press(m, keyRunes("N")) // wraps back
	if m.logMatchCur != 1 {
		t.Fatalf("N wrap -> cursor %d, want 1", m.logMatchCur)
	}
}

func TestEscUnwindsSearchThenGroupThenFocus(t *testing.T) {
	m := logsModel(logLinesN(10))
	m.logSearch = "line"
	m = press(m, special(tea.KeyEsc))
	if m.logSearch != "" {
		t.Fatal("first Esc should clear the search")
	}
	m.logGrouped = true
	m = press(m, special(tea.KeyEsc))
	if m.logGrouped {
		t.Fatal("second Esc should exit grouping")
	}
	if m.focus != focusDetail {
		t.Fatal("focus should still be detail before the final Esc")
	}
	m = press(m, special(tea.KeyEsc))
	if m.focus != focusSidebar {
		t.Fatal("final Esc should defocus back to the sidebar")
	}
}

func TestScrollPausesFollowAndBottomResumes(t *testing.T) {
	m := logsModel(logLinesN(50))
	m = press(m, keyRunes("k"))
	if m.logFollow {
		t.Fatal("scrolling up should pause follow")
	}
	m = press(m, keyRunes("G"))
	if !m.logFollow {
		t.Fatal("G should resume follow")
	}
	if m.logScroll != m.logMaxTop() {
		t.Fatalf("G should snap to bottom: scroll %d, maxTop %d", m.logScroll, m.logMaxTop())
	}
}

func TestGroupToggle(t *testing.T) {
	m := logsModel([]string{"GET /x/1 200", "GET /x/2 200"})
	m = press(m, keyRunes("p"))
	if !m.logGrouped {
		t.Fatal("p should enable grouping")
	}
	if got := len(m.logDisplayLines()); got != 1 {
		t.Fatalf("grouped display lines = %d, want 1", got)
	}
	m = press(m, keyRunes("p"))
	if m.logGrouped {
		t.Fatal("p should toggle grouping off")
	}
}

func TestCopyKeysYankVisibleAndAll(t *testing.T) {
	captured := stubClipboard(t)
	m := logsModel([]string{"a", "b", "c"})
	m = press(m, keyRunes("Y")) // whole buffer
	if *captured != "a\nb\nc" {
		t.Fatalf("Y clipboard = %q, want whole buffer", *captured)
	}
	if m.toast != "copied 3 lines" {
		t.Fatalf("toast = %q", m.toast)
	}
}
