package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thapakazi/kazi/internal/runtime"
	"github.com/thapakazi/kazi/internal/store"
	"github.com/thapakazi/kazi/internal/template"
)

// saveStack writes a manifest for a test after setting the config dir.
func saveStack(t *testing.T, name string, src store.Source) {
	t.Helper()
	m := store.Manifest{APIVersion: "kazi.dev/v1alpha1", Kind: "Stack"}
	m.Metadata.Name = name
	m.Spec.Source = src
	if err := store.SaveStack(m); err != nil {
		t.Fatal(err)
	}
}

func TestEditTargetsComposeStack(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	saveStack(t, "blog", store.Source{Compose: "/tmp/blog/compose.yaml"})

	e := testEngine(t, &runtime.Fake{})
	targets, err := e.EditTargets("blog")
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 2 {
		t.Fatalf("compose stack should have manifest+compose targets, got %d", len(targets))
	}
	if targets[0].Kind != "manifest" || targets[0].Path != store.StackPath("blog") {
		t.Errorf("target[0] = %+v, want manifest at %s", targets[0], store.StackPath("blog"))
	}
	if targets[1].Kind != "compose" || targets[1].Path != "/tmp/blog/compose.yaml" {
		t.Errorf("target[1] = %+v, want compose at /tmp/blog/compose.yaml", targets[1])
	}
}

func TestEditTargetsTemplateRejectsCompose(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	saveStack(t, "pg", store.Source{Template: "postgres"})

	e := testEngine(t, &runtime.Fake{})
	targets, err := e.EditTargets("pg")
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 || targets[0].Kind != "manifest" {
		t.Fatalf("template stack should have only a manifest target, got %+v", targets)
	}
	_, err = e.ComposeTarget("pg")
	if err == nil {
		t.Fatal("ComposeTarget must reject a non-compose stack")
	}
	if !strings.Contains(err.Error(), "no user-owned compose file") {
		t.Errorf("error should point at the alternative, got %v", err)
	}
}

func TestEditTargetsImageAndContainersRejectCompose(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	saveStack(t, "img", store.Source{Image: "nginx"})
	saveStack(t, "adopt", store.Source{Containers: []string{"c1"}})
	e := testEngine(t, &runtime.Fake{})
	for _, name := range []string{"img", "adopt"} {
		targets, _ := e.EditTargets(name)
		if len(targets) != 1 {
			t.Fatalf("%s: non-compose stack should have only a manifest target, got %d", name, len(targets))
		}
		if _, err := e.ComposeTarget(name); err == nil {
			t.Fatalf("%s: ComposeTarget should reject a non-compose stack", name)
		}
	}
}

func TestEditTargetsNotFound(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	e := testEngine(t, &runtime.Fake{})
	if _, err := e.EditTargets("ghost"); !errors.Is(err, ErrStackNotFound) {
		t.Fatalf("missing stack should yield ErrStackNotFound, got %v", err)
	}
}

func TestManifestValidatorWiring(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	saveStack(t, "blog", store.Source{Compose: "/tmp/blog/compose.yaml"})

	e := testEngine(t, &runtime.Fake{})
	tg, err := e.ManifestTarget("blog")
	if err != nil {
		t.Fatal(err)
	}
	if err := tg.Validate(context.Background()); err != nil {
		t.Fatalf("saved manifest should validate, got %v", err)
	}
	// Corrupt the manifest on disk: an unknown top-level field must fail.
	if err := os.WriteFile(store.StackPath("blog"), []byte("bogus: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := tg.Validate(context.Background()); err == nil {
		t.Fatal("an invalid manifest should fail validation")
	}
}

func TestComposeValidatorWiring(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	saveStack(t, "blog", store.Source{Compose: "/tmp/blog/compose.yaml"})

	// Fake compose config exits 0 → valid.
	e := testEngine(t, &runtime.Fake{})
	tg, err := e.ComposeTarget("blog")
	if err != nil {
		t.Fatal(err)
	}
	if err := tg.Validate(context.Background()); err != nil {
		t.Fatalf("passing compose config should validate, got %v", err)
	}

	// Fake compose config exits non-zero → invalid.
	ef := testEngine(t, &runtime.Fake{FailComposeArgs: []string{"config"}})
	tgf, _ := ef.ComposeTarget("blog")
	if err := tgf.Validate(context.Background()); err == nil {
		t.Fatal("failing compose config should fail validation")
	}
}

func TestResolveEditor(t *testing.T) {
	t.Setenv("EDITOR", "")
	t.Setenv("VISUAL", "")
	if got := ResolveEditor("nano"); got != "nano" {
		t.Errorf("override should win, got %q", got)
	}
	t.Setenv("EDITOR", "emacs")
	if got := ResolveEditor(""); got != "emacs" {
		t.Errorf("$EDITOR should win over $VISUAL, got %q", got)
	}
	t.Setenv("EDITOR", "")
	t.Setenv("VISUAL", "vim")
	if got := ResolveEditor(""); got != "vim" {
		t.Errorf("$VISUAL should be used when $EDITOR is empty, got %q", got)
	}
	t.Setenv("VISUAL", "")
	if got := ResolveEditor(""); got != "vi" {
		t.Errorf("fallback should be vi, got %q", got)
	}
}

func TestSetHostname(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	saveStack(t, "blog", store.Source{Compose: "/tmp/blog/compose.yaml"})

	e := testEngine(t, &runtime.Fake{})
	if err := e.SetHostname("blog", "cache"); err != nil {
		t.Fatal(err)
	}
	m, _ := store.LoadStack("blog")
	if m.Spec.Proxy == nil || m.Spec.Proxy.Hostname != "cache" {
		t.Fatalf("SetHostname should write spec.proxy.hostname, got %+v", m.Spec.Proxy)
	}
	// A non-DNS-label hostname is rejected.
	if err := e.SetHostname("blog", "Bad_Host"); err == nil {
		t.Fatal("SetHostname should reject a non-DNS-label hostname")
	}
	// Empty clears the override.
	if err := e.SetHostname("blog", ""); err != nil {
		t.Fatal(err)
	}
	m, _ = store.LoadStack("blog")
	if m.Spec.Proxy.Hostname != "" {
		t.Fatalf("empty host should clear the override, got %q", m.Spec.Proxy.Hostname)
	}
}

func TestTryValues(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	dir := filepath.Join(template.Dir(), "pg")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "compose.yml"), []byte("services: {}\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "values.yaml"),
		[]byte("description: pg\npostgres_db: app\npostgres_password: change-me\n"), 0o644)

	e := testEngine(t, &runtime.Fake{})
	vals, err := e.TryValues("pg")
	if err != nil {
		t.Fatal(err)
	}
	if len(vals) != 2 {
		t.Fatalf("want 2 values (description is stripped), got %d: %+v", len(vals), vals)
	}
	// Sorted by key: postgres_db then postgres_password.
	if vals[0].Key != "postgres_db" || vals[0].MustChange {
		t.Errorf("vals[0] = %+v, want postgres_db not-must-change", vals[0])
	}
	if vals[1].Key != "postgres_password" || !vals[1].MustChange || vals[1].Value != "change-me" {
		t.Errorf("vals[1] = %+v, want postgres_password must-change change-me", vals[1])
	}
}
