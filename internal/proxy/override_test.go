package proxy

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestRenderOverrideRoutableAndPorts(t *testing.T) {
	got := string(RenderOverride("blog", []OverrideService{
		{Name: "web", Routable: true, Alias: "web.blog"},
		{Name: "db", Ports: []string{"42017:5432"}},
	}))
	want := `services:
  "web":
    labels:
      kazi.managed: "true"
      kazi.stack: "blog"
    networks:
      "default": {}
      "kazi":
        aliases:
          - "web.blog"
  "db":
    labels:
      kazi.managed: "true"
      kazi.stack: "blog"
    ports:
      - "42017:5432"
networks:
  "kazi":
    external: true
`
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	var parsed map[string]any
	if err := yaml.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatalf("override is not valid YAML: %v", err)
	}
}

func TestRenderOverridePreservesExistingNetworks(t *testing.T) {
	got := string(RenderOverride("blog", []OverrideService{
		{Name: "web", Routable: true, Alias: "web.blog", Networks: []string{"backend", "frontend"}},
	}))
	for _, w := range []string{`"backend": {}`, `"frontend": {}`, `"kazi":`} {
		if !strings.Contains(got, w) {
			t.Errorf("missing %q in:\n%s", w, got)
		}
	}
	if strings.Contains(got, `"default": {}`) {
		t.Errorf("default must not be added when service has explicit networks:\n%s", got)
	}
}

func TestRenderOverridePlainLabelsOnly(t *testing.T) {
	got := string(RenderOverride("blog", []OverrideService{{Name: "worker"}}))
	if strings.Contains(got, "networks") || strings.Contains(got, "ports") {
		t.Errorf("non-routable service must get labels only:\n%s", got)
	}
	if !strings.Contains(got, `kazi.stack: "blog"`) {
		t.Errorf("labels missing:\n%s", got)
	}
}
