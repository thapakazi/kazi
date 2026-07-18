package engine

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/thapakazi/kazi/internal/runtime"
)

func TestAddDirectoryFindsComposeFile(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	dir := t.TempDir()
	file := filepath.Join(dir, "docker-compose.yml")
	os.WriteFile(file, []byte("services: {}\n"), 0o644)

	e := New(&runtime.Fake{}, io.Discard, io.Discard)
	m, err := e.Add("blog", dir)
	if err != nil {
		t.Fatal(err)
	}
	if m.Spec.Source.Compose != file {
		t.Errorf("compose = %q, want %q", m.Spec.Source.Compose, file)
	}
	if m.APIVersion != "kazi.dev/v1alpha1" || m.Kind != "Stack" || m.Metadata.Name != "blog" {
		t.Errorf("manifest = %+v", m)
	}
}

func TestAddExplicitFile(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	dir := t.TempDir()
	file := filepath.Join(dir, "compose.yaml")
	os.WriteFile(file, []byte("services: {}\n"), 0o644)
	e := New(&runtime.Fake{}, io.Discard, io.Discard)
	m, err := e.Add("api", file)
	if err != nil || m.Spec.Source.Compose != file {
		t.Errorf("m=%+v err=%v", m, err)
	}
}

func TestAddNameTaken(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte("services: {}\n"), 0o644)
	e := New(&runtime.Fake{}, io.Discard, io.Discard)
	if _, err := e.Add("blog", dir); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Add("blog", dir); err == nil {
		t.Error("second Add with same name should fail")
	}
}

func TestAddNoComposeInDir(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	e := New(&runtime.Fake{}, io.Discard, io.Discard)
	if _, err := e.Add("empty", t.TempDir()); err == nil {
		t.Error("want error for dir without compose file")
	}
}

func TestRemove(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte("services: {}\n"), 0o644)
	e := New(&runtime.Fake{}, io.Discard, io.Discard)
	e.Add("blog", dir)
	if err := e.Remove("blog"); err != nil {
		t.Fatal(err)
	}
	if err := e.Remove("blog"); !errors.Is(err, ErrStackNotFound) {
		t.Errorf("want ErrStackNotFound, got %v", err)
	}
}

func TestJumpRegistered(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	blogDir := registerStack(t, "blog")
	e := New(&runtime.Fake{}, io.Discard, io.Discard)
	dir, err := e.Jump(t.Context(), "blog")
	if err != nil || dir != blogDir {
		t.Errorf("dir=%q err=%v want %q", dir, err, blogDir)
	}
}

func TestJumpDiscovered(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	fake := &runtime.Fake{Containers: []runtime.Container{
		container("legacy-db-1", "legacy", "/srv/legacy", "db", "running", "Up 2 days"),
	}}
	e := New(fake, io.Discard, io.Discard)
	dir, err := e.Jump(t.Context(), "legacy")
	if err != nil || dir != "/srv/legacy" {
		t.Errorf("dir=%q err=%v", dir, err)
	}
}
