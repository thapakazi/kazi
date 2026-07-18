package engine

import (
	"errors"
	"testing"

	"github.com/thapakazi/kazi/internal/runtime"
	"github.com/thapakazi/kazi/internal/store"
)

func TestDescribeRegisteredStack(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	blogDir := registerStack(t, "blog")
	m, _ := store.LoadStack("blog")
	m.Spec.Proxy = &store.ProxySpec{Service: "web"}
	if err := store.SaveStack(m); err != nil {
		t.Fatal(err)
	}
	f := &runtime.Fake{
		ConfigJSON: blogConfigJSON,
		Containers: []runtime.Container{
			{Name: "blog-web-1", State: "running", Status: "Up 1 hour", Labels: map[string]string{
				"com.docker.compose.project":             "kazi-blog",
				"com.docker.compose.project.working_dir": blogDir,
				"com.docker.compose.service":             "web",
				"kazi.managed":                           "true",
				"kazi.stack":                             "blog",
			}},
		},
	}
	e := testEngine(t, f)
	d, err := e.Describe(t.Context(), "blog")
	if err != nil {
		t.Fatal(err)
	}
	if d.Name != "blog" || d.Kind != KindRegistered || d.Running != 1 {
		t.Errorf("detail = %+v", d.StackInfo)
	}
	if d.Source == "" || d.Proxy == nil || d.Proxy.Service != "web" {
		t.Errorf("manifest fields missing: source=%q proxy=%+v", d.Source, d.Proxy)
	}
	var httpSeen bool
	for _, ep := range d.Endpoints {
		if ep.Kind == "http" && ep.URL == "https://blog.localhost" {
			httpSeen = true
		}
	}
	if !httpSeen {
		t.Errorf("endpoints missing https route: %+v", d.Endpoints)
	}
}

func TestDescribeNotFound(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	e := testEngine(t, &runtime.Fake{})
	if _, err := e.Describe(t.Context(), "ghost"); !errors.Is(err, ErrStackNotFound) {
		t.Errorf("want ErrStackNotFound, got %v", err)
	}
}
