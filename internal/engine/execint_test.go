//go:build integration

package engine

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/thapakazi/kazi/internal/runtime"
	"github.com/thapakazi/kazi/internal/store"
)

// TestExecIntegration drives real `<runtime> compose exec` end to end: capture
// output + exit-code passthrough, resolution, and the not-running/absent errors.
// Run with: go test -tags integration ./internal/engine/ -v -run ExecIntegration
func TestExecIntegration(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not on PATH")
	}
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())

	rt, err := runtime.Detect("docker")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := store.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	e := New(rt, cfg, os.Stdout, os.Stderr)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	fixture, err := filepath.Abs("testdata/fixture") // single service "app" (alpine sleep)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Add("iexec", fixture); err != nil {
		t.Fatal(err)
	}
	defer e.Down(ctx, "iexec")
	if err := e.Up(ctx, "iexec"); err != nil {
		t.Fatal(err)
	}

	// Capture: stdout + exit 0.
	res, err := e.Exec(ctx, "iexec", "app", []string{"echo", "hi"}, ExecOpts{})
	if err != nil {
		t.Fatalf("exec echo: %v", err)
	}
	if res.ExitCode != 0 || strings.TrimSpace(res.Stdout) != "hi" {
		t.Errorf("echo result = %+v, want exit 0 stdout hi", res)
	}

	// Exit-code passthrough: `false` exits 1, and that is NOT a kazi error.
	res, err = e.Exec(ctx, "iexec", "app", []string{"false"}, ExecOpts{})
	if err != nil {
		t.Errorf("non-zero command should not be a kazi error: %v", err)
	}
	if res.ExitCode != 1 {
		t.Errorf("false exit code = %d, want 1", res.ExitCode)
	}

	// Resolution returns a real container id.
	id, err := e.ResolveContainer(ctx, "iexec", "app", 1)
	if err != nil || id == "" {
		t.Errorf("ResolveContainer = %q err %v", id, err)
	}

	// Unknown service ⇒ ErrServiceNotFound.
	if _, err := e.Exec(ctx, "iexec", "ghost", []string{"true"}, ExecOpts{}); !errors.Is(err, ErrServiceNotFound) {
		t.Errorf("unknown service err = %v, want ErrServiceNotFound", err)
	}

	// Interactive command constructs a runnable `compose exec` (smoke, no PTY).
	c, err := e.ExecCommand(ctx, "iexec", "app", nil, ExecOpts{})
	if err != nil {
		t.Fatalf("ExecCommand: %v", err)
	}
	if !strings.Contains(strings.Join(c.Args, " "), "exec") {
		t.Errorf("interactive argv missing exec: %v", c.Args)
	}

	// Not running: after down, the service resolves to a clean not-running error.
	if err := e.Down(ctx, "iexec"); err != nil {
		t.Fatal(err)
	}
	_, err = e.Exec(ctx, "iexec", "app", []string{"true"}, ExecOpts{})
	if !errors.Is(err, ErrServiceNotRunning) && !errors.Is(err, ErrServiceNotFound) {
		t.Errorf("exec after down err = %v, want not-running/not-found", err)
	}
}
