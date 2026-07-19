package engine

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/thapakazi/kazi/internal/runtime"
)

// minimalConfigJSON is a minimal compose config JSON for tests that don't
// need full routing logic (web service only on default network).
const minimalConfigJSON = `{"services":{"web":{"networks":{"default":null}}}}`

func TestUpRegisteredInjectsLabels(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	blogDir := registerStack(t, "blog")
	fake := &runtime.Fake{ConfigJSON: minimalConfigJSON}
	e := testEngine(t, fake)

	if err := e.Up(t.Context(), "blog"); err != nil {
		t.Fatal(err)
	}
	if len(fake.Calls) < 2 {
		t.Fatalf("calls = %v", fake.Calls)
	}
	// call 0: config --format json against the manifest's compose file
	cfg := fake.Calls[0]
	if cfg[0] != "kazi-blog" || cfg[1] != blogDir || !slices.Contains(cfg, "config") || !slices.Contains(cfg, "--format") || !slices.Contains(cfg, "json") {
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
		ConfigJSON: minimalConfigJSON,
		Containers: []runtime.Container{
			container("blog-web-1", "handrolled", blogDir, "web", "running", "Up 1 hour"),
		},
	}
	e := testEngine(t, fake)
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
	e := testEngine(t, fake)
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
	e := testEngine(t, fake)
	if err := e.Logs(t.Context(), "blog", "web", true, "50", "5m"); err != nil {
		t.Fatal(err)
	}
	logs := fake.Calls[0]
	for _, want := range []string{"logs", "-f", "--tail", "50", "--since", "5m", "web"} {
		if !slices.Contains(logs, want) {
			t.Errorf("logs call missing %q: %v", want, logs)
		}
	}
	composePath := filepath.Join(blogDir, "docker-compose.yml")
	if !slices.Contains(logs, composePath) {
		t.Errorf("logs call missing compose file path %q: %v", composePath, logs)
	}
}

func TestLogsSinceOmittedWhenEmpty(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	registerStack(t, "blog")
	fake := &runtime.Fake{}
	e := testEngine(t, fake)
	if err := e.Logs(t.Context(), "blog", "", true, "", ""); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(fake.Calls[0], "--since") {
		t.Errorf("empty since should omit --since: %v", fake.Calls[0])
	}
}

func TestRestartRegisteredStack(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	blogDir := registerStack(t, "blog")
	fake := &runtime.Fake{}
	e := testEngine(t, fake)
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
	e := testEngine(t, &runtime.Fake{})
	err := e.Up(t.Context(), "blog")
	if err == nil || !strings.Contains(err.Error(), "no longer exists") {
		t.Errorf("want actionable missing-path error, got %v", err)
	}
}
