package engine

import (
	"encoding/json"
	"testing"
)

// M9 Phase 0: ExecResult is the --json / MCP wire shape. Lock its JSON field
// names so the CLI envelope and the (later) MCP tool stay in sync.
func TestExecResultJSONShape(t *testing.T) {
	r := ExecResult{ExitCode: 3, Stdout: "out", Stderr: "err"}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	want := `{"exitCode":3,"stdout":"out","stderr":"err"}`
	if got != want {
		t.Errorf("ExecResult JSON = %s, want %s", got, want)
	}
}

// ExecOpts carries the docker-exec-style knobs; a zero value means "login-shell
// probe, current user/workdir, index 1 resolved downstream".
func TestExecOptsZeroValue(t *testing.T) {
	var o ExecOpts
	if o.Shell != "" || o.User != "" || o.Workdir != "" || o.TTY || o.Index != 0 || o.Env != nil {
		t.Errorf("ExecOpts zero value not clean: %+v", o)
	}
}
