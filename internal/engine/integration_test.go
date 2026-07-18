//go:build integration

package engine

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/thapakazi/kazi/internal/runtime"
)

// TestComposeLifecycle drives a real compose project end to end against
// Docker: add -> up -> status/ps (labels, discovery) -> down.
// Run with: go test -tags integration ./internal/engine/ -v -run Lifecycle
func TestComposeLifecycle(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not on PATH")
	}
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())

	rt, err := runtime.Detect("docker")
	if err != nil {
		t.Fatal(err)
	}
	e := New(rt, os.Stdout, os.Stderr)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	fixture, err := filepath.Abs("testdata/fixture")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Add("itest", fixture); err != nil {
		t.Fatal(err)
	}
	defer e.Down(ctx, "itest") // cleanup even on failure

	if err := e.Up(ctx, "itest"); err != nil {
		t.Fatal(err)
	}
	st, err := e.Status(ctx, "itest")
	if err != nil {
		t.Fatal(err)
	}
	if st.Kind != KindRegistered || st.Running < 1 {
		t.Errorf("status = %+v", st)
	}

	// kazi labels were injected via the override file.
	cs, err := e.Ps(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range cs {
		if c.Stack == "itest" && c.Kind == KindRegistered {
			found = true
		}
	}
	if !found {
		t.Errorf("itest container not grouped as registered: %+v", cs)
	}

	if err := e.Down(ctx, "itest"); err != nil {
		t.Fatal(err)
	}
}
