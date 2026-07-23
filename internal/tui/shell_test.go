package tui

import (
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/thapakazi/kazi/internal/engine"
)

func hasToken(items []menuItem, tok string) bool {
	for _, it := range items {
		if it.token == tok {
			return true
		}
	}
	return false
}

// shell is offered only for running, non-system stacks.
func TestStackMenuShellGating(t *testing.T) {
	if !hasToken(stackMenuItems(true, true, false), "shell") {
		t.Error("running registered stack should offer shell")
	}
	if hasToken(stackMenuItems(false, true, false), "shell") {
		t.Error("stopped stack should not offer shell")
	}
	if hasToken(stackMenuItems(true, true, true), "shell") {
		t.Error("system stack should not offer shell")
	}
}

func TestShellJoin(t *testing.T) {
	// $(...) and pipes must be single-quoted so the host shell keeps them literal.
	got := shellJoin([]string{"docker", "exec", "-it", "c", "/bin/sh", "-c", "eval $(id -un)"})
	want := `'docker' 'exec' '-it' 'c' '/bin/sh' '-c' 'eval $(id -un)'`
	if got != want {
		t.Errorf("shellJoin = %s\nwant %s", got, want)
	}
	// An embedded single quote is escaped as '\''.
	if q := shellJoin([]string{"a'b"}); q != `'a'\''b'` {
		t.Errorf("single-quote escape = %s", q)
	}
}

func TestWrapReturnPause(t *testing.T) {
	base := exec.Command("docker", "exec", "-it", "c", "sh")
	base.Env = []string{"FOO=bar"}

	// No pause ⇒ unchanged command.
	if got := wrapReturnPause(base, false); got != base {
		t.Error("pause=false should return the original cmd")
	}
	// Pause ⇒ sh -c wrapper preserving Env, and running the quoted inner argv.
	w := wrapReturnPause(base, true)
	if w.Args[0] != "sh" || w.Args[1] != "-c" {
		t.Errorf("wrapper argv = %v, want sh -c ...", w.Args[:2])
	}
	if !reflect.DeepEqual(w.Env, base.Env) {
		t.Errorf("wrapper Env = %v, want %v", w.Env, base.Env)
	}
	if !strings.Contains(w.Args[2], `'docker' 'exec'`) || !strings.Contains(w.Args[2], "read __kazi_x") {
		t.Errorf("wrapper script missing inner cmd or pause: %s", w.Args[2])
	}
}

func TestRunningShellTargets(t *testing.T) {
	st := engine.StackInfo{Containers: []engine.ContainerInfo{
		{Name: "blog-web-1", Service: "web", State: "running"},
		{Name: "blog-web-2", Service: "web", State: "running"}, // dedup by service
		{Name: "blog-db-1", Service: "db", State: "exited"},     // skip stopped
		{Name: "adopted", Service: "", State: "running"},        // fall back to name
	}}
	got := runningShellTargets(st)
	want := []string{"web", "adopted"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("runningShellTargets = %v, want %v", got, want)
	}
}

// The shell picker must render as a list (its services + "shell" verb), not the
// y/n confirm body — regression for modalShellChoose missing from renderModal.
func TestShellPickerRendersAsList(t *testing.T) {
	m := New(fakeEngine{}, time.Second)
	m.modal = modalState{active: true, mkind: modalShellChoose, stack: "draw",
		prompt: "› draw — shell", options: []string{"frontend", "backend"}, values: []string{"frontend", "backend"}}
	out := m.renderModal()
	if !strings.Contains(out, "frontend") || !strings.Contains(out, "backend") {
		t.Errorf("picker should list services, got:\n%s", out)
	}
	if strings.Contains(out, "confirm") {
		t.Errorf("shell picker rendered as a y/n confirm:\n%s", out)
	}
	if !strings.Contains(out, "shell") {
		t.Errorf("picker should show the shell verb:\n%s", out)
	}
}

// recordShell substitutes the suspend with a recorder for the test's duration.
func recordShell(t *testing.T) *struct {
	called          bool
	stack, service  string
	pause           bool
} {
	t.Helper()
	rec := &struct {
		called         bool
		stack, service string
		pause          bool
	}{}
	prev := shellCmd
	shellCmd = func(_ *exec.Cmd, stack, service string, pause bool) tea.Cmd {
		rec.called, rec.stack, rec.service, rec.pause = true, stack, service, pause
		return nil
	}
	t.Cleanup(func() { shellCmd = prev })
	return rec
}

// One running service ⇒ suspend straight in, with the return pause on.
func TestShellTargetsSingleSuspends(t *testing.T) {
	rec := recordShell(t)
	m := New(fakeEngine{}, time.Second)
	m.Update(shellTargetsMsg{stack: "blog", services: []string{"web"}})
	if !rec.called || rec.stack != "blog" || rec.service != "web" || !rec.pause {
		t.Errorf("recorder = %+v, want blog/web pause=true", rec)
	}
}

// returnImmediately config ⇒ no pause.
func TestShellReturnImmediately(t *testing.T) {
	rec := recordShell(t)
	m := New(fakeEngine{}, time.Second, WithReturnImmediately(true))
	m.Update(shellTargetsMsg{stack: "blog", services: []string{"web"}})
	if !rec.called || rec.pause {
		t.Errorf("recorder pause = %v, want false", rec.pause)
	}
}

// Multiple running services ⇒ open the transient picker, no immediate suspend.
func TestShellTargetsMultiPicker(t *testing.T) {
	rec := recordShell(t)
	m := New(fakeEngine{}, time.Second)
	next, _ := m.Update(shellTargetsMsg{stack: "blog", services: []string{"web", "db"}})
	nm := next.(Model)
	if rec.called {
		t.Error("multi-service should not suspend before a pick")
	}
	if !nm.modal.active || nm.modal.mkind != modalShellChoose {
		t.Errorf("modal = %+v, want active modalShellChoose", nm.modal)
	}
	if !reflect.DeepEqual(nm.modal.values, []string{"web", "db"}) {
		t.Errorf("picker values = %v", nm.modal.values)
	}

	// Choosing db from the picker suspends into it.
	dm, _ := nm.shellChoose(1)
	if !rec.called || rec.service != "db" {
		t.Errorf("shellChoose ⇒ %+v, want db", rec)
	}
	if dm.(Model).modal.active {
		t.Error("picker should close after choosing")
	}
}
