package tui

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/thapakazi/kazi/internal/engine"
	"github.com/thapakazi/kazi/internal/store"
	"github.com/thapakazi/kazi/internal/template"
)

// fakeEngine implements the Engine seam with canned data for tests:
// registered "blog" (running, healthy), registered "api" (stopped),
// discovered "redis" (running), unmanaged "n8n" (running), and the protected
// system stack "kazi-proxy" (running).
type fakeEngine struct{}

var _ Engine = fakeEngine{}

func (fakeEngine) List(context.Context) ([]engine.StackInfo, error) {
	return []engine.StackInfo{
		{Name: "blog", Kind: engine.KindRegistered, Running: 2, Total: 2, Project: "kazi-blog",
			Containers: []engine.ContainerInfo{
				{Name: "blog-web-1", Service: "web", State: "running", Health: "healthy", Kind: engine.KindRegistered},
				{Name: "blog-db-1", Service: "db", State: "running", Health: "healthy", Kind: engine.KindRegistered},
			}},
		{Name: "api", Kind: engine.KindRegistered, Running: 0, Total: 1, Project: "kazi-api"},
		{Name: "redis", Kind: engine.KindDiscovered, Running: 1, Total: 1, Project: "redis",
			Containers: []engine.ContainerInfo{
				{Name: "redis-1", Service: "redis", State: "running", Kind: engine.KindDiscovered},
			}},
		{Name: "kazi-proxy", Kind: engine.KindRegistered, Running: 1, Total: 1, Project: "kazi-proxy",
			Containers: []engine.ContainerInfo{
				{Name: "kazi-proxy-1", Service: "proxy", State: "running", Kind: engine.KindRegistered},
			}},
	}, nil
}

func (f fakeEngine) Ps(ctx context.Context) ([]engine.ContainerInfo, error) {
	stacks, _ := f.List(ctx)
	var out []engine.ContainerInfo
	for _, s := range stacks {
		out = append(out, s.Containers...)
	}
	out = append(out, engine.ContainerInfo{
		Name: "n8n", Image: "n8nio/n8n", State: "running", Kind: engine.KindUnmanaged,
	})
	return out, nil
}

func (f fakeEngine) StackEnv(ctx context.Context, name string) ([]engine.ContainerEnv, error) {
	st, err := f.Status(ctx, name)
	if err != nil {
		return nil, err
	}
	var out []engine.ContainerEnv
	for _, c := range st.Containers {
		svc := c.Service
		if svc == "" {
			svc = c.Name
		}
		out = append(out, engine.ContainerEnv{
			Service: svc, Name: c.Name,
			Env: []string{"PATH=/usr/bin", "SVC=" + svc},
		})
	}
	return out, nil
}

func (f fakeEngine) Status(ctx context.Context, name string) (engine.StackInfo, error) {
	stacks, _ := f.List(ctx)
	for _, s := range stacks {
		if s.Name == name {
			return s, nil
		}
	}
	return engine.StackInfo{}, engine.ErrStackNotFound
}

func (f fakeEngine) Describe(ctx context.Context, name string) (engine.StackDetail, error) {
	st, err := f.Status(ctx, name)
	if err != nil {
		return engine.StackDetail{}, err
	}
	d := engine.StackDetail{StackInfo: st}
	if name == "blog" {
		d.Source = "/tmp/blog/compose.yaml"
	}
	if name == "kazi-proxy" {
		d.System = true
	}
	return d, nil
}

func (fakeEngine) Urls(ctx context.Context, name string) ([]engine.Endpoint, error) {
	if name == "blog" {
		return []engine.Endpoint{
			{Stack: "blog", Service: "web", Kind: "http", URL: "https://blog.localhost", Target: "web:80"},
			{Stack: "blog", Service: "db", Kind: "tcp", URL: "localhost:42017", Target: "db:5432"},
		}, nil
	}
	return nil, nil
}

func (fakeEngine) GcDebris(context.Context) int { return 2 }

func (fakeEngine) LogStream(ctx context.Context, name, service string, opts engine.LogStreamOpts) (io.ReadCloser, context.CancelFunc, error) {
	var body string
	if name == "blog" {
		body = "blog-web-1  | GET / 200\nblog-db-1   | ready to accept connections\n"
	}
	return io.NopCloser(strings.NewReader(body)), func() {}, nil
}

func (fakeEngine) Remove(string) error                           { return nil }
func (fakeEngine) RemoveContainer(context.Context, string) error { return nil }
func (fakeEngine) Adopt(context.Context, string, []string) error { return nil }

func (fakeEngine) ActionStream(_ context.Context, action, name string) (io.ReadCloser, <-chan error) {
	errc := make(chan error, 1)
	errc <- nil
	body := fmt.Sprintf("[+] Running 1/1\nContainer %s-1  %s\n", name, action)
	return io.NopCloser(strings.NewReader(body)), errc
}

func (fakeEngine) TemplateList() ([]template.Info, error) {
	return []template.Info{
		{Name: "wordpress", Description: "WordPress + MySQL", Embedded: true},
		{Name: "postgres", Description: "PostgreSQL", Embedded: true},
	}, nil
}

func (fakeEngine) Add(name, path string) (store.Manifest, error) {
	m := store.Manifest{APIVersion: "kazi.dev/v1alpha1", Kind: "Stack"}
	m.Metadata.Name = name
	m.Spec.Source.Compose = path
	return m, nil
}

// EditTargets: compose-backed "blog" gets both manifest+compose; everything
// else (e.g. template-backed) gets manifest only. Validators are no-ops here.
func (fakeEngine) EditTargets(name string) ([]engine.EditTarget, error) {
	ok := func(context.Context) error { return nil }
	targets := []engine.EditTarget{{Path: "/cfg/stacks/" + name + ".yaml", Kind: "manifest", Validate: ok}}
	if name == "blog" {
		targets = append(targets, engine.EditTarget{Path: "/tmp/blog/compose.yaml", Kind: "compose", Validate: ok})
	}
	return targets, nil
}

// TryValues returns a must-change password plus a normal key for any template.
func (fakeEngine) TryValues(tmpl string) ([]engine.TryValue, error) {
	return []engine.TryValue{
		{Key: "postgres_db", Value: "app", MustChange: false},
		{Key: "postgres_password", Value: "change-me", MustChange: true},
	}, nil
}

func (fakeEngine) Try(_ context.Context, tmpl string, opts engine.TryOpts) (string, []engine.Endpoint, error) {
	if opts.Name != "" {
		return opts.Name, nil, nil
	}
	return tmpl, nil, nil
}

func (fakeEngine) RunImage(_ context.Context, name, image string, _ engine.RunOpts) (string, error) {
	return name, nil
}

func (fakeEngine) SetHostname(string, string) error { return nil }

func (fakeEngine) Keep(string) error                      { return nil }
func (fakeEngine) Teardown(context.Context, string) error { return nil }

func (fakeEngine) RoutesFromStack(_ context.Context, stack string) ([]engine.RouteCandidate, error) {
	if stack == "redis" {
		return []engine.RouteCandidate{{Host: "redis", Port: 63799, Service: "redis", Target: 6379}}, nil
	}
	return nil, nil
}
func (fakeEngine) RouteAdd(context.Context, string, int, string, string) error { return nil }
