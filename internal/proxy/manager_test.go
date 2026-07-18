package proxy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thapakazi/kazi/internal/runtime"
)

func TestEnsureNetworkCreatesWhenMissing(t *testing.T) {
	f := &runtime.Fake{FailPrefix: []string{"network inspect"}}
	if err := EnsureNetwork(t.Context(), f); err != nil {
		t.Fatal(err)
	}
	if len(f.Cmds) != 2 || strings.Join(f.Cmds[1], " ") != "network create kazi" {
		t.Errorf("cmds = %v", f.Cmds)
	}
	// existing network ⇒ no create
	f2 := &runtime.Fake{}
	EnsureNetwork(t.Context(), f2)
	if len(f2.Cmds) != 1 {
		t.Errorf("cmds = %v", f2.Cmds)
	}
}

func TestSyncWritesValidatesReloads(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	f := &runtime.Fake{}
	routes := []Route{{Stack: "blog", Service: "web", Hostname: "blog.localhost", Alias: "web.blog", Port: 80}}
	if err := Sync(t.Context(), f, routes, false); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(Dir(), "Caddyfile"))
	if err != nil || !strings.Contains(string(b), "blog.localhost") {
		t.Errorf("caddyfile: %s err=%v", b, err)
	}
	joined := ""
	for _, c := range f.Cmds {
		joined += strings.Join(c, " ") + "\n"
	}
	if !strings.Contains(joined, "caddy validate") || !strings.Contains(joined, "caddy reload") {
		t.Errorf("missing validate/reload in:\n%s", joined)
	}
	// proxy wasn't running ⇒ compose up recorded
	upSeen := false
	for _, c := range f.Calls {
		if c[0] == StackName && strings.Contains(strings.Join(c, " "), "up -d") {
			upSeen = true
		}
	}
	if !upSeen {
		t.Errorf("proxy compose up missing: %v", f.Calls)
	}
	if _, err := os.Stat(filepath.Join(Dir(), "compose.yml")); err != nil {
		t.Error("compose.yml not written")
	}
}

func TestSyncNoChangeNoReload(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	f := &runtime.Fake{}
	routes := []Route{{Stack: "blog", Service: "web", Hostname: "blog.localhost", Alias: "web.blog", Port: 80}}
	if err := Sync(t.Context(), f, routes, false); err != nil {
		t.Fatal(err)
	}
	f2 := &runtime.Fake{}
	if err := Sync(t.Context(), f2, routes, true); err != nil { // same routes, proxy now running
		t.Fatal(err)
	}
	if len(f2.Cmds) != 0 || len(f2.Calls) != 0 {
		t.Errorf("unchanged content must be a no-op: cmds=%v calls=%v", f2.Cmds, f2.Calls)
	}
}

func TestSyncValidateFailureKeepsOldConfig(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	f := &runtime.Fake{}
	old := []Route{{Stack: "a", Service: "w", Hostname: "a.localhost", Alias: "w.a", Port: 80}}
	if err := Sync(t.Context(), f, old, false); err != nil {
		t.Fatal(err)
	}
	bad := &runtime.Fake{FailPrefix: []string{"exec kazi-proxy caddy validate"}}
	next := append(old, Route{Stack: "b", Service: "w", Hostname: "b.localhost", Alias: "w.b", Port: 80})
	if err := Sync(t.Context(), bad, next, true); err == nil {
		t.Fatal("want validate error")
	}
	b, _ := os.ReadFile(filepath.Join(Dir(), "Caddyfile"))
	if strings.Contains(string(b), "b.localhost") {
		t.Errorf("old config must keep serving after failed validate:\n%s", b)
	}
}

func TestSyncNoRoutesIdleProxyStaysDown(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	f := &runtime.Fake{}
	// Pre-write a sentinel compose.yml to verify it is NOT overwritten.
	composeFile := filepath.Join(Dir(), "compose.yml")
	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		t.Fatal(err)
	}
	sentinel := "# sentinel — must not be overwritten\n"
	if err := os.WriteFile(composeFile, []byte(sentinel), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Sync(t.Context(), f, nil, false); err != nil {
		t.Fatal(err)
	}
	if len(f.Calls) != 0 {
		t.Errorf("no routes + proxy down ⇒ never start it: %v", f.Calls)
	}
	// compose.yml must not have been overwritten by EnsureStackFiles.
	got, err := os.ReadFile(composeFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != sentinel {
		t.Errorf("compose.yml was overwritten; got:\n%s", got)
	}
}

// TestSyncRestartsCrashedProxy verifies that unchanged content + routes exist +
// proxy NOT running still results in a compose up call (crashed-proxy recovery),
// but does NOT call validate or reload (caddy reads the Caddyfile at startup).
func TestSyncRestartsCrashedProxy(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	routes := []Route{{Stack: "blog", Service: "web", Hostname: "blog.localhost", Alias: "web.blog", Port: 80}}

	// First Sync: normal start path writes files and brings proxy up.
	f1 := &runtime.Fake{}
	if err := Sync(t.Context(), f1, routes, false); err != nil {
		t.Fatal(err)
	}

	// Second Sync: same routes (content unchanged), proxy NOT running (simulates crash).
	f2 := &runtime.Fake{}
	if err := Sync(t.Context(), f2, routes, false); err != nil {
		t.Fatal(err)
	}

	// Must record a compose up call for the proxy.
	upSeen := false
	for _, c := range f2.Calls {
		if c[0] == StackName && strings.Contains(strings.Join(c, " "), "up -d") {
			upSeen = true
		}
	}
	if !upSeen {
		t.Errorf("crashed proxy must be restarted: calls=%v", f2.Calls)
	}

	// Must NOT call validate or reload (content unchanged; caddy reads file at startup).
	for _, c := range f2.Cmds {
		joined := strings.Join(c, " ")
		if strings.Contains(joined, "caddy validate") {
			t.Errorf("unexpected caddy validate on unchanged content: %v", f2.Cmds)
		}
		if strings.Contains(joined, "caddy reload") {
			t.Errorf("unexpected caddy reload on unchanged content: %v", f2.Cmds)
		}
	}
}

// TestSyncReloadFailureKeepsOldConfigOnDisk verifies that a reload failure after a
// successful validate leaves the old Caddyfile on disk unchanged (Caddyfile.new is
// removed), so the next Sync sees a diff and retries.
func TestSyncReloadFailureKeepsOldConfigOnDisk(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())

	// Step 1: seed a successful Sync with routesA.
	f1 := &runtime.Fake{}
	routesA := []Route{{Stack: "a", Service: "w", Hostname: "a.localhost", Alias: "w.a", Port: 80}}
	if err := Sync(t.Context(), f1, routesA, false); err != nil {
		t.Fatal(err)
	}

	// Verify routesA content is on disk.
	caddyPath := filepath.Join(Dir(), "Caddyfile")
	b, err := os.ReadFile(caddyPath)
	if err != nil || !strings.Contains(string(b), "a.localhost") {
		t.Fatalf("seed Caddyfile bad: %s err=%v", b, err)
	}

	// Step 2: Sync with routesB, but fail on caddy reload.
	routesB := []Route{{Stack: "b", Service: "w", Hostname: "b.localhost", Alias: "w.b", Port: 80}}
	bad := &runtime.Fake{FailPrefix: []string{"exec kazi-proxy caddy reload"}}
	if err := Sync(t.Context(), bad, routesB, true); err == nil {
		t.Fatal("want reload error")
	}

	// Caddyfile must still hold routesA's content.
	b, err = os.ReadFile(caddyPath)
	if err != nil {
		t.Fatalf("reading Caddyfile after reload failure: %v", err)
	}
	if !strings.Contains(string(b), "a.localhost") {
		t.Errorf("old config hostname missing after reload failure:\n%s", b)
	}
	if strings.Contains(string(b), "b.localhost") {
		t.Errorf("new config hostname must NOT be in Caddyfile after reload failure:\n%s", b)
	}

	// Caddyfile.new must be cleaned up.
	newPath := filepath.Join(Dir(), "Caddyfile.new")
	if _, err := os.Stat(newPath); !os.IsNotExist(err) {
		t.Errorf("Caddyfile.new should be removed after reload failure; stat err=%v", err)
	}
}

func TestSystemManifest(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	m := SystemManifest()
	if m.Metadata.Name != StackName || !m.Spec.System ||
		m.Spec.Source.Compose != filepath.Join(Dir(), "compose.yml") {
		t.Errorf("m = %+v", m)
	}
}
