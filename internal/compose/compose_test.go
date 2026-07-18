package compose

import (
	"bytes"
	"errors"
	"os/exec"
	"strings"
	"testing"
)

func TestRunStreamsAndSucceeds(t *testing.T) {
	var out, errBuf bytes.Buffer
	err := Run(exec.Command("sh", "-c", "echo hello; echo oops >&2"), &out, &errBuf)
	if err != nil {
		t.Fatal(err)
	}
	if out.String() != "hello\n" || errBuf.String() != "oops\n" {
		t.Errorf("out=%q err=%q", out.String(), errBuf.String())
	}
}

func TestRunExitError(t *testing.T) {
	var out, errBuf bytes.Buffer
	err := Run(exec.Command("sh", "-c", "exit 3"), &out, &errBuf)
	var xe *ExitError
	if !errors.As(err, &xe) || xe.ExitCode != 3 {
		t.Fatalf("want ExitError{3}, got %v", err)
	}
}

func TestOutputCaptures(t *testing.T) {
	got, err := Output(exec.Command("sh", "-c", "printf 'web\\ndb\\n'"))
	if err != nil || got != "web\ndb\n" {
		t.Errorf("got %q err %v", got, err)
	}
}

func TestOutputExitErrorWithStderr(t *testing.T) {
	_, err := Output(exec.Command("sh", "-c", "echo bad >&2; exit 5"))
	var xe *ExitError
	if !errors.As(err, &xe) || xe.ExitCode != 5 || !strings.Contains(xe.Stderr, "bad") {
		t.Fatalf("got %v", err)
	}
}
