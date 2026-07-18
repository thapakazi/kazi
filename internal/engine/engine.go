// Package engine is kazi's single facade: all logic lives here, behind
// typed results that every skin (CLI now; MCP/TUI later) renders.
package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/thapakazi/kazi/internal/labels"
	"github.com/thapakazi/kazi/internal/runtime"
	"github.com/thapakazi/kazi/internal/store"
)

var ErrStackNotFound = errors.New("stack not found")

type Kind string

const (
	KindRegistered Kind = "registered"
	KindDiscovered Kind = "discovered"
	KindUnmanaged  Kind = "unmanaged"
)

type ContainerInfo struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Image   string `json:"image"`
	Service string `json:"service,omitempty"`
	State   string `json:"state"`
	Status  string `json:"status"`
	Health  string `json:"health"`
	Ports   string `json:"ports,omitempty"`
	Stack   string `json:"stack,omitempty"`
	Kind    Kind   `json:"kind"`
}

type StackInfo struct {
	Name       string          `json:"name"`
	Kind       Kind            `json:"kind"`
	Dir        string          `json:"dir,omitempty"`
	Project    string          `json:"project,omitempty"`
	Running    int             `json:"running"`
	Total      int             `json:"total"`
	Containers []ContainerInfo `json:"containers,omitempty"`
}

type Engine struct {
	RT  runtime.Runtime
	Out io.Writer // compose stdout passthrough
	Err io.Writer // compose stderr passthrough
}

func New(rt runtime.Runtime, out, errW io.Writer) *Engine {
	return &Engine{RT: rt, Out: out, Err: errW}
}

// target is a resolved stack ready for lifecycle commands.
type target struct {
	name        string
	kind        Kind
	dir         string
	project     string
	composeFile string // set only for registered stacks
}

// resolve finds a stack by name: registered manifest first, then
// discovered compose project. Implements the project-naming rule: a
// registered stack reuses an existing project running from the same
// working dir, else gets the collision-proof kazi-<name>.
func (e *Engine) resolve(ctx context.Context, name string) (target, error) {
	m, err := store.LoadStack(name)
	if err == nil {
		if _, statErr := os.Stat(m.Spec.Source.Compose); statErr != nil {
			return target{}, fmt.Errorf("stack %q manifest points at %s, which no longer exists; fix the path or `kazi rm %s`",
				name, m.Spec.Source.Compose, name)
		}
		t := target{
			name: name, kind: KindRegistered,
			dir:         filepath.Dir(m.Spec.Source.Compose),
			composeFile: m.Spec.Source.Compose,
			project:     "kazi-" + name,
		}
		cs, psErr := e.RT.Ps(ctx)
		if psErr != nil {
			return target{}, psErr
		}
		// kazi assumes at most one registered stack per working directory.
		// Two manifests pointing at the same dir would both reuse the same live
		// project here (tracked for a future milestone; snapshot()'s claimed map
		// guards the view side of this assumption).
		for _, c := range cs {
			if c.Labels[labels.ComposeWorkingDir] == t.dir && c.Labels[labels.ComposeProject] != "" {
				t.project = c.Labels[labels.ComposeProject]
				break
			}
		}
		return t, nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return target{}, err
	}
	cs, psErr := e.RT.Ps(ctx)
	if psErr != nil {
		return target{}, psErr
	}
	for _, c := range cs {
		if c.Labels[labels.ComposeProject] == name {
			return target{
				name: name, kind: KindDiscovered,
				dir:     c.Labels[labels.ComposeWorkingDir],
				project: name,
			}, nil
		}
	}
	return target{}, fmt.Errorf("%w: %s", ErrStackNotFound, name)
}

// healthOf extracts M0-depth health from the runtime's status string:
// compose healthcheck result when defined, else "-" (state itself is a
// separate column).
func healthOf(status string) string {
	switch {
	case strings.Contains(status, "health: starting"):
		return "starting"
	case strings.Contains(status, "(unhealthy)"):
		return "unhealthy"
	case strings.Contains(status, "(healthy)"):
		return "healthy"
	default:
		return "-"
	}
}
