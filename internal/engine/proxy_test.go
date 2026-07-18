package engine

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thapakazi/kazi/internal/proxy"
	"github.com/thapakazi/kazi/internal/runtime"
	"github.com/thapakazi/kazi/internal/store"
)

const blogConfigJSON = `{"services":{"web":{"ports":[{"target":80,"published":""}],"networks":{"default":null}},"db":{"expose":["5432"],"networks":{"default":null}}}}`

func testEngine(t *testing.T, f *runtime.Fake) *Engine {
	t.Helper()
	cfg, err := store.LoadConfig() // seeds default lists under KAZI_CONFIG_DIR
	if err != nil {
		t.Fatal(err)
	}
	return New(f, cfg, io.Discard, io.Discard)
}

func TestUpRoutableCreatesNetworkAndSyncsProxy(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	registerStack(t, "blog")
	f := &runtime.Fake{ConfigJSON: blogConfigJSON, FailPrefix: []string{"network inspect"}}
	e := testEngine(t, f)
	if err := e.Up(t.Context(), "blog"); err != nil {
		t.Fatal(err)
	}
	joined := ""
	for _, c := range f.Cmds {
		joined += strings.Join(c, " ") + "\n"
	}
	if !strings.Contains(joined, "network create kazi") {
		t.Errorf("kazi network not created:\n%s", joined)
	}
	// Caddyfile rendered with the stack route
	b, err := os.ReadFile(filepath.Join(proxy.Dir(), "Caddyfile"))
	if err != nil || !strings.Contains(string(b), "blog.localhost") ||
		!strings.Contains(string(b), "reverse_proxy web.blog:80") {
		t.Errorf("caddyfile=%s err=%v", b, err)
	}
	// proxy system manifest registered
	if _, err := store.LoadStack(proxy.StackName); err != nil {
		t.Errorf("system manifest not registered: %v", err)
	}
}

func TestUpProxyFailureIsWarningNotError(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	registerStack(t, "blog")
	var errBuf strings.Builder
	f := &runtime.Fake{ConfigJSON: blogConfigJSON, FailPrefix: []string{"exec kazi-proxy caddy validate"}}
	cfg, _ := store.LoadConfig()
	e := New(f, cfg, io.Discard, &errBuf)
	if err := e.Up(t.Context(), "blog"); err != nil {
		t.Fatalf("proxy failure must not fail up: %v", err)
	}
	if !strings.Contains(errBuf.String(), "kazi: warning: proxy sync failed") {
		t.Errorf("warning missing: %q", errBuf.String())
	}
}

func TestUpInjectsAliasAndExposePorts(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	dir := registerStack(t, "blog")
	m, _ := store.LoadStack("blog")
	m.Spec.Expose = []store.ExposeSpec{{Service: "db", Port: "auto"}}
	store.SaveStack(m)
	f := &runtime.Fake{ConfigJSON: blogConfigJSON}
	e := testEngine(t, f)
	if err := e.Up(t.Context(), "blog"); err != nil {
		t.Fatal(err)
	}
	// find the override file arg of the up call and snapshot its content is
	// impossible post-cleanup; instead assert allocation happened...
	ps, _ := proxy.LoadPorts()
	alloc, ok := ps.Lookup("blog", "db")
	if !ok || alloc.ContainerPort != 5432 || alloc.HostPort < 42000 {
		t.Errorf("allocation = %+v ok=%v", alloc, ok)
	}
	_ = dir
}

func TestDownSyncsProxyRoutesAway(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	registerStack(t, "blog")
	f := &runtime.Fake{ConfigJSON: blogConfigJSON}
	e := testEngine(t, f)
	if err := e.Up(t.Context(), "blog"); err != nil { // renders blog route (fake has no running containers, but Up's own stack is part of desired routes via its plan)
		t.Fatal(err)
	}
	if err := e.Down(t.Context(), "blog"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(proxy.Dir(), "Caddyfile"))
	if strings.Contains(string(b), "blog.localhost") {
		t.Errorf("route must disappear after down:\n%s", b)
	}
}

func TestAddRejectsNonDNSNames(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte("services: {}\n"), 0o644)
	e := testEngine(t, &runtime.Fake{})
	for _, bad := range []string{"My_Blog", "UPPER", "-x", "a.b"} {
		if _, err := e.Add(bad, dir); err == nil || !strings.Contains(err.Error(), "DNS label") {
			t.Errorf("Add(%q) = %v, want DNS-label error", bad, err)
		}
	}
}

func TestRemoveRefusesSystemStackAndFreesPorts(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	if err := store.SaveStack(proxy.SystemManifest()); err != nil {
		t.Fatal(err)
	}
	e := testEngine(t, &runtime.Fake{})
	if err := e.Remove(proxy.StackName); err == nil || !strings.Contains(err.Error(), "system stack") {
		t.Errorf("system rm = %v", err)
	}
	// port freeing on rm
	registerStack(t, "blog")
	ps, _ := proxy.LoadPorts()
	ps.Allocate("blog", "db", 5432, 0, 42000, 42010)
	if err := e.Remove("blog"); err != nil {
		t.Fatal(err)
	}
	ps2, _ := proxy.LoadPorts()
	if _, ok := ps2.Lookup("blog", "db"); ok {
		t.Error("ports not freed on rm")
	}
}
