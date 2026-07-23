package engine

import (
	"errors"
	"testing"

	"github.com/thapakazi/kazi/internal/runtime"
	"github.com/thapakazi/kazi/internal/store"
)

// composeContainer builds a ps row for a compose service replica of the "blog"
// stack (project kazi-blog) at the given dir.
func composeContainer(dir, service, replica, id, state string) runtime.Container {
	return runtime.Container{
		ID: id, Name: "kazi-blog-" + service + "-" + replica, State: state,
		Status: "Up", Labels: map[string]string{
			"com.docker.compose.project":             "kazi-blog",
			"com.docker.compose.project.working_dir": dir,
			"com.docker.compose.service":             service,
			"kazi.managed":                           "true",
			"kazi.stack":                             "blog",
		},
	}
}

// Compose stack: service resolves to its running container; the compose path is
// flagged so exec goes through `compose … exec <service>`.
func TestResolveContainerCompose(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	dir := registerStack(t, "blog")
	f := &runtime.Fake{ConfigJSON: blogConfigJSON, Containers: []runtime.Container{
		composeContainer(dir, "web", "1", "web111", "running"),
		composeContainer(dir, "db", "1", "db111", "running"),
	}}
	e := testEngine(t, f)

	r, err := e.resolveExec(t.Context(), "blog", "web", 0)
	if err != nil {
		t.Fatal(err)
	}
	if !r.useCompose || r.service != "web" || r.container != "web111" || r.index != 1 {
		t.Errorf("resolvedExec = %+v", r)
	}

	// ResolveContainer is the public convenience returning just the id.
	id, err := e.ResolveContainer(t.Context(), "blog", "db", 1)
	if err != nil || id != "db111" {
		t.Errorf("ResolveContainer db = %q err %v", id, err)
	}
}

// Multi-replica: default index 1, --index selects, out-of-range errors.
func TestResolveContainerReplicaIndex(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	dir := registerStack(t, "blog")
	f := &runtime.Fake{ConfigJSON: blogConfigJSON, Containers: []runtime.Container{
		composeContainer(dir, "web", "1", "web1", "running"),
		composeContainer(dir, "web", "2", "web2", "running"),
	}}
	e := testEngine(t, f)

	if id, _ := e.ResolveContainer(t.Context(), "blog", "web", 0); id != "web1" {
		t.Errorf("index 0 ⇒ %q, want web1 (first replica)", id)
	}
	if id, _ := e.ResolveContainer(t.Context(), "blog", "web", 2); id != "web2" {
		t.Errorf("index 2 ⇒ %q, want web2", id)
	}
	if _, err := e.ResolveContainer(t.Context(), "blog", "web", 3); !errors.Is(err, ErrServiceNotFound) {
		t.Errorf("index 3 out of range ⇒ %v, want ErrServiceNotFound", err)
	}
}

// Unknown service ⇒ ErrServiceNotFound; a stopped replica ⇒ ErrServiceNotRunning.
func TestResolveContainerServiceErrors(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	dir := registerStack(t, "blog")
	f := &runtime.Fake{ConfigJSON: blogConfigJSON, Containers: []runtime.Container{
		composeContainer(dir, "web", "1", "web1", "exited"),
	}}
	e := testEngine(t, f)

	if _, err := e.ResolveContainer(t.Context(), "blog", "ghost", 1); !errors.Is(err, ErrServiceNotFound) {
		t.Errorf("unknown service ⇒ %v, want ErrServiceNotFound", err)
	}
	if _, err := e.ResolveContainer(t.Context(), "blog", "web", 1); !errors.Is(err, ErrServiceNotRunning) {
		t.Errorf("stopped service ⇒ %v, want ErrServiceNotRunning", err)
	}
}

// Missing stack ⇒ ErrStackNotFound (bubbles from resolve()).
func TestResolveContainerStackNotFound(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	e := testEngine(t, &runtime.Fake{})
	if _, err := e.ResolveContainer(t.Context(), "ghost", "web", 1); !errors.Is(err, ErrStackNotFound) {
		t.Errorf("missing stack ⇒ %v, want ErrStackNotFound", err)
	}
}

// Image stack: the single container resolves directly (exec <container>, no compose).
func TestResolveContainerImage(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	registerImageStack(t, "cache", "redis:7", false, nil, nil, nil)
	f := &runtime.Fake{Containers: []runtime.Container{
		{ID: "redis1", Name: "cache", State: "running", Status: "Up", Labels: map[string]string{
			"kazi.managed": "true", "kazi.stack": "cache",
		}},
	}}
	e := testEngine(t, f)

	r, err := e.resolveExec(t.Context(), "cache", "cache", 1)
	if err != nil {
		t.Fatal(err)
	}
	if r.useCompose || r.container != "redis1" {
		t.Errorf("image resolvedExec = %+v, want direct exec of redis1", r)
	}
}

// Adopted stack: the service names an adopted container, resolved by name.
func TestResolveContainerAdopted(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	saveStack(t, "legacy", store.Source{Containers: []string{"pg"}})
	f := &runtime.Fake{Containers: []runtime.Container{
		{ID: "pg1", Name: "pg", State: "running", Status: "Up", Labels: map[string]string{}},
	}}
	e := testEngine(t, f)

	r, err := e.resolveExec(t.Context(), "legacy", "pg", 1)
	if err != nil {
		t.Fatal(err)
	}
	if r.useCompose || r.container != "pg1" {
		t.Errorf("adopted resolvedExec = %+v, want direct exec of pg1", r)
	}
	if _, err := e.ResolveContainer(t.Context(), "legacy", "nope", 1); !errors.Is(err, ErrServiceNotFound) {
		t.Errorf("unknown adopted container ⇒ %v, want ErrServiceNotFound", err)
	}
}
