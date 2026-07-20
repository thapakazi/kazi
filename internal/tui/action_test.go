package tui

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/thapakazi/kazi/internal/engine"
)

// recordingEngine captures Remove calls for the action tests.
type recordingEngine struct {
	fakeEngine
	removed *[]string
	err     error
}

func (r recordingEngine) Remove(name string) error {
	*r.removed = append(*r.removed, name)
	return r.err
}

// loadedWith returns a snapshot-applied model over the given engine.
func loadedWith(t *testing.T, eng Engine) Model {
	t.Helper()
	m := New(eng, time.Second)
	nm, _ := m.Update(snapshotCmd(m.eng)())
	return nm.(Model)
}

// selectStack moves the sidebar selection to the named registered stack row.
func selectStack(t *testing.T, m Model, name string) Model {
	t.Helper()
	for i, r := range m.rows {
		if r.kind == rowStack && r.label == name {
			m.sel = i
			return m
		}
	}
	t.Fatalf("stack %q not in rows", name)
	return m
}

// TestDeleteConfirmDispatches: x opens a confirm modal; y invokes Remove and
// clears the modal.
func TestDeleteConfirmDispatches(t *testing.T) {
	var removed []string
	eng := recordingEngine{removed: &removed}
	m := loadedWith(t, eng)
	m = selectStack(t, m, "blog")

	// x opens the modal (no engine call yet).
	m = press(m, keyRunes("x"))
	if !m.modal.active {
		t.Fatal("x did not open a confirm modal")
	}
	if m.modal.action != actDelete || m.modal.stack != "blog" {
		t.Fatalf("modal = %+v, want delete/blog", m.modal)
	}
	if len(removed) != 0 {
		t.Fatalf("Remove called before confirm: %v", removed)
	}

	// y confirms → dispatch the remove command and run it.
	nm, cmd := m.Update(keyRunes("y"))
	m = nm.(Model)
	if m.modal.active {
		t.Fatal("modal should close on confirm")
	}
	if cmd == nil {
		t.Fatal("confirm produced no command")
	}
	if _, ok := cmd().(actionDoneMsg); !ok {
		t.Fatalf("confirm command did not run the delete")
	}
	if len(removed) != 1 || removed[0] != "blog" {
		t.Fatalf("Remove calls = %v, want [blog]", removed)
	}
}

// TestDeleteCancelDoesNotDispatch: n closes the modal without calling Remove.
func TestDeleteCancelDoesNotDispatch(t *testing.T) {
	var removed []string
	eng := recordingEngine{removed: &removed}
	m := loadedWith(t, eng)
	m = selectStack(t, m, "blog")

	m = press(m, keyRunes("x"))
	nm, _ := m.Update(keyRunes("n"))
	m = nm.(Model)
	if m.modal.active {
		t.Fatal("n should cancel the modal")
	}
	if len(removed) != 0 {
		t.Fatalf("cancel still called Remove: %v", removed)
	}
}

// TestDeleteNotOfferedOnDiscovered: x is inert on a discovered stack (no
// manifest to deregister), so no modal opens.
func TestDeleteNotOfferedOnDiscovered(t *testing.T) {
	m := loaded(t)
	m = selectStack(t, m, "redis") // discovered in the fixture
	if m.selectedRow().selKind != selDiscovered {
		t.Fatalf("redis should be discovered, got %v", m.selectedRow().selKind)
	}
	m = press(m, keyRunes("x"))
	if m.modal.active {
		t.Fatal("x must not open a delete modal on a discovered stack")
	}
}

// TestActionDoneRaisesToast: a successful delete result sets a toast.
func TestActionDoneRaisesToast(t *testing.T) {
	m := loaded(t)
	nm, _ := m.Update(actionDoneMsg{action: "delete", stack: "blog"})
	m = nm.(Model)
	if m.toast == "" {
		t.Fatal("successful action should raise a toast")
	}
}

// lifecycleEngine records up/down/restart dispatches (via ActionStream).
type lifecycleEngine struct {
	fakeEngine
	calls *[]string
}

func (e lifecycleEngine) ActionStream(_ context.Context, action, name string) (io.ReadCloser, <-chan error) {
	*e.calls = append(*e.calls, action+":"+name)
	errc := make(chan error, 1)
	errc <- nil
	return io.NopCloser(strings.NewReader("[+] Running 1/1\n")), errc
}

// TestStackMenuOpens: s opens the quick-actions menu for a selected stack.
func TestStackMenuOpens(t *testing.T) {
	m := selectStack(t, loaded(t), "blog")
	m = press(m, keyRunes("s"))
	if !m.modal.active || m.modal.mkind != modalMenu {
		t.Fatalf("s did not open the stack menu, got %+v", m.modal)
	}
	if m.modal.stack != "blog" {
		t.Fatalf("menu stack = %q, want blog", m.modal.stack)
	}
	// blog is running+registered → down/restart/open present, delete present.
	joined := strings.Join(m.modal.values, ",")
	for _, want := range []string{"down", "restart", "open", "logs", "urls", "config", "delete"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("menu missing %q: %v", want, m.modal.values)
		}
	}
}

// TestStackMenuDispatchesLifecycle: choosing "down" runs the engine verb.
func TestStackMenuDispatchesLifecycle(t *testing.T) {
	var calls []string
	m := loadedWith(t, lifecycleEngine{calls: &calls})
	m = selectStack(t, m, "blog")
	m = press(m, keyRunes("s"))
	// Select "down" by its menu index (order-independent).
	di := -1
	for i, v := range m.modal.values {
		if v == "down" {
			di = i
		}
	}
	if di < 0 {
		t.Fatal("no down entry in menu")
	}
	nm, cmd := m.Update(keyRunes(string(rune('1' + di))))
	m = nm.(Model)
	if m.modal.active {
		t.Fatal("choosing an action should close the menu")
	}
	if cmd == nil {
		t.Fatal("menu choice produced no command")
	}
	cmd() // runs the lifecycle verb
	if len(calls) != 1 || calls[0] != "down:blog" {
		t.Fatalf("calls = %v, want [down:blog]", calls)
	}
}

// TestActionPanelCapturesOutput: dispatching a lifecycle verb opens the Action
// panel and streams captured output into it (never to the terminal).
func TestActionPanelCapturesOutput(t *testing.T) {
	m := selectStack(t, sized(t, 120, 40), "blog")
	m = press(m, keyRunes("s"))
	// Choose "restart" (first item for a running stack).
	nm, cmd := m.Update(keyRunes("1"))
	m = nm.(Model)
	// Run the start command → actionStreamMsg, then pump one line.
	nm, next := m.Update(cmd())
	m = nm.(Model)
	if !m.actionOpen || m.actionTitle == "" {
		t.Fatalf("dispatch should open the Action panel, got title=%q open=%v", m.actionTitle, m.actionOpen)
	}
	// Drain the stream (line then done).
	for next != nil {
		msg := next()
		nm, next = m.Update(msg)
		m = nm.(Model)
	}
	if len(m.actionLines) == 0 {
		t.Fatal("action output should be captured into the panel")
	}
	if m.actionRunning {
		t.Fatal("action should be finished after draining the stream")
	}
	// The panel renders in the frame.
	if !strings.Contains(m.View(), "Action Logs") {
		t.Fatal("Action panel not rendered")
	}
	// ` collapses it (a is now a:adopt).
	before := m.actionPanelHeight()
	m = press(m, keyRunes("`"))
	if m.actionOpen {
		t.Fatal("` should collapse the panel")
	}
	if m.actionPanelHeight() >= before {
		t.Fatal("collapsed panel should be shorter")
	}
}

// TestActionPanelScrolls: with more history than fits, the expanded panel
// scrolls to reveal older lines and clamps at both ends.
func TestActionPanelScrolls(t *testing.T) {
	m := sized(t, 120, 40)
	m.actionTitle = "restart dozzle ✓"
	m.actionOpen = true
	for i := 0; i < 50; i++ {
		m.actionLines = append(m.actionLines, fmt.Sprintf("line %d", i))
	}
	rows := m.actionRows()
	if rows >= len(m.actionLines) {
		t.Skip("fixture doesn't overflow the panel")
	}

	// Bottom-pinned by default: shows the latest lines.
	last := m.actionVisible(rows)
	if last[len(last)-1] != "line 49" {
		t.Fatalf("default view should be bottom-pinned, got %q", last[len(last)-1])
	}

	// PgUp scrolls toward older history.
	m = press(m, special(tea.KeyPgUp))
	if m.actionScroll == 0 {
		t.Fatal("PgUp did not scroll the action history")
	}
	up := m.actionVisible(m.actionRows())
	if up[len(up)-1] == "line 49" {
		t.Fatal("scrolled view should no longer end at the latest line")
	}

	// Scrolling down past the bottom clamps to 0.
	m.scrollAction(-1000)
	if m.actionScroll != 0 {
		t.Fatalf("scroll should clamp to 0 at the bottom, got %d", m.actionScroll)
	}
	// Scrolling up past the top clamps to the max.
	m.scrollAction(10000)
	if want := len(m.actionLines) - m.actionRows(); m.actionScroll != want {
		t.Fatalf("scroll should clamp to %d at the top, got %d", want, m.actionScroll)
	}
}

// TestStackMenuDeleteRoutesToConfirm: choosing "delete" opens the confirm modal.
func TestStackMenuDeleteRoutesToConfirm(t *testing.T) {
	m := selectStack(t, loaded(t), "blog")
	m = press(m, keyRunes("s"))
	// Find the delete index and select it by number.
	di := -1
	for i, v := range m.modal.values {
		if v == "delete" {
			di = i
		}
	}
	if di < 0 {
		t.Fatal("no delete entry in menu")
	}
	nm, _ := m.Update(keyRunes(string(rune('1' + di))))
	m = nm.(Model)
	if !m.modal.active || m.modal.mkind != modalConfirm || m.modal.action != actDelete {
		t.Fatalf("delete should route to a confirm modal, got %+v", m.modal)
	}
}

// TestStackMenuNavigatesToLogs: choosing "logs" focuses the Logs tab.
func TestStackMenuNavigatesToLogs(t *testing.T) {
	m := selectStack(t, loaded(t), "blog")
	m = press(m, keyRunes("s"))
	li := -1
	for i, v := range m.modal.values {
		if v == "logs" {
			li = i
		}
	}
	nm, _ := m.Update(keyRunes(string(rune('1' + li))))
	m = nm.(Model)
	if m.modal.active {
		t.Fatal("navigating should close the menu")
	}
	if m.tab != tabLogs || m.focus != focusDetail {
		t.Fatalf("logs entry should focus the Logs tab, got tab=%v focus=%v", m.tab, m.focus)
	}
}

// captureOpen swaps browserOpen for a recorder for the duration of a test.
func captureOpen(t *testing.T) *[]string {
	t.Helper()
	var opened []string
	prev := browserOpen
	browserOpen = func(u string) error { opened = append(opened, u); return nil }
	t.Cleanup(func() { browserOpen = prev })
	return &opened
}

// multiURLEngine returns two HTTP endpoints for "blog" to exercise the picker.
type multiURLEngine struct{ fakeEngine }

func (multiURLEngine) Urls(_ context.Context, name string) ([]engine.Endpoint, error) {
	if name == "blog" {
		return []engine.Endpoint{
			{Stack: "blog", Service: "web", Kind: "http", URL: "https://blog.localhost"},
			{Stack: "blog", Service: "admin", Kind: "http", URL: "https://admin.blog.localhost"},
			{Stack: "blog", Service: "db", Kind: "tcp", URL: "localhost:42017"},
		}, nil
	}
	return nil, nil
}

// TestOpenSingleURL: o → b on a stack with exactly one HTTP URL opens it
// directly.
func TestOpenSingleURL(t *testing.T) {
	opened := captureOpen(t)
	m := selectStack(t, loaded(t), "blog") // fixture blog has 1 http (web), 1 tcp
	// o opens the transient open menu...
	m = press(m, keyRunes("o"))
	if !m.modal.active || m.modal.mkind != modalOpenChoose {
		t.Fatalf("o should open the open menu, got %+v", m.modal)
	}
	// ...b dispatches the URL resolve command...
	nm, cmd := m.Update(keyRunes("b"))
	m = nm.(Model)
	if cmd == nil {
		t.Fatal("o-b produced no command")
	}
	// ...which yields an openResolvedMsg; a single URL opens directly.
	nm, openCmd := m.Update(cmd())
	m = nm.(Model)
	if m.modal.active {
		t.Fatalf("single URL should not open a picker; modal=%+v", m.modal)
	}
	if openCmd == nil {
		t.Fatal("single URL should dispatch an open command")
	}
	openCmd() // runs browserOpen
	if len(*opened) != 1 || (*opened)[0] != "https://blog.localhost" {
		t.Fatalf("opened = %v, want [https://blog.localhost]", *opened)
	}
}

// TestOpenMultipleURLsPicker: o → b with several HTTP URLs opens a picker;
// Enter opens the highlighted one.
func TestOpenMultipleURLsPicker(t *testing.T) {
	opened := captureOpen(t)
	m := loadedWith(t, multiURLEngine{})
	m = selectStack(t, m, "blog")
	m = press(m, keyRunes("o"))
	nm, cmd := m.Update(keyRunes("b"))
	nm, _ = nm.(Model).Update(cmd())
	m = nm.(Model)
	if !m.modal.active || m.modal.mkind != modalPicker {
		t.Fatalf("multiple URLs should open a picker, got %+v", m.modal)
	}
	if len(m.modal.values) != 2 {
		t.Fatalf("picker should list 2 http URLs, got %d", len(m.modal.values))
	}
	// Move to the second choice and open it.
	m = press(m, keyRunes("j"))
	nm, openC := m.Update(keyRunes("enter"))
	m = nm.(Model)
	if m.modal.active {
		t.Fatal("enter should close the picker")
	}
	openC()
	if len(*opened) != 1 || (*opened)[0] != "https://admin.blog.localhost" {
		t.Fatalf("opened = %v, want [https://admin.blog.localhost]", *opened)
	}
}

// TestOpenNoURL: resolving zero HTTP URLs raises a toast, opens nothing.
func TestOpenNoURL(t *testing.T) {
	opened := captureOpen(t)
	m := loaded(t)
	nm, _ := m.Update(openResolvedMsg{stack: "api", choices: nil})
	m = nm.(Model)
	if m.toast == "" {
		t.Fatal("no-URL open should raise a toast")
	}
	if len(*opened) != 0 {
		t.Fatalf("no-URL open should not launch a browser: %v", *opened)
	}
}

var _ tea.Msg = actionDoneMsg{}
