package store

import (
	"errors"
	"os"
	"path/filepath"
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
