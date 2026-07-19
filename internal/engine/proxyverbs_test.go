package engine

import (
	"context"
	"io"
	"os/exec"
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

// TestUrlsImageStackPinnedPort: an image stack routes via a pinned
// spec.proxy.http_port (e.g. mailpit's 8025) and is listed by `kazi urls`,
// honoring a custom spec.proxy.hostname.
func TestUrlsImageStackPinnedPort(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	m := store.Manifest{APIVersion: "kazi.dev/v1alpha1", Kind: "Stack"}
	m.Metadata.Name = "malpit"
	m.Spec.Source.Image = "axllent/mailpit"
	m.Spec.Proxy = &store.ProxySpec{Hostname: "mailpit", HTTPPort: 8025}
	if err := store.SaveStack(m); err != nil {
		t.Fatal(err)
	}
	e := testEngine(t, &runtime.Fake{})
	eps, err := e.Urls(t.Context(), "malpit")
	if err != nil {
		t.Fatal(err)
	}
	var ok bool
	for _, ep := range eps {
		if ep.Kind == "http" && ep.URL == "https://mailpit.localhost" && ep.Target == "malpit:8025" {
			ok = true
		}
	}
	if !ok {
		t.Fatalf("image stack with a pinned port should list its URL, got %+v", eps)
	}
}

func TestTrustDarwinCommands(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	f := &runtime.Fake{CmdOut: map[string]string{"exec kazi-proxy cat": "CERT"}}
	e := testEngine(t, f)
	old := osOverride
	osOverride = "darwin"
	defer func() { osOverride = old }()
	// Record host-side commands instead of running sudo for real.
	var hostCalls [][]string
	oldHost := hostCmd
	hostCmd = func(ctx context.Context, args []string) *exec.Cmd {
		hostCalls = append(hostCalls, args)
		return exec.Command("true")
	}
	defer func() { hostCmd = oldHost }()
	if err := e.Trust(t.Context(), false); err != nil {
		t.Fatal(err)
	}
	if len(f.Cmds) == 0 || !strings.HasPrefix(strings.Join(f.Cmds[0], " "), "exec kazi-proxy cat") {
		t.Errorf("cert extraction missing: %v", f.Cmds)
	}
	// The keychain install runs on the HOST, never through the container
	// runtime: the only runtime command allowed is the cert extraction.
	if len(f.Cmds) != 1 {
		t.Errorf("trust must not route host commands through the runtime; recorded runtime cmds: %v", f.Cmds)
	}
	if len(hostCalls) != 1 {
		t.Fatalf("want 1 host command, got %v", hostCalls)
	}
	joined := strings.Join(hostCalls[0], " ")
	if !strings.HasPrefix(joined, "sudo security add-trusted-cert") {
		t.Errorf("host command = %q, want sudo security add-trusted-cert ...", joined)
	}
}

func TestUrlsConfigFailureWarning(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	registerStack(t, "blog")
	// ConfigJSON set to invalid JSON so ParseConfig errors inside Urls.
	f := &runtime.Fake{ConfigJSON: "not-valid-json"}
	var errBuf strings.Builder
	cfg, _ := store.LoadConfig()
	e := New(f, cfg, io.Discard, &errBuf)

	// Urls must return nil error and emit a warning on Err.
	eps, err := e.Urls(t.Context(), "blog")
	if err != nil {
		t.Fatalf("Urls must return nil error even when compose config fails: %v", err)
	}
	if len(eps) != 0 {
		t.Errorf("expected no endpoints when config fails, got: %+v", eps)
	}
	if !strings.Contains(errBuf.String(), "kazi: warning: reading compose config for blog") {
		t.Errorf("expected config warning on Err writer; got: %q", errBuf.String())
	}
}

func TestExposeRecreateWarningOnFailure(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	blogDir := registerStack(t, "blog")
	// A running container for "blog" so exposeRecreate sees it as running.
	containers := []runtime.Container{
		{
			Name:  "blog-web-1",
			State: "running",
			Labels: map[string]string{
				"com.docker.compose.project":             "kazi-blog",
				"com.docker.compose.project.working_dir": blogDir,
				"com.docker.compose.service":             "web",
				"kazi.managed":                           "true",
				"kazi.stack":                             "blog",
			},
		},
	}
	// ConfigJSON supplies web:80 for exposeRecreate's buildOverride call too.
	f := &runtime.Fake{
		ConfigJSON: blogConfigJSON,
		Containers: containers,
		// FailPrefix on Cmd only affects RT.Cmd; compose "up" goes through ComposeCmd.
		// We make the ComposeCmd "up" call fail by giving Fake a FailPrefix that
		// matches on args joined — but Fake.ComposeCmd doesn't honour FailPrefix.
		// Instead, override ConfigJSON to empty so ParseConfig errors during buildOverride
		// inside exposeRecreate, which causes it to return an error.
	}
	// First: allocate so Expose (remove=false) succeeds up to the recreate step.
	// We need valid ConfigJSON for the initial Expose call's detectContainerPort, then
	// fail inside exposeRecreate's buildOverride. We achieve this by calling Expose
	// with an explicit port (skipping detectContainerPort) and then swapping ConfigJSON
	// to invalid JSON so exposeRecreate's compose config call yields a parse error.
	var errBuf strings.Builder
	cfg, _ := store.LoadConfig()
	e := New(f, cfg, io.Discard, &errBuf)

	// Expose with pinned port so detectContainerPort is skipped.
	_, err := e.Expose(t.Context(), "blog", "db", 42500, false)
	if err != nil {
		t.Fatalf("initial expose failed: %v", err)
	}

	// Now corrupt ConfigJSON so the NEXT compose config call (inside exposeRecreate)
	// returns invalid JSON, causing ParseConfig to error inside buildOverride.
	f.ConfigJSON = "not-json"

	// Remove should trigger exposeRecreate, which will fail, but Expose should return nil.
	_, err = e.Expose(t.Context(), "blog", "db", 0, true)
	if err != nil {
		t.Fatalf("Expose(remove) must return nil even when recreate fails: %v", err)
	}
	if !strings.Contains(errBuf.String(), "kazi: warning: expose recreate failed") {
		t.Errorf("expected recreate warning on Err writer; got: %q", errBuf.String())
	}
}
