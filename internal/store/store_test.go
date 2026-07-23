package store

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testManifest(name, compose string) Manifest {
	m := Manifest{APIVersion: "kazi.dev/v1alpha1", Kind: "Stack"}
	m.Metadata.Name = name
	m.Spec.Source.Compose = compose
	return m
}

func TestStackCRUD(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())

	if err := SaveStack(testManifest("blog", "/tmp/blog/docker-compose.yml")); err != nil {
		t.Fatal(err)
	}
	m, err := LoadStack("blog")
	if err != nil {
		t.Fatal(err)
	}
	if m.Spec.Source.Compose != "/tmp/blog/docker-compose.yml" || m.Metadata.Name != "blog" {
		t.Errorf("round-trip mismatch: %+v", m)
	}

	if err := SaveStack(testManifest("api", "/tmp/api/compose.yaml")); err != nil {
		t.Fatal(err)
	}
	all, err := ListStacks()
	if err != nil || len(all) != 2 {
		t.Fatalf("ListStacks = %d manifests, err %v", len(all), err)
	}

	if err := DeleteStack("blog"); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadStack("blog"); !errors.Is(err, ErrNotFound) {
		t.Errorf("want ErrNotFound after delete, got %v", err)
	}
	if err := DeleteStack("blog"); !errors.Is(err, ErrNotFound) {
		t.Errorf("double delete: want ErrNotFound, got %v", err)
	}
}

func TestManifestFileShape(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("KAZI_CONFIG_DIR", dir)
	if err := SaveStack(testManifest("blog", "/x/compose.yml")); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "stacks", "blog.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	want := "apiVersion: kazi.dev/v1alpha1\nkind: Stack\nmetadata:\n    name: blog\nspec:\n    source:\n        compose: /x/compose.yml\n"
	if string(b) != want {
		t.Errorf("file:\n%s\nwant:\n%s", b, want)
	}
}

func TestLoadConfigDefaults(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Spec.Runtime != "auto" {
		t.Errorf("default runtime = %q, want auto", cfg.Spec.Runtime)
	}
}

func TestLoadConfigFromFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("KAZI_CONFIG_DIR", dir)
	yaml := "apiVersion: kazi.dev/v1alpha1\nkind: Config\nspec:\n  runtime: podman\n"
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig()
	if err != nil || cfg.Spec.Runtime != "podman" {
		t.Errorf("runtime = %q err %v, want podman", cfg.Spec.Runtime, err)
	}
}

func TestInvalidStackNames(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	for _, bad := range []string{"", "..", "a/b", `a\b`} {
		m := testManifest(bad, "/x/compose.yml")
		if err := SaveStack(m); err == nil {
			t.Errorf("SaveStack(%q) should fail", bad)
		}
		if _, err := LoadStack(bad); err == nil || errors.Is(err, ErrNotFound) {
			t.Errorf("LoadStack(%q) = %v, want validation error", bad, err)
		}
		if err := DeleteStack(bad); err == nil || errors.Is(err, ErrNotFound) {
			t.Errorf("DeleteStack(%q) = %v, want validation error", bad, err)
		}
	}
}

func TestProxyExposeRoundTrip(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	m := testManifest("blog", "/x/compose.yml")
	off := false
	m.Spec.Proxy = &ProxySpec{Service: "web", HTTPPort: 8080, Enabled: &off}
	m.Spec.Expose = []ExposeSpec{{Service: "postgres", Port: "auto"}, {Service: "redis", Port: "6380"}}
	m.Spec.System = true
	if err := SaveStack(m); err != nil {
		t.Fatal(err)
	}
	got, err := LoadStack("blog")
	if err != nil {
		t.Fatal(err)
	}
	if got.Spec.Proxy == nil || got.Spec.Proxy.Service != "web" || got.Spec.Proxy.HTTPPort != 8080 {
		t.Errorf("proxy = %+v", got.Spec.Proxy)
	}
	if got.Spec.Proxy.Enabled == nil || *got.Spec.Proxy.Enabled != false {
		t.Errorf("enabled = %v", got.Spec.Proxy.Enabled)
	}
	if len(got.Spec.Expose) != 2 || got.Spec.Expose[0].Port != "auto" || got.Spec.Expose[1].Port != "6380" {
		t.Errorf("expose = %+v", got.Spec.Expose)
	}
	if !got.Spec.System {
		t.Error("system flag lost")
	}
}

// M0 manifests (no proxy/expose keys) still load, and M1 fields stay omitted when empty.
func TestM0ManifestStillValid(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	if err := SaveStack(testManifest("plain", "/x/compose.yml")); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(Root(), "stacks", "plain.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, banned := range []string{"proxy", "expose", "system"} {
		if strings.Contains(string(b), banned) {
			t.Errorf("empty %s should be omitted:\n%s", banned, b)
		}
	}
	m, err := LoadStack("plain")
	if err != nil || m.Spec.Proxy != nil || m.Spec.Expose != nil || m.Spec.System {
		t.Errorf("m=%+v err=%v", m, err)
	}
}

func TestConfigProxyDefaults(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Spec.Proxy.TCPPorts) == 0 || cfg.Spec.Proxy.TCPPorts[2] != 5432 {
		t.Errorf("tcp defaults = %v", cfg.Spec.Proxy.TCPPorts)
	}
	if len(cfg.Spec.Proxy.HTTPPorts) == 0 || cfg.Spec.Proxy.HTTPPorts[0] != 80 {
		t.Errorf("http defaults = %v", cfg.Spec.Proxy.HTTPPorts)
	}
	if cfg.Spec.Ports.Range != "42000-42999" {
		t.Errorf("range = %q", cfg.Spec.Ports.Range)
	}
}

// A config file that sets runtime but omits proxy lists still gets defaults.
func TestConfigPartialFileGetsDefaults(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("KAZI_CONFIG_DIR", dir)
	yaml := "apiVersion: kazi.dev/v1alpha1\nkind: Config\nspec:\n  runtime: docker\n"
	os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(yaml), 0o644)
	cfg, err := LoadConfig()
	if err != nil || cfg.Spec.Runtime != "docker" {
		t.Fatalf("cfg=%+v err=%v", cfg, err)
	}
	if len(cfg.Spec.Proxy.TCPPorts) == 0 || cfg.Spec.Ports.Range == "" {
		t.Errorf("defaults not seeded: %+v", cfg.Spec)
	}
}

func TestIsDNSLabel(t *testing.T) {
	valid := []string{"blog", "my-app", "a", "app2", "a1-b2"}
	invalid := []string{"", "-app", "app-", "My_App", "UPPER", "a.b", strings.Repeat("x", 64)}
	for _, n := range valid {
		if !IsDNSLabel(n) {
			t.Errorf("IsDNSLabel(%q) = false, want true", n)
		}
	}
	for _, n := range invalid {
		if IsDNSLabel(n) {
			t.Errorf("IsDNSLabel(%q) = true, want false", n)
		}
	}
}

// M2 tests ---------------------------------------------------------------

func TestSourceKind(t *testing.T) {
	cases := []struct {
		src  Source
		want string
	}{
		{Source{Compose: "/x/compose.yml"}, "compose"},
		{Source{Image: "nginx:latest"}, "image"},
		{Source{Template: "postgres"}, "template"},
		{Source{Containers: []string{"my-container"}}, "containers"},
		{Source{}, ""},
	}
	for _, tc := range cases {
		if got := tc.src.Kind(); got != tc.want {
			t.Errorf("Source%+v.Kind() = %q, want %q", tc.src, got, tc.want)
		}
	}
}

func TestM2ManifestRoundTrip(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	m := Manifest{APIVersion: "kazi.dev/v1alpha1", Kind: "Stack"}
	m.Metadata.Name = "trybox"
	m.Metadata.CreatedAt = "2026-07-17T10:00:00Z"
	m.Spec.Source = Source{Image: "nginx:latest"}
	m.Spec.Ephemeral = true
	m.Spec.Values = map[string]string{"PORT": "8080", "DEBUG": "true"}
	m.Spec.Volumes = []string{"data:/data", "logs:/logs"}
	// M0/M1 fields
	m.Spec.System = false
	m.Spec.Proxy = nil
	m.Spec.Expose = nil

	if err := SaveStack(m); err != nil {
		t.Fatal(err)
	}
	got, err := LoadStack("trybox")
	if err != nil {
		t.Fatal(err)
	}

	if got.Metadata.CreatedAt != "2026-07-17T10:00:00Z" {
		t.Errorf("createdAt = %q, want 2026-07-17T10:00:00Z", got.Metadata.CreatedAt)
	}
	if got.Spec.Source.Kind() != "image" {
		t.Errorf("source.Kind() = %q, want image", got.Spec.Source.Kind())
	}
	if got.Spec.Source.Image != "nginx:latest" {
		t.Errorf("source.image = %q, want nginx:latest", got.Spec.Source.Image)
	}
	if !got.Spec.Ephemeral {
		t.Error("ephemeral lost on round-trip")
	}
	if len(got.Spec.Values) != 2 || got.Spec.Values["PORT"] != "8080" {
		t.Errorf("values = %v", got.Spec.Values)
	}
	if len(got.Spec.Volumes) != 2 || got.Spec.Volumes[0] != "data:/data" {
		t.Errorf("volumes = %v", got.Spec.Volumes)
	}
	// M0/M1 fields untouched
	if got.Spec.Proxy != nil {
		t.Errorf("proxy should be nil, got %+v", got.Spec.Proxy)
	}
	if got.Spec.Expose != nil {
		t.Errorf("expose should be nil, got %+v", got.Spec.Expose)
	}
}

// Extend M0 manifest validation: new M2 keys must stay absent from plain manifests.
func TestM0ManifestNewKeysBanned(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	if err := SaveStack(testManifest("plain2", "/x/compose.yml")); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(Root(), "stacks", "plain2.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, banned := range []string{"ephemeral", "values", "volumes", "createdAt"} {
		if strings.Contains(string(b), banned) {
			t.Errorf("empty %s should be omitted from plain manifest:\n%s", banned, b)
		}
	}
}

// M9: spec.exec.shell and spec.tui.returnImmediately are additive config keys.
// Absent ⇒ zero values (empty shell ⇒ login-shell probe; returnImmediately false);
// a file that sets them round-trips.
func TestExecConfigRoundTrip(t *testing.T) {
	// Absent config file ⇒ zero values, no defaults injected.
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Spec.Exec.Shell != "" {
		t.Errorf("absent config: exec.shell = %q, want empty", cfg.Spec.Exec.Shell)
	}
	if cfg.Spec.TUI.ReturnImmediately {
		t.Errorf("absent config: tui.returnImmediately = true, want false")
	}

	// File that sets both ⇒ round-trips.
	dir := t.TempDir()
	t.Setenv("KAZI_CONFIG_DIR", dir)
	yaml := "apiVersion: kazi.dev/v1alpha1\nkind: Config\nspec:\n" +
		"  exec:\n    shell: /bin/zsh\n" +
		"  tui:\n    returnImmediately: true\n"
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg2, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg2.Spec.Exec.Shell != "/bin/zsh" {
		t.Errorf("exec.shell = %q, want /bin/zsh", cfg2.Spec.Exec.Shell)
	}
	if !cfg2.Spec.TUI.ReturnImmediately {
		t.Errorf("tui.returnImmediately = false, want true")
	}
}

func TestCleanupTTLDefault(t *testing.T) {
	// Absent config file => EphemeralTTL seeded to "24h".
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Spec.Cleanup.EphemeralTTL != "24h" {
		t.Errorf("absent config: EphemeralTTL = %q, want 24h", cfg.Spec.Cleanup.EphemeralTTL)
	}

	// Partial file (only runtime set) => EphemeralTTL still seeded.
	dir := t.TempDir()
	t.Setenv("KAZI_CONFIG_DIR", dir)
	partial := "apiVersion: kazi.dev/v1alpha1\nkind: Config\nspec:\n  runtime: docker\n"
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(partial), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg2, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg2.Spec.Cleanup.EphemeralTTL != "24h" {
		t.Errorf("partial config: EphemeralTTL = %q, want 24h", cfg2.Spec.Cleanup.EphemeralTTL)
	}
}
