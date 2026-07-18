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
	if len(f2.Cmds) != 0 && len(f2.Calls) != 0 {
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
	if err := Sync(t.Context(), f, nil, false); err != nil {
		t.Fatal(err)
	}
	if len(f.Calls) != 0 {
		t.Errorf("no routes + proxy down ⇒ never start it: %v", f.Calls)
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
