package engine

import (
	"errors"
	"testing"

	"github.com/thapakazi/kazi/internal/runtime"
)

func execImageEngine(t *testing.T, f *runtime.Fake) *Engine {
	t.Helper()
	registerImageStack(t, "cache", "redis:7", false, nil, nil, nil)
	if f.Containers == nil {
		f.Containers = []runtime.Container{{
			ID: "redis1", Name: "cache", State: "running", Status: "Up",
			Labels: map[string]string{"kazi.managed": "true", "kazi.stack": "cache"},
		}}
	}
	return testEngine(t, f)
}

// A command that exits 0 with output ⇒ ExecResult{0, stdout, ...}, nil error.
// Default capture allocates no TTY (no -i/-t in the argv).
func TestExecCapturesOutput(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	f := &runtime.Fake{CmdOut: map[string]string{"exec": "hi"}}
	e := execImageEngine(t, f)

	res, err := e.Exec(t.Context(), "cache", "cache", []string{"echo", "hi"}, ExecOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 0 || res.Stdout != "hi\n" {
		t.Errorf("res = %+v, want exit 0 stdout %q", res, "hi\n")
	}
	last := lastCall(f.Cmds)
	for _, a := range last {
		if a == "-t" || a == "-i" {
			t.Errorf("capture should not allocate a TTY, argv = %v", last)
		}
	}
}

// A command that exits non-zero is passthrough, NOT a kazi error: the exit code
// lands in ExecResult and err is nil so scripts read the command's status.
func TestExecExitCodePassthrough(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	f := &runtime.Fake{FailPrefix: []string{"exec"}}
	e := execImageEngine(t, f)

	res, err := e.Exec(t.Context(), "cache", "cache", []string{"false"}, ExecOpts{})
	if err != nil {
		t.Errorf("non-zero command should not be a kazi error, got %v", err)
	}
	if res.ExitCode != 1 {
		t.Errorf("exit code = %d, want 1", res.ExitCode)
	}
}

// A pre-exec failure (unknown service) is a kazi error with a zero ExecResult —
// distinct from a command that ran and exited non-zero.
func TestExecPreExecError(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	f := &runtime.Fake{}
	e := execImageEngine(t, f)

	res, err := e.Exec(t.Context(), "cache", "ghost", []string{"echo", "x"}, ExecOpts{})
	if !errors.Is(err, ErrServiceNotFound) {
		t.Errorf("err = %v, want ErrServiceNotFound", err)
	}
	if res.ExitCode != 0 || res.Stdout != "" {
		t.Errorf("pre-exec ExecResult should be zero, got %+v", res)
	}
}

// opts.TTY opts capture into a TTY (-t) for the rare interactive-capture case.
func TestExecTTYOptIn(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	f := &runtime.Fake{}
	e := execImageEngine(t, f)

	if _, err := e.Exec(t.Context(), "cache", "cache", []string{"top"}, ExecOpts{TTY: true}); err != nil {
		t.Fatal(err)
	}
	var sawT bool
	for _, a := range lastCall(f.Cmds) {
		if a == "-t" {
			sawT = true
		}
	}
	if !sawT {
		t.Errorf("opts.TTY should add -t, argv = %v", lastCall(f.Cmds))
	}
}
