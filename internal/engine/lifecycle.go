package engine

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/thapakazi/kazi/internal/compose"
	"github.com/thapakazi/kazi/internal/labels"
)

// Up brings a stack up detached. For registered stacks kazi injects its
// labels through a generated override file (pure compose spec, portable
// across runtimes). compose up -d is already idempotent — an
// already-running stack exits 0.
func (e *Engine) Up(ctx context.Context, name string) error {
	t, err := e.resolve(ctx, name)
	if err != nil {
		return err
	}
	var files []string
	if t.kind == KindRegistered {
		override, err := e.writeOverride(ctx, t)
		if err != nil {
			return err
		}
		defer os.Remove(override)
		files = []string{t.composeFile, override}
	}
	return e.frame(compose.Run(e.RT.ComposeCmd(ctx, t.project, t.dir, files, "up", "-d"), e.Out, e.Err), "up", name)
}

// writeOverride asks compose for the service list, then renders the label
// override to a temp file. Caller removes it.
func (e *Engine) writeOverride(ctx context.Context, t target) (string, error) {
	out, err := compose.Output(e.RT.ComposeCmd(ctx, t.project, t.dir, []string{t.composeFile}, "config", "--services"))
	if err != nil {
		return "", e.frame(err, "config", t.name)
	}
	services := strings.Fields(out)
	f, err := os.CreateTemp("", "kazi-override-*.yml")
	if err != nil {
		return "", err
	}
	if _, err := f.Write(labels.OverrideYAML(t.name, services)); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), f.Close()
}

// Down stops and removes a stack's containers. Never passes -v in M0.
func (e *Engine) Down(ctx context.Context, name string) error {
	t, err := e.resolve(ctx, name)
	if err != nil {
		return err
	}
	return e.frame(compose.Run(e.RT.ComposeCmd(ctx, t.project, t.dir, t.files(), "down"), e.Out, e.Err), "down", name)
}

func (e *Engine) Restart(ctx context.Context, name string) error {
	t, err := e.resolve(ctx, name)
	if err != nil {
		return err
	}
	return e.frame(compose.Run(e.RT.ComposeCmd(ctx, t.project, t.dir, t.files(), "restart"), e.Out, e.Err), "restart", name)
}

// Logs streams compose logs; service may be empty for all services.
func (e *Engine) Logs(ctx context.Context, name, service string, follow bool, tail string) error {
	t, err := e.resolve(ctx, name)
	if err != nil {
		return err
	}
	args := []string{"logs"}
	if follow {
		args = append(args, "-f")
	}
	if tail != "" {
		args = append(args, "--tail", tail)
	}
	if service != "" {
		args = append(args, service)
	}
	return e.frame(compose.Run(e.RT.ComposeCmd(ctx, t.project, t.dir, t.files(), args...), e.Out, e.Err), "logs", name)
}

// files returns the -f list for lifecycle verbs: the manifest's compose
// file for registered stacks, nothing for discovered (compose finds the
// file in the project directory).
func (t target) files() []string {
	if t.composeFile != "" {
		return []string{t.composeFile}
	}
	return nil
}

// frame wraps a compose failure with one trailing line of context (what
// failed, which stack, which runtime) without swallowing streamed output.
func (e *Engine) frame(err error, verb, stack string) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("`%s compose %s` failed for stack %q: %w", e.RT.Name(), verb, stack, err)
}
