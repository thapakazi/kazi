package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// onEnvFor parks a loaded model on the Env tab for a real sidebar stack, with
// its environment fetched through the same command the app uses.
func onEnvFor(t *testing.T, name string) Model {
	t.Helper()
	m := selectStack(t, loaded(t), name)
	m.tab = tabEnv
	m.focus = focusDetail
	nm, _ := m.Update(envCmd(m.eng, name)())
	return nm.(Model)
}

func TestEnvTabRendersPerContainer(t *testing.T) {
	m := onEnvFor(t, "blog")
	if m.envFor != "blog" {
		t.Fatalf("envFor = %q, want blog", m.envFor)
	}
	out := m.renderEnv(80)
	for _, want := range []string{"web", "db", "SVC=web", "SVC=db", "containers:2"} {
		if !strings.Contains(out, want) {
			t.Fatalf("env render missing %q in:\n%s", want, out)
		}
	}
}

func TestEnvContainerFilter(t *testing.T) {
	m := onEnvFor(t, "blog")

	// c opens the same "all services + one per service" picker as Logs.
	m = press(m, keyRunes("c"))
	if !m.modal.active || m.modal.mkind != modalEnvService {
		t.Fatalf("c should open the env container picker, got %+v", m.modal)
	}
	if got := strings.Join(m.modal.options, ","); got != "all services,db,web" {
		t.Fatalf("picker options = %q", got)
	}

	// Number-select "db" (option 2) → view narrows to that container.
	m = press(m, keyRunes("2"))
	if m.envService != "db" {
		t.Fatalf("envService = %q, want db", m.envService)
	}
	out := m.renderEnv(80)
	if !strings.Contains(out, "SVC=db") || strings.Contains(out, "SVC=web") {
		t.Fatalf("filtered env should show only db:\n%s", out)
	}
	if !strings.Contains(m.envControlStrip(), "svc:db") {
		t.Fatalf("control strip = %q, want svc:db", m.envControlStrip())
	}

	// Esc clears the filter back to all containers.
	m = press(m, special(tea.KeyEsc))
	if m.envService != "" {
		t.Fatalf("Esc should clear the env filter, got %q", m.envService)
	}
}

func TestEnvSearchHighlightsAndNavigates(t *testing.T) {
	m := onEnvFor(t, "blog")

	// / enters incremental search; typed runes build the query live.
	m = press(m, keyRunes("/"))
	if !m.envSearching {
		t.Fatal("/ should enter env search mode")
	}
	for _, r := range "SVC" {
		m = press(m, keyRunes(string(r)))
	}
	// Both containers have an "SVC=" line → two matches.
	if got := len(m.envMatchIndices()); got != 2 {
		t.Fatalf("matches = %d, want 2", got)
	}
	if !strings.Contains(m.envControlStrip(), "/SVC") {
		t.Fatalf("control strip should show the live query, got %q", m.envControlStrip())
	}

	// Enter locks the search and reveals the first match.
	m = press(m, special(tea.KeyEnter))
	if m.envSearching {
		t.Fatal("Enter should lock the search")
	}
	if !strings.Contains(m.envControlStrip(), "(1/2)") {
		t.Fatalf("strip should show match position, got %q", m.envControlStrip())
	}
	// The rendered viewport highlights the current match.
	if !strings.Contains(m.renderEnv(80), logMatchMarker) {
		t.Fatal("current match should be marked in the render")
	}

	// n advances the match cursor; wraps within the set.
	m = press(m, keyRunes("n"))
	if !strings.Contains(m.envControlStrip(), "(2/2)") {
		t.Fatalf("n should advance to 2/2, got %q", m.envControlStrip())
	}

	// Esc clears search first (before the container filter / defocus).
	m = press(m, special(tea.KeyEsc))
	if m.envSearch != "" {
		t.Fatalf("Esc should clear the search, got %q", m.envSearch)
	}
}

func TestEnvCachedUntilActionInvalidates(t *testing.T) {
	m := onEnvFor(t, "blog")
	// Env is immutable per container lifetime, so a cached stack skips refetch.
	if cmd := m.detailReadCmd(); cmd != nil {
		t.Fatal("Env read should be cached (nil cmd) once loaded")
	}
	// A lifecycle action recreates containers → cache invalidated → refetch.
	nm, _ := m.Update(actionDoneMsg{action: "restart", stack: "blog"})
	m = nm.(Model)
	if m.envFor != "" {
		t.Fatalf("actionDone should invalidate envFor, got %q", m.envFor)
	}
	if cmd := m.detailReadCmd(); cmd == nil {
		t.Fatal("Env read should refetch after invalidation")
	}
}
