package engine

import (
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/thapakazi/kazi/internal/runtime"
)

func TestUpRegisteredInjectsLabels(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	blogDir := registerStack(t, "blog")
	fake := &runtime.Fake{Services: []string{"web", "db"}}
	e := New(fake, io.Discard, io.Discard)

	if err := e.Up(t.Context(), "blog"); err != nil {
		t.Fatal(err)
	}
	if len(fake.Calls) != 2 {
		t.Fatalf("calls = %v", fake.Calls)
	}
	// call 0: config --services against the manifest's compose file
	cfg := fake.Calls[0]
	if cfg[0] != "kazi-blog" || cfg[1] != blogDir || !slices.Contains(cfg, "config") {
		t.Errorf("config call = %v", cfg)
	}
	// call 1: up -d with original file + override file
	up := fake.Calls[1]
	if up[0] != "kazi-blog" || !slices.Contains(up, "up") || !slices.Contains(up, "-d") {
		t.Errorf("up call = %v", up)
	}
	var override string
	for _, a := range up[2:] {
		if strings.Contains(a, "kazi-override") {
			override = a
		}
	}
	if override == "" {
		t.Fatalf("no override file in up call: %v", up)
	}
	// override was cleaned up after Up returned
	if _, err := os.Stat(override); !os.IsNotExist(err) {
		t.Errorf("override %s not cleaned up", override)
	}
}

func TestUpReusesExistingProjectName(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	blogDir := registerStack(t, "blog")
	fake := &runtime.Fake{
		Services: []string{"web"},
		Containers: []runtime.Container{
			container("blog-web-1", "handrolled", blogDir, "web", "running", "Up 1 hour"),
		},
	}
	e := New(fake, io.Discard, io.Discard)
	if err := e.Up(t.Context(), "blog"); err != nil {
		t.Fatal(err)
	}
	for _, call := range fake.Calls {
		if call[0] != "handrolled" {
			t.Errorf("call used project %q, want handrolled: %v", call[0], call)
		}
	}
}

func TestDownDiscoveredStack(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	fake := &runtime.Fake{Containers: []runtime.Container{
		container("legacy-db-1", "legacy", "/srv/legacy", "db", "running", "Up 2 days"),
	}}
	e := New(fake, io.Discard, io.Discard)
	if err := e.Down(t.Context(), "legacy"); err != nil {
		t.Fatal(err)
	}
	down := fake.Calls[0]
	if down[0] != "legacy" || down[1] != "/srv/legacy" || !slices.Contains(down, "down") {
		t.Errorf("down call = %v", down)
	}
	if slices.Contains(down, "-v") {
		t.Errorf("down must never pass -v: %v", down)
	}
}

func TestLogsFlags(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	blogDir := registerStack(t, "blog")
	fake := &runtime.Fake{}
	e := New(fake, io.Discard, io.Discard)
	if err := e.Logs(t.Context(), "blog", "web", true, "50"); err != nil {
		t.Fatal(err)
	}
	logs := fake.Calls[0]
	for _, want := range []string{"logs", "-f", "--tail", "50", "web"} {
		if !slices.Contains(logs, want) {
			t.Errorf("logs call missing %q: %v", want, logs)
		}
	}
	composePath := filepath.Join(blogDir, "docker-compose.yml")
	if !slices.Contains(logs, composePath) {
		t.Errorf("logs call missing compose file path %q: %v", composePath, logs)
	}
}

func TestRestartRegisteredStack(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	blogDir := registerStack(t, "blog")
	fake := &runtime.Fake{}
	e := New(fake, io.Discard, io.Discard)
	if err := e.Restart(t.Context(), "blog"); err != nil {
		t.Fatal(err)
	}
	if len(fake.Calls) != 1 {
		t.Fatalf("want 1 call, got %d: %v", len(fake.Calls), fake.Calls)
	}
	call := fake.Calls[0]
	if call[0] != "kazi-blog" {
		t.Errorf("project = %q, want kazi-blog", call[0])
	}
	if !slices.Contains(call, "restart") {
		t.Errorf("restart call missing \"restart\": %v", call)
	}
	composePath := filepath.Join(blogDir, "docker-compose.yml")
	if !slices.Contains(call, composePath) {
		t.Errorf("restart call missing compose file path %q: %v", composePath, call)
	}
}

func TestUpMissingComposePath(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	dir := registerStack(t, "blog")
	os.Remove(dir + "/docker-compose.yml")
	e := New(&runtime.Fake{}, io.Discard, io.Discard)
	err := e.Up(t.Context(), "blog")
	if err == nil || !strings.Contains(err.Error(), "no longer exists") {
		t.Errorf("want actionable missing-path error, got %v", err)
	}
}
