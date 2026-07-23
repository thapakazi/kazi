package engine

import (
	"reflect"
	"testing"

	"github.com/thapakazi/kazi/internal/runtime"
)

// composeExecArgs extracts the exec-onward args from a recorded ComposeCmd call
// ({project, dir, files..., args...}).
func composeExecArgs(t *testing.T, call []string) []string {
	t.Helper()
	for i, a := range call {
		if a == "exec" {
			return call[i:]
		}
	}
	t.Fatalf("no exec token in compose call %v", call)
	return nil
}

func lastCall(calls [][]string) []string { return calls[len(calls)-1] }

// Compose stack, no argv, no shell ⇒ login-shell probe, interactive -i -t,
// index 1 omits --index.
func TestExecCommandComposeProbe(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	dir := registerStack(t, "blog")
	f := &runtime.Fake{ConfigJSON: blogConfigJSON, Containers: []runtime.Container{
		composeContainer(dir, "web", "1", "web1", "running"),
	}}
	e := testEngine(t, f)

	if _, err := e.ExecCommand(t.Context(), "blog", "web", nil, ExecOpts{}); err != nil {
		t.Fatal(err)
	}
	got := composeExecArgs(t, lastCall(f.Calls))
	want := []string{"exec", "-i", "-t", "web", "/bin/sh", "-c", loginShellProbe}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("compose probe argv =\n  %v\nwant\n  %v", got, want)
	}
}

// --shell + --index + --user/--workdir/-e are all threaded, in a stable order.
func TestExecCommandComposeFlags(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	dir := registerStack(t, "blog")
	f := &runtime.Fake{ConfigJSON: blogConfigJSON, Containers: []runtime.Container{
		composeContainer(dir, "web", "1", "web1", "running"),
		composeContainer(dir, "web", "2", "web2", "running"),
	}}
	e := testEngine(t, f)

	opts := ExecOpts{Shell: "/bin/bash", Index: 2, User: "root", Workdir: "/app", Env: []string{"FOO=bar"}}
	if _, err := e.ExecCommand(t.Context(), "blog", "web", nil, opts); err != nil {
		t.Fatal(err)
	}
	got := composeExecArgs(t, lastCall(f.Calls))
	want := []string{"exec", "--index", "2", "-i", "-t", "--user", "root", "--workdir", "/app", "--env", "FOO=bar", "web", "/bin/bash"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("compose flags argv =\n  %v\nwant\n  %v", got, want)
	}
}

// Compose capture (Exec) must add -T to disable compose's default pseudo-TTY;
// interactive (ExecCommand, -t) must not.
func TestExecComposeCaptureDisablesTTY(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	dir := registerStack(t, "blog")
	newFake := func() *runtime.Fake {
		return &runtime.Fake{ConfigJSON: blogConfigJSON, Containers: []runtime.Container{
			composeContainer(dir, "web", "1", "web1", "running"),
		}}
	}

	// Capture path: -T present, no -t.
	f := newFake()
	e := testEngine(t, f)
	if _, err := e.Exec(t.Context(), "blog", "web", []string{"echo", "hi"}, ExecOpts{}); err != nil {
		t.Fatal(err)
	}
	got := composeExecArgs(t, lastCall(f.Calls))
	if got[1] != "-T" {
		t.Errorf("compose capture argv = %v, want -T after exec", got)
	}
	for _, a := range got {
		if a == "-t" {
			t.Errorf("capture should not allocate a TTY: %v", got)
		}
	}

	// Interactive path: no -T.
	f2 := newFake()
	e2 := testEngine(t, f2)
	if _, err := e2.ExecCommand(t.Context(), "blog", "web", nil, ExecOpts{}); err != nil {
		t.Fatal(err)
	}
	for _, a := range composeExecArgs(t, lastCall(f2.Calls)) {
		if a == "-T" {
			t.Error("interactive compose exec should not pass -T")
		}
	}
}

// Explicit argv (-- cmd) runs verbatim, no shell wrapper, even if a shell is set.
func TestExecCommandComposeArgvWins(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	dir := registerStack(t, "blog")
	f := &runtime.Fake{ConfigJSON: blogConfigJSON, Containers: []runtime.Container{
		composeContainer(dir, "db", "1", "db1", "running"),
	}}
	e := testEngine(t, f)

	opts := ExecOpts{Shell: "/bin/bash"} // ignored because argv is explicit
	if _, err := e.ExecCommand(t.Context(), "blog", "db", []string{"pg_isready", "-q"}, opts); err != nil {
		t.Fatal(err)
	}
	got := composeExecArgs(t, lastCall(f.Calls))
	want := []string{"exec", "-i", "-t", "db", "pg_isready", "-q"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("argv-wins argv =\n  %v\nwant\n  %v", got, want)
	}
}

// Image stack ⇒ direct `<runtime> exec <container>`, recorded via Cmd not ComposeCmd.
func TestExecCommandImageDirect(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	registerImageStack(t, "cache", "redis:7", false, nil, nil, nil)
	f := &runtime.Fake{Containers: []runtime.Container{
		{ID: "redis1", Name: "cache", State: "running", Status: "Up", Labels: map[string]string{
			"kazi.managed": "true", "kazi.stack": "cache",
		}},
	}}
	e := testEngine(t, f)

	if _, err := e.ExecCommand(t.Context(), "cache", "cache", nil, ExecOpts{}); err != nil {
		t.Fatal(err)
	}
	if len(f.Calls) != 0 {
		t.Errorf("image path should not use ComposeCmd, got %v", f.Calls)
	}
	got := lastCall(f.Cmds)
	want := []string{"exec", "-i", "-t", "redis1", "/bin/sh", "-c", loginShellProbe}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("image direct argv =\n  %v\nwant\n  %v", got, want)
	}
}

// spec.exec.shell is the fallback shell when neither --shell nor argv is given.
func TestExecCommandConfigShellFallback(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	dir := registerStack(t, "blog")
	f := &runtime.Fake{ConfigJSON: blogConfigJSON, Containers: []runtime.Container{
		composeContainer(dir, "web", "1", "web1", "running"),
	}}
	e := testEngine(t, f)
	e.Cfg.Spec.Exec.Shell = "/bin/zsh"

	if _, err := e.ExecCommand(t.Context(), "blog", "web", nil, ExecOpts{}); err != nil {
		t.Fatal(err)
	}
	got := composeExecArgs(t, lastCall(f.Calls))
	want := []string{"exec", "-i", "-t", "web", "/bin/zsh"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("config-shell argv =\n  %v\nwant\n  %v", got, want)
	}
}
