package main

import (
	"errors"
	"reflect"
	"testing"

	"github.com/thapakazi/kazi/internal/engine"
)

func TestParseExecArgs(t *testing.T) {
	cases := []struct {
		name        string
		args        []string
		dash        int
		wantStack   string
		wantService string
		wantCommand []string
		wantErr     bool
	}{
		{"shell only", []string{"blog", "db"}, -1, "blog", "db", nil, false},
		{"with command", []string{"blog", "db", "pg_isready", "-q"}, 2, "blog", "db", []string{"pg_isready", "-q"}, false},
		{"empty command after dash", []string{"blog", "db"}, 2, "blog", "db", nil, false},
		{"too few positional", []string{"blog"}, -1, "", "", nil, true},
		{"command but missing service", []string{"blog", "ls"}, 1, "", "", nil, true},
		{"too many positional", []string{"blog", "db", "web"}, -1, "", "", nil, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			stack, service, command, err := parseExecArgs(c.args, c.dash)
			if c.wantErr {
				if !errors.Is(err, ErrUsage) {
					t.Fatalf("err = %v, want ErrUsage", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err %v", err)
			}
			if stack != c.wantStack || service != c.wantService || !reflect.DeepEqual(command, c.wantCommand) {
				t.Errorf("got (%q,%q,%v), want (%q,%q,%v)", stack, service, command, c.wantStack, c.wantService, c.wantCommand)
			}
		})
	}
}

// Service resolution failures map to exit 3; a passed-through command exit code
// is carried verbatim via exitError.
func TestExecExitCodes(t *testing.T) {
	if got := exitCode(engine.ErrServiceNotFound); got != 3 {
		t.Errorf("ErrServiceNotFound ⇒ %d, want 3", got)
	}
	if got := exitCode(engine.ErrServiceNotRunning); got != 3 {
		t.Errorf("ErrServiceNotRunning ⇒ %d, want 3", got)
	}
	if got := exitCode(&exitError{code: 7}); got != 7 {
		t.Errorf("exitError{7} ⇒ %d, want 7", got)
	}
}

func TestExecErrCodes(t *testing.T) {
	if got := errCode(engine.ErrServiceNotFound); got != "service_not_found" {
		t.Errorf("ErrServiceNotFound ⇒ %q", got)
	}
	if got := errCode(engine.ErrServiceNotRunning); got != "service_not_running" {
		t.Errorf("ErrServiceNotRunning ⇒ %q", got)
	}
}
