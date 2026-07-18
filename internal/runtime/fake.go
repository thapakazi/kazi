package runtime

import (
	"context"
	"os/exec"
	"strings"
)

// Fake is a scripted Runtime for engine unit tests: canned ps output,
// recorded compose invocations, no real containers involved.
type Fake struct {
	Containers []Container
	Services   []string // emitted by `config --services`
	PsErr      error
	Calls      [][]string // each: {project, dir, files..., args...}
}

func (f *Fake) Name() string { return "fake" }

func (f *Fake) Ps(ctx context.Context) ([]Container, error) {
	return f.Containers, f.PsErr
}

func (f *Fake) ComposeCmd(ctx context.Context, project, dir string, files []string, args ...string) *exec.Cmd {
	call := append([]string{project, dir}, files...)
	call = append(call, args...)
	f.Calls = append(f.Calls, call)
	if len(args) >= 2 && args[0] == "config" && args[1] == "--services" {
		return exec.Command("echo", strings.Join(f.Services, "\n"))
	}
	return exec.Command("true")
}
