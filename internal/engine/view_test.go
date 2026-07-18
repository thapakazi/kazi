package engine

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/thapakazi/kazi/internal/runtime"
	"github.com/thapakazi/kazi/internal/store"
)

// registerStack writes a manifest whose compose file actually exists.
func registerStack(t *testing.T, name string) string {
	t.Helper()
	dir := t.TempDir()
	compose := filepath.Join(dir, "docker-compose.yml")
	if err := os.WriteFile(compose, []byte("services:\n  web:\n    image: nginx\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := store.Manifest{APIVersion: "kazi.dev/v1alpha1", Kind: "Stack"}
	m.Metadata.Name = name
	m.Spec.Source.Compose = compose
	if err := store.SaveStack(m); err != nil {
		t.Fatal(err)
	}
	return dir
}

func container(name, project, workdir, service, state, status string) runtime.Container {
	l := map[string]string{}
	if project != "" {
		l["com.docker.compose.project"] = project
		l["com.docker.compose.project.working_dir"] = workdir
		l["com.docker.compose.service"] = service
	}
	return runtime.Container{ID: name + "-id", Name: name, Image: "img", State: state, Status: status, Labels: l}
}

func TestListGroupsThreeKinds(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	blogDir := registerStack(t, "blog")

	fake := &runtime.Fake{Containers: []runtime.Container{
		container("blog-web-1", "kazi-blog", blogDir, "web", "running", "Up 1 hour (healthy)"),
		container("legacy-db-1", "legacy", "/srv/legacy", "db", "running", "Up 2 days"),
		container("stray", "", "", "", "running", "Up 5 minutes"),
	}}
	e := New(fake, io.Discard, io.Discard)

	stacks, err := e.List(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(stacks) != 2 {
		t.Fatalf("got %d stacks: %+v", len(stacks), stacks)
	}
	byName := map[string]StackInfo{}
	for _, s := range stacks {
		byName[s.Name] = s
	}
	blog := byName["blog"]
	if blog.Kind != KindRegistered || blog.Running != 1 || blog.Total != 1 || blog.Project != "kazi-blog" {
		t.Errorf("blog = %+v", blog)
	}
	legacy := byName["legacy"]
	if legacy.Kind != KindDiscovered || legacy.Dir != "/srv/legacy" {
		t.Errorf("legacy = %+v", legacy)
	}
}

func TestListShowsStoppedRegisteredStack(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	registerStack(t, "idle")
	e := New(&runtime.Fake{}, io.Discard, io.Discard)
	stacks, err := e.List(t.Context())
	if err != nil || len(stacks) != 1 {
		t.Fatalf("stacks=%v err=%v", stacks, err)
	}
	if stacks[0].Running != 0 || stacks[0].Total != 0 || stacks[0].Kind != KindRegistered {
		t.Errorf("idle = %+v", stacks[0])
	}
}

// Registered stack matched by working_dir even under a foreign project
// name (user ran `docker compose up` by hand before registering).
func TestListMatchesRegisteredByWorkingDir(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	blogDir := registerStack(t, "blog")
	fake := &runtime.Fake{Containers: []runtime.Container{
		container("blog-web-1", "blog", blogDir, "web", "running", "Up 1 hour"),
	}}
	e := New(fake, io.Discard, io.Discard)
	stacks, _ := e.List(t.Context())
	if len(stacks) != 1 || stacks[0].Kind != KindRegistered || stacks[0].Project != "blog" {
		t.Errorf("stacks = %+v", stacks)
	}
}

func TestPsIncludesUnmanaged(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	fake := &runtime.Fake{Containers: []runtime.Container{
		container("legacy-db-1", "legacy", "/srv/legacy", "db", "running", "Up 2 days"),
		container("stray", "", "", "", "exited", "Exited (0) 1 day ago"),
	}}
	e := New(fake, io.Discard, io.Discard)
	cs, err := e.Ps(t.Context())
	if err != nil || len(cs) != 2 {
		t.Fatalf("cs=%v err=%v", cs, err)
	}
	kinds := map[string]Kind{}
	for _, c := range cs {
		kinds[c.Name] = c.Kind
	}
	if kinds["legacy-db-1"] != KindDiscovered || kinds["stray"] != KindUnmanaged {
		t.Errorf("kinds = %v", kinds)
	}
}

func TestStatusSingleStack(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	blogDir := registerStack(t, "blog")
	fake := &runtime.Fake{Containers: []runtime.Container{
		container("blog-web-1", "kazi-blog", blogDir, "web", "running", "Up 1 hour (unhealthy)"),
	}}
	e := New(fake, io.Discard, io.Discard)
	st, err := e.Status(t.Context(), "blog")
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Containers) != 1 || st.Containers[0].Service != "web" || st.Containers[0].Health != "unhealthy" {
		t.Errorf("status = %+v", st)
	}
}

func TestStatusNotFound(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	e := New(&runtime.Fake{}, io.Discard, io.Discard)
	if _, err := e.Status(t.Context(), "ghost"); !errors.Is(err, ErrStackNotFound) {
		t.Errorf("want ErrStackNotFound, got %v", err)
	}
}

func TestHealthOf(t *testing.T) {
	cases := map[string]string{
		"Up 3 hours (healthy)":           "healthy",
		"Up 3 hours (unhealthy)":         "unhealthy",
		"Up 1 second (health: starting)": "starting",
		"Up 3 hours":                     "-",
		"Restarting (1) 5 seconds ago":   "-",
	}
	for in, want := range cases {
		if got := healthOf(in); got != want {
			t.Errorf("healthOf(%q) = %q, want %q", in, got, want)
		}
	}
}
