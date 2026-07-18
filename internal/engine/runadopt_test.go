package engine

import (
	"errors"
	"strings"
	"testing"

	"github.com/thapakazi/kazi/internal/labels"
	"github.com/thapakazi/kazi/internal/runtime"
	"github.com/thapakazi/kazi/internal/store"
)

// TestRunImageCreatesManifestAndContainer: manifest arms right, `run -d`
// recorded with `-p 8080:80`, `-e`, `-v`, labels; second Up records `start` only.
func TestRunImageCreatesManifestAndContainer(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())

	f := &runtime.Fake{
		CmdOut: map[string]string{
			"image inspect": "{}",
		},
	}
	e := testEngine(t, f)

	name, err := e.RunImage(t.Context(), "myapp", "nginx:alpine",
		[]string{"8080:80"}, []string{"DEBUG=true"}, []string{"data:/app/data"})
	if err != nil {
		t.Fatal(err)
	}
	if name != "myapp" {
		t.Errorf("name = %q, want myapp", name)
	}

	// Manifest must have been written with the correct arms.
	m, err := store.LoadStack("myapp")
	if err != nil {
		t.Fatal(err)
	}
	if m.Spec.Source.Image != "nginx:alpine" {
		t.Errorf("source.image = %q, want nginx:alpine", m.Spec.Source.Image)
	}
	if len(m.Spec.Expose) == 0 || m.Spec.Expose[0].Port != "8080:80" {
		t.Errorf("expose = %+v, want port 8080:80 (full mapping)", m.Spec.Expose)
	}
	if m.Spec.Values["debug"] != "true" {
		t.Errorf("values = %v, want debug=true", m.Spec.Values)
	}
	if len(m.Spec.Volumes) == 0 || m.Spec.Volumes[0] != "data:/app/data" {
		t.Errorf("volumes = %v, want data:/app/data", m.Spec.Volumes)
	}

	// `run -d` must be recorded with the right flags.
	joined := joinCmds(f.Cmds)
	if !strings.Contains(joined, "run -d --name kazi-myapp") {
		t.Errorf("run -d --name kazi-myapp not recorded:\n%s", joined)
	}
	if !strings.Contains(joined, "-p 8080:80") {
		t.Errorf("expected -p 8080:80:\n%s", joined)
	}
	if !strings.Contains(joined, "-e DEBUG=true") {
		t.Errorf("expected -e DEBUG=true:\n%s", joined)
	}
	if !strings.Contains(joined, "-v data:/app/data") {
		t.Errorf("expected -v data:/app/data:\n%s", joined)
	}
	if !strings.Contains(joined, "--label "+labels.Managed+"=true") {
		t.Errorf("expected kazi.managed=true label:\n%s", joined)
	}
	if !strings.Contains(joined, "--label "+labels.Stack+"=myapp") {
		t.Errorf("expected kazi.stack=myapp label:\n%s", joined)
	}

	// Second Up must record `start` only, not `run -d`.
	f2 := &runtime.Fake{
		Containers: []runtime.Container{
			{ID: "c1", Name: "kazi-myapp", Image: "nginx:alpine", State: "exited",
				Labels: map[string]string{labels.Managed: "true", labels.Stack: "myapp"}},
		},
		CmdOut: map[string]string{"image inspect": "{}"},
	}
	e2 := testEngine(t, f2)
	if err := e2.Up(t.Context(), "myapp"); err != nil {
		t.Fatal(err)
	}
	joined2 := joinCmds(f2.Cmds)
	if !strings.Contains(joined2, "start kazi-myapp") {
		t.Errorf("second Up must record start kazi-myapp:\n%s", joined2)
	}
	if strings.Contains(joined2, "run -d") {
		t.Errorf("second Up must not run -d:\n%s", joined2)
	}
}

// TestRunImageDerivesName: ghcr.io/acme/My_App:v2 → "my-app".
func TestRunImageDerivesName(t *testing.T) {
	cases := []struct {
		image string
		want  string
	}{
		{"ghcr.io/acme/My_App:v2", "my-app"},
		{"nginx:latest", "nginx"},
		{"nginx", "nginx"},
		{"postgres:15-alpine", "postgres"},
		{"my_image:v1.2.3", "my-image"},
		{"UPPER:tag", "upper"},
		{"foo@sha256:abc123", "foo"},
		{"registry.example.com/org/Sub_App:1.0", "sub-app"},
		{"--bad", "bad"}, // leading dash stripped
		{"bad--", "bad"}, // trailing dash stripped
	}
	for _, c := range cases {
		got := deriveImageName(c.image)
		if got != c.want {
			t.Errorf("deriveImageName(%q) = %q, want %q", c.image, got, c.want)
		}
	}
}

// TestAdoptRejectsComposeContainers: error mentions the compose project.
func TestAdoptRejectsComposeContainers(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())

	f := &runtime.Fake{
		Containers: []runtime.Container{
			{
				ID: "c1", Name: "web1", Image: "nginx", State: "running",
				Labels: map[string]string{
					labels.ComposeProject: "myproject",
				},
			},
		},
	}
	e := testEngine(t, f)

	err := e.Adopt(t.Context(), "grp", []string{"web1"})
	if err == nil {
		t.Fatal("expected error for compose-labeled container")
	}
	if !strings.Contains(err.Error(), "myproject") {
		t.Errorf("error must mention compose project %q: %v", "myproject", err)
	}
	if !strings.Contains(err.Error(), "web1") {
		t.Errorf("error must mention container name %q: %v", "web1", err)
	}
}

// TestAdoptConnectsHTTPContainers: `network connect` with `--alias web1.grp`
// recorded for a container exposing 8080; NOT recorded for 5432.
func TestAdoptConnectsHTTPContainers(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())

	f := &runtime.Fake{
		Containers: []runtime.Container{
			{ID: "c1", Name: "web1", Image: "nginx", State: "running",
				Ports:  "0.0.0.0:8080->80/tcp",
				Labels: map[string]string{}},
			{ID: "c2", Name: "db1", Image: "postgres", State: "running",
				Ports:  "5432/tcp",
				Labels: map[string]string{}},
		},
	}
	e := testEngine(t, f)

	if err := e.Adopt(t.Context(), "grp", []string{"web1", "db1"}); err != nil {
		t.Fatal(err)
	}

	// `network connect --alias web1.grp kazi web1` must be recorded.
	joined := joinCmds(f.Cmds)
	if !strings.Contains(joined, "network connect") || !strings.Contains(joined, "--alias web1.grp") {
		t.Errorf("expected network connect --alias web1.grp for HTTP container:\n%s", joined)
	}
	// `network connect` must NOT be recorded for db1 (port 5432 is TCP, not HTTP).
	if strings.Contains(joined, "--alias db1.grp") {
		t.Errorf("network connect must NOT be recorded for TCP-only container db1:\n%s", joined)
	}
}

// TestAdoptUnknownContainer: error when container doesn't exist.
func TestAdoptUnknownContainer(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())

	f := &runtime.Fake{Containers: []runtime.Container{}}
	e := testEngine(t, f)

	err := e.Adopt(t.Context(), "grp", []string{"missing-container"})
	if err == nil {
		t.Fatal("expected error for unknown container")
	}
	if !strings.Contains(err.Error(), "missing-container") {
		t.Errorf("error must mention unknown container name: %v", err)
	}
}

// TestRunImagePortValidation: invalid port mapping strings are rejected early.
func TestRunImagePortValidation(t *testing.T) {
	cases := []struct {
		port    string
		wantErr bool
	}{
		{"8080:80", false},
		{"8080", false},
		{"80:80", false},
		{"bad:80", true},
		{"8080:bad", true},
		{"8080:80:extra", true}, // three-part is invalid (SplitN(2) makes it "8080" + "80:extra")
		{"0:80", true},          // port 0 invalid
	}
	for _, tc := range cases {
		t.Run(tc.port, func(t *testing.T) {
			t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
			f := &runtime.Fake{CmdOut: map[string]string{"image inspect": "{}"}}
			e := testEngine(t, f)
			_, err := e.RunImage(t.Context(), "app", "nginx:alpine", []string{tc.port}, nil, nil)
			if tc.wantErr && err == nil {
				t.Errorf("port %q: expected error, got nil", tc.port)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("port %q: unexpected error: %v", tc.port, err)
			}
		})
	}
}

// TestRunImageBarePort: bare port "8080" stores "8080" and renders -p 8080:8080.
func TestRunImageBarePort(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	f := &runtime.Fake{CmdOut: map[string]string{"image inspect": "{}"}}
	e := testEngine(t, f)
	_, err := e.RunImage(t.Context(), "myapp", "nginx:alpine", []string{"8080"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	m, err := store.LoadStack("myapp")
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Spec.Expose) == 0 || m.Spec.Expose[0].Port != "8080" {
		t.Errorf("expose = %+v, want port 8080 (bare)", m.Spec.Expose)
	}
	joined := joinCmds(f.Cmds)
	if !strings.Contains(joined, "-p 8080:8080") {
		t.Errorf("bare port must render as -p 8080:8080:\n%s", joined)
	}
}

// TestAdoptedRmManifestOnly: Remove on an adopted stack leaves runtime
// untouched — no stop/rm calls.
func TestAdoptedRmManifestOnly(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())

	// Pre-register an adopted stack.
	m := store.Manifest{APIVersion: "kazi.dev/v1alpha1", Kind: "Stack"}
	m.Metadata.Name = "grp"
	m.Spec.Source.Containers = []string{"web1", "db1"}
	if err := store.SaveStack(m); err != nil {
		t.Fatal(err)
	}

	f := &runtime.Fake{}
	e := testEngine(t, f)

	if err := e.Remove("grp"); err != nil {
		t.Fatal(err)
	}

	// Manifest must be gone.
	if _, err := store.LoadStack("grp"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("manifest must be deleted after Remove, got err=%v", err)
	}

	// Runtime must not have been touched.
	for _, cmd := range f.Cmds {
		if len(cmd) > 0 && (cmd[0] == "stop" || cmd[0] == "rm") {
			t.Errorf("adopted Remove must not call stop/rm: got cmd %v", cmd)
		}
	}
}
