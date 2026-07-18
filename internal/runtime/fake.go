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
	Services   []string          // emitted by `config --services`
	ConfigJSON string            // emitted by `config --format json`
	PsErr      error
	Calls      [][]string        // recorded ComposeCmd calls: {project, dir, files..., args...}
	Cmds       [][]string        // recorded Cmd calls
	FailPrefix []string          // Cmd args joined → command fails if any prefix matches
	CmdOut     map[string]string // prefix -> stdout for Cmd
}

func (f *Fake) Name() string { return "fake" }

func (f *Fake) Ps(ctx context.Context) ([]Container, error) {
	return f.Containers, f.PsErr
}

func (f *Fake) ComposeCmd(ctx context.Context, project, dir string, files []string, args ...string) *exec.Cmd {
	call := append([]string{project, dir}, files...)
	call = append(call, args...)
	f.Calls = append(f.Calls, call)
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "config") && strings.Contains(joined, "json") {
		return exec.Command("echo", f.ConfigJSON)
	}
	if len(args) >= 2 && args[0] == "config" && args[1] == "--services" {
		return exec.Command("echo", strings.Join(f.Services, "\n"))
	}
	return exec.Command("true")
}

func (f *Fake) Cmd(ctx context.Context, args ...string) *exec.Cmd {
	f.Cmds = append(f.Cmds, args)
	key := strings.Join(args, " ")
	for _, p := range f.FailPrefix {
		if strings.HasPrefix(key, p) {
			return exec.Command("false")
		}
	}
	for p, out := range f.CmdOut {
		if strings.HasPrefix(key, p) {
			return exec.Command("echo", out)
		}
	}
	return exec.Command("true")
}
