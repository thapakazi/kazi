// Package compose runs pre-built runtime commands, streaming or capturing
// output and normalizing failures into typed ExitErrors.
package compose

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os/exec"
)

type ExitError struct {
	ExitCode int
	Stderr   string
}

func (e *ExitError) Error() string {
	return fmt.Sprintf("compose exited with code %d", e.ExitCode)
}

// Run executes cmd streaming stdout/stderr through untouched (the spec
// requires compose output to pass through as-is on up/down/logs).
func Run(cmd *exec.Cmd, stdout, stderr io.Writer) error {
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return wrapExit(cmd.Run(), "")
}

// Output executes cmd and returns captured stdout.
func Output(cmd *exec.Cmd) (string, error) {
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := wrapExit(cmd.Run(), errBuf.String()); err != nil {
		return "", err
	}
	return out.String(), nil
}

func wrapExit(err error, stderr string) error {
	if err == nil {
		return nil
	}
	var xe *exec.ExitError
	if errors.As(err, &xe) {
		return &ExitError{ExitCode: xe.ExitCode(), Stderr: stderr}
	}
	return err
}
