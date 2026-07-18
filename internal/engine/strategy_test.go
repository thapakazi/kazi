package engine

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/thapakazi/kazi/internal/labels"
	"github.com/thapakazi/kazi/internal/proxy"
	"github.com/thapakazi/kazi/internal/runtime"
	"github.com/thapakazi/kazi/internal/store"
)

// registerImageStack writes an image-source manifest.
func registerImageStack(t *testing.T, name, image string, ephemeral bool, values map[string]string, expose []store.ExposeSpec, volumes []string) {
	t.Helper()
	m := store.Manifest{APIVersion: "kazi.dev/v1alpha1", Kind: "Stack"}
	m.Metadata.Name = name
	m.Metadata.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	m.Spec.Source.Image = image
	m.Spec.Ephemeral = ephemeral
	m.Spec.Values = values
	m.Spec.Expose = expose
	m.Spec.Volumes = volumes
	if err := store.SaveStack(m); err != nil {
		t.Fatal(err)
	}
}

// registerContainersStack writes an adopted-containers-source manifest.
func registerContainersStack(t *testing.T, name string, containers []string) {
	t.Helper()
	m := store.Manifest{APIVersion: "kazi.dev/v1alpha1", Kind: "Stack"}
	m.Metadata.Name = name
	m.Spec.Source.Containers = containers
	if err := store.SaveStack(m); err != nil {
		t.Fatal(err)
	}
}

func joinCmds(cmds [][]string) string {
	var b strings.Builder
	for _, c := range cmds {
		b.WriteString(strings.Join(c, " "))
		b.WriteString("\n")
	}
	return b.String()
}

// TestSourceKindDispatch: image manifest — fresh state → `run -d --name
// kazi-app` with both kazi labels; container already present → only `start`.
func TestSourceKindDispatch(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	registerImageStack(t, "app", "nginx:alpine", false, nil, nil, nil)

	// Fresh: no container present. Image inspect returns no ports (not routable).
	f := &runtime.Fake{CmdOut: map[string]string{
		"image inspect": "{}",
	}}
	e := testEngine(t, f)
	if err := e.Up(t.Context(), "app"); err != nil {
		t.Fatal(err)
	}
	joined := joinCmds(f.Cmds)
	if !strings.Contains(joined, "run -d --name kazi-app") {
		t.Errorf("fresh up must record run -d --name kazi-app:\n%s", joined)
	}
	if !strings.Contains(joined, "--label kazi.managed=true") || !strings.Contains(joined, "--label kazi.stack=app") {
		t.Errorf("run must carry both kazi labels:\n%s", joined)
	}

	// Now the container exists → up should only start it.
	f2 := &runtime.Fake{
		Containers: []runtime.Container{
			{ID: "c1", Name: "kazi-app", Image: "nginx:alpine", State: "exited",
				Labels: map[string]string{labels.Managed: "true", labels.Stack: "app"}},
		},
		CmdOut: map[string]string{"image inspect": "{}"},
	}
	e2 := testEngine(t, f2)
	if err := e2.Up(t.Context(), "app"); err != nil {
		t.Fatal(err)
	}
	joined2 := joinCmds(f2.Cmds)
	if !strings.Contains(joined2, "start kazi-app") {
		t.Errorf("existing container up must record start kazi-app:\n%s", joined2)
	}
	if strings.Contains(joined2, "run -d") {
		t.Errorf("existing container up must not run -d:\n%s", joined2)
	}
}

// TestImageUpRoutable: routable image → network create + --network kazi +
// alias args + Caddyfile route.
func TestImageUpRoutable(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	registerImageStack(t, "app", "traefik/whoami", false, nil, nil, nil)
	f := &runtime.Fake{
		FailPrefix: []string{"network inspect"}, // force network create
		CmdOut: map[string]string{
			"image inspect": `{"80/tcp":{}}`,
		},
	}
	e := testEngine(t, f)
	if err := e.Up(t.Context(), "app"); err != nil {
		t.Fatal(err)
	}
	joined := joinCmds(f.Cmds)
	if !strings.Contains(joined, "network create kazi") {
		t.Errorf("routable image must create network:\n%s", joined)
	}
	if !strings.Contains(joined, "--network kazi") || !strings.Contains(joined, "--network-alias app.app") {
		t.Errorf("routable run must join kazi network with alias app.app:\n%s", joined)
	}
	b, err := os.ReadFile(filepath.Join(proxy.Dir(), "Caddyfile"))
	if err != nil || !strings.Contains(string(b), "app.localhost") {
		t.Errorf("Caddyfile must contain app.localhost: caddyfile=%s err=%v", b, err)
	}
}

// TestImageDownStops: down records `stop kazi-app`, never `rm`.
func TestImageDownStops(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	registerImageStack(t, "app", "nginx:alpine", false, nil, nil, nil)
	f := &runtime.Fake{}
	e := testEngine(t, f)
	if err := e.Down(t.Context(), "app"); err != nil {
		t.Fatal(err)
	}
	joined := joinCmds(f.Cmds)
	if !strings.Contains(joined, "stop kazi-app") {
		t.Errorf("image down must record stop kazi-app:\n%s", joined)
	}
	for _, c := range f.Cmds {
		if len(c) > 0 && c[0] == "rm" {
			t.Errorf("image down must never rm:\n%s", joined)
		}
	}
}

// TestContainersLifecycle: adopted two names — up=start a b; down=stop a b;
// never recreated (no run/rm ever).
func TestContainersLifecycle(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	registerContainersStack(t, "grp", []string{"a", "b"})
	f := &runtime.Fake{}
	e := testEngine(t, f)
	if err := e.Up(t.Context(), "grp"); err != nil {
		t.Fatal(err)
	}
	if err := e.Down(t.Context(), "grp"); err != nil {
		t.Fatal(err)
	}
	joined := joinCmds(f.Cmds)
	if !strings.Contains(joined, "start a b") {
		t.Errorf("containers up must record start a b:\n%s", joined)
	}
	if !strings.Contains(joined, "stop a b") {
		t.Errorf("containers down must record stop a b:\n%s", joined)
	}
	for _, c := range f.Cmds {
		if len(c) > 0 && (c[0] == "run" || c[0] == "rm") {
			t.Errorf("adopted containers must never run/rm:\n%s", joined)
		}
	}
}

func TestParseExposedPorts(t *testing.T) {
	cases := []struct {
		in   string
		want []int
	}{
		{`{"5432/tcp":{}}`, []int{5432}},
		{`{"80/tcp":{},"443/tcp":{}}`, []int{80, 443}},
		{`{"9000/tcp":{},"9001/tcp":{}}`, []int{9000, 9001}},
		{`{}`, nil},
		{``, nil},
		{`null`, nil},
		{`{"53/udp":{}}`, []int{53}},
	}
	for _, c := range cases {
		got := parseExposedPorts([]byte(c.in))
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("parseExposedPorts(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestParsePsPorts(t *testing.T) {
	cases := []struct {
		in   string
		want []int
	}{
		{"0.0.0.0:8080->80/tcp, 6379/tcp", []int{80, 6379}},
		{"0.0.0.0:8080->80/tcp", []int{80}},
		{"6379/tcp", []int{6379}},
		{"", nil},
		{"0.0.0.0:5432->5432/tcp, :::5432->5432/tcp", []int{5432}},
	}
	for _, c := range cases {
		got := parsePsPorts(c.in)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("parsePsPorts(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

// TestTemplateStackEnvInterpolation: template manifest + values → the recorded
// compose call's cmd.Env contains POSTGRES_DB=custom.
func TestTemplateStackEnvInterpolation(t *testing.T) {
	cfgDir := t.TempDir()
	t.Setenv("KAZI_CONFIG_DIR", cfgDir)
	// Materialize the embedded postgres template so its dir + values exist.
	// (template.Materialize copies embedded starters into the config dir.)
	m := store.Manifest{APIVersion: "kazi.dev/v1alpha1", Kind: "Stack"}
	m.Metadata.Name = "pg"
	m.Spec.Source.Template = "postgres"
	m.Spec.Values = map[string]string{"postgres_db": "custom"}
	if err := store.SaveStack(m); err != nil {
		t.Fatal(err)
	}
	f := &runtime.Fake{ConfigJSON: minimalConfigJSON}
	e := testEngine(t, f)
	if err := e.Up(t.Context(), "pg"); err != nil {
		t.Fatal(err)
	}
	// Find the up call's captured env.
	found := false
	for _, env := range f.Envs() {
		for _, kv := range env {
			if kv == "POSTGRES_DB=custom" {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("template compose calls must have POSTGRES_DB=custom in env; envs=%v", f.Envs())
	}
}

// TestImageStrategyPortMapping: imageStrategy renders -p correctly for both
// colon form ("8080:80" → `-p 8080:80`) and bare form ("8080" → `-p 8080:8080`).
func TestImageStrategyPortMapping(t *testing.T) {
	t.Run("colon form", func(t *testing.T) {
		t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
		registerImageStack(t, "app", "nginx:alpine", false, nil,
			[]store.ExposeSpec{{Service: "app", Port: "8080:80"}}, nil)
		f := &runtime.Fake{CmdOut: map[string]string{"image inspect": "{}"}}
		e := testEngine(t, f)
		if err := e.Up(t.Context(), "app"); err != nil {
			t.Fatal(err)
		}
		joined := joinCmds(f.Cmds)
		if !strings.Contains(joined, "-p 8080:80") {
			t.Errorf("colon-form port must render as -p 8080:80:\n%s", joined)
		}
		if strings.Contains(joined, "-p 8080:80:") {
			t.Errorf("port must not be double-expanded:\n%s", joined)
		}
	})

	t.Run("bare form", func(t *testing.T) {
		t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
		registerImageStack(t, "app2", "nginx:alpine", false, nil,
			[]store.ExposeSpec{{Service: "app2", Port: "9090"}}, nil)
		f := &runtime.Fake{CmdOut: map[string]string{"image inspect": "{}"}}
		e := testEngine(t, f)
		if err := e.Up(t.Context(), "app2"); err != nil {
			t.Fatal(err)
		}
		joined := joinCmds(f.Cmds)
		if !strings.Contains(joined, "-p 9090:9090") {
			t.Errorf("bare-form port must render as -p 9090:9090:\n%s", joined)
		}
	})
}

// TestImageDownExtraArgsRmsContainer verifies Finding 3: imageStrategy.down
// with no extraArgs issues `stop`; with extraArgs (teardown intent) issues `rm -f`.
func TestImageDownExtraArgsRmsContainer(t *testing.T) {
	t.Run("no extraArgs → stop", func(t *testing.T) {
		t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
		registerImageStack(t, "app", "nginx:alpine", false, nil, nil, nil)
		f := &runtime.Fake{}
		e := testEngine(t, f)
		if err := e.Down(t.Context(), "app"); err != nil {
			t.Fatal(err)
		}
		joined := joinCmds(f.Cmds)
		if !strings.Contains(joined, "stop kazi-app") {
			t.Errorf("down with no extraArgs must issue stop kazi-app:\n%s", joined)
		}
		for _, c := range f.Cmds {
			if len(c) > 0 && c[0] == "rm" {
				t.Errorf("down with no extraArgs must not rm:\n%s", joined)
			}
		}
	})

	t.Run("with extraArgs → rm -f", func(t *testing.T) {
		t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
		registerImageStack(t, "app", "nginx:alpine", true, nil, nil, nil)
		f := &runtime.Fake{}
		e := testEngine(t, f)
		if err := e.Teardown(t.Context(), "app"); err != nil {
			t.Fatal(err)
		}
		joined := joinCmds(f.Cmds)
		if !strings.Contains(joined, "rm -f kazi-app") {
			t.Errorf("teardown must issue rm -f kazi-app:\n%s", joined)
		}
		for _, c := range f.Cmds {
			if len(c) > 0 && c[0] == "stop" {
				t.Errorf("teardown must not stop:\n%s", joined)
			}
		}
	})
}

// TestSnapshotGroupsKaziStackLabel: a container with only kazi labels (no
// compose project) + a registered image manifest → registered, not unmanaged.
func TestSnapshotGroupsKaziStackLabel(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	registerImageStack(t, "app", "nginx:alpine", false, nil, nil, nil)
	f := &runtime.Fake{Containers: []runtime.Container{
		{ID: "c1", Name: "kazi-app", Image: "nginx:alpine", State: "running", Status: "Up 1 min",
			Labels: map[string]string{labels.Managed: "true", labels.Stack: "app"}},
	}}
	e := testEngine(t, f)
	stacks, loose, err := e.snapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(loose) != 0 {
		t.Errorf("kazi.stack-labeled container must not be loose: %+v", loose)
	}
	var appStack *StackInfo
	for i := range stacks {
		if stacks[i].Name == "app" {
			appStack = &stacks[i]
		}
	}
	if appStack == nil {
		t.Fatalf("app stack missing: %+v", stacks)
	}
	if appStack.Kind != KindRegistered {
		t.Errorf("app stack kind = %q, want registered", appStack.Kind)
	}
	if len(appStack.Containers) != 1 || appStack.Containers[0].Kind != KindRegistered {
		t.Errorf("app stack must claim its container as registered: %+v", appStack.Containers)
	}
}
