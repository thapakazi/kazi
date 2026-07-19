package engine

import (
	"testing"

	"github.com/thapakazi/kazi/internal/labels"
	"github.com/thapakazi/kazi/internal/runtime"
)

func TestStackEnvPerContainerSortedByService(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	f := &runtime.Fake{
		Containers: []runtime.Container{
			{Name: "app_web_1", Labels: map[string]string{
				labels.ComposeProject: "app", labels.ComposeService: "web",
			}},
			{Name: "app_db_1", Labels: map[string]string{
				labels.ComposeProject: "app", labels.ComposeService: "db",
			}},
		},
		CmdOut: map[string]string{
			"inspect --format {{json .Config.Env}} app_web_1": `["PATH=/bin","PORT=80"]`,
			// db has no env → JSON null.
			"inspect --format {{json .Config.Env}} app_db_1": `null`,
		},
	}
	e := testEngine(t, f)

	envs, err := e.StackEnv(t.Context(), "app")
	if err != nil {
		t.Fatal(err)
	}
	if len(envs) != 2 {
		t.Fatalf("want 2 containers, got %d: %+v", len(envs), envs)
	}
	// Sorted by service: db before web.
	if envs[0].Service != "db" || envs[1].Service != "web" {
		t.Fatalf("order = %s,%s; want db,web", envs[0].Service, envs[1].Service)
	}
	if len(envs[0].Env) != 0 {
		t.Errorf("db env = %v, want empty (null inspects to none)", envs[0].Env)
	}
	// web env is sorted by key: PATH before PORT.
	if len(envs[1].Env) != 2 || envs[1].Env[0] != "PATH=/bin" || envs[1].Env[1] != "PORT=80" {
		t.Errorf("web env = %v, want sorted [PATH,PORT]", envs[1].Env)
	}
}

func TestStackEnvIsolatesInspectFailure(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	f := &runtime.Fake{
		Containers: []runtime.Container{
			{Name: "app_web_1", Labels: map[string]string{
				labels.ComposeProject: "app", labels.ComposeService: "web",
			}},
		},
		FailPrefix: []string{"inspect"},
	}
	e := testEngine(t, f)

	envs, err := e.StackEnv(t.Context(), "app")
	if err != nil {
		t.Fatal(err) // one bad container must not fail the whole read
	}
	if len(envs) != 1 || envs[0].Err == "" {
		t.Fatalf("want one entry carrying an Err, got %+v", envs)
	}
}
