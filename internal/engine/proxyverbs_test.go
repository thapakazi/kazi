package engine

import (
	"strings"
	"testing"

	"github.com/thapakazi/kazi/internal/proxy"
	"github.com/thapakazi/kazi/internal/runtime"
	"github.com/thapakazi/kazi/internal/store"
)

func TestExposeRoundTrip(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	registerStack(t, "blog")
	f := &runtime.Fake{ConfigJSON: blogConfigJSON} // web:80 + db exposing 5432
	e := testEngine(t, f)

	port, err := e.Expose(t.Context(), "blog", "db", 0, false)
	if err != nil || port < 42000 || port > 42999 {
		t.Fatalf("port=%d err=%v", port, err)
	}
	m, _ := store.LoadStack("blog")
	if len(m.Spec.Expose) != 1 || m.Spec.Expose[0].Service != "db" || m.Spec.Expose[0].Port != "auto" {
		t.Errorf("manifest expose = %+v", m.Spec.Expose)
	}
	// stable across repeat
	again, _ := e.Expose(t.Context(), "blog", "db", 0, false)
	if again != port {
		t.Errorf("unstable: %d vs %d", again, port)
	}
	// remove
	if _, err := e.Expose(t.Context(), "blog", "db", 0, true); err != nil {
		t.Fatal(err)
	}
	m2, _ := store.LoadStack("blog")
	if len(m2.Spec.Expose) != 0 {
		t.Errorf("expose entry not removed: %+v", m2.Spec.Expose)
	}
	ps, _ := proxy.LoadPorts()
	if _, ok := ps.Lookup("blog", "db"); ok {
		t.Error("allocation not freed")
	}
}

func TestExposePinned(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	registerStack(t, "blog")
	e := testEngine(t, &runtime.Fake{ConfigJSON: blogConfigJSON})
	port, err := e.Expose(t.Context(), "blog", "db", 42500, false)
	if err != nil || port != 42500 {
		t.Fatalf("port=%d err=%v", port, err)
	}
	m, _ := store.LoadStack("blog")
	if m.Spec.Expose[0].Port != "42500" {
		t.Errorf("pinned port not in manifest: %+v", m.Spec.Expose)
	}
}

func TestExposeUnregisteredStackFails(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	e := testEngine(t, &runtime.Fake{})
	if _, err := e.Expose(t.Context(), "ghost", "db", 0, false); err == nil {
		t.Error("want error for unregistered stack")
	}
}

func TestUrls(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	blogDir := registerStack(t, "blog")
	f := &runtime.Fake{
		ConfigJSON: blogConfigJSON,
		Containers: []runtime.Container{
			{Name: "blog-web-1", State: "running", Labels: map[string]string{
				"com.docker.compose.project":             "kazi-blog",
				"com.docker.compose.project.working_dir": blogDir,
				"com.docker.compose.service":             "web",
				"kazi.managed":                           "true", "kazi.stack": "blog",
			}},
		},
	}
	e := testEngine(t, f)
	eps, err := e.Urls(t.Context(), "blog")
	if err != nil {
		t.Fatal(err)
	}
	var httpSeen, tcpNudge bool
	for _, ep := range eps {
		if ep.Kind == "http" && ep.URL == "https://blog.localhost" && ep.Target == "web:80" {
			httpSeen = true
		}
		if ep.Kind == "tcp" && ep.Service == "db" && strings.Contains(ep.Note, "kazi expose blog db") {
			tcpNudge = true
		}
	}
	if !httpSeen || !tcpNudge {
		t.Errorf("endpoints = %+v", eps)
	}
}

func TestTrustDarwinCommands(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	f := &runtime.Fake{CmdOut: map[string]string{"exec kazi-proxy cat": "CERT"}}
	e := testEngine(t, f)
	old := osOverride
	osOverride = "darwin"
	defer func() { osOverride = old }()
	sudoRun = func(cmd []string) []string { return append([]string{"echo"}, cmd...) } // stub sudo for test
	defer func() { sudoRun = realSudo }()
	if err := e.Trust(t.Context(), false); err != nil {
		t.Fatal(err)
	}
	if len(f.Cmds) == 0 || !strings.HasPrefix(strings.Join(f.Cmds[0], " "), "exec kazi-proxy cat") {
		t.Errorf("cert extraction missing: %v", f.Cmds)
	}
}
