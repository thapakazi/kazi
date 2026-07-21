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
	"github.com/thapakazi/kazi/internal/template"
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
	Cfg store.Config
	Out io.Writer // compose stdout passthrough
	Err io.Writer // compose stderr passthrough

	// host reads host CPU/mem/disk for HostStats; nil ⇒ the gopsutil-backed
	// default. Tests inject a fake provider so the mapping stays unit-testable
	// without touching the real OS.
	host hostProvider
}

func New(rt runtime.Runtime, cfg store.Config, out, errW io.Writer) *Engine {
	return &Engine{RT: rt, Cfg: cfg, Out: out, Err: errW}
}

// target is a resolved stack ready for lifecycle commands.
type target struct {
	name        string
	kind        Kind
	srcKind     string // "compose"|"image"|"template"|"containers"
	dir         string
	project     string
	composeFile string          // set only for compose/template stacks
	image       string          // image source stacks
	containers  []string        // adopted-container source stacks
	manifest    *store.Manifest // loaded once in resolve; nil for discovered
}

// resolve finds a stack by name: registered manifest first, then
// discovered compose project. Implements the project-naming rule: a
// registered stack reuses an existing project running from the same
// working dir, else gets the collision-proof kazi-<name>.
//
// The manifest's source arm (Source.Kind()) sets t.srcKind so lifecycle
// verbs dispatch per source; discovered stacks are always "compose".
func (e *Engine) resolve(ctx context.Context, name string) (target, error) {
	m, err := store.LoadStack(name)
	if err == nil {
		srcKind := m.Spec.Source.Kind()
		if srcKind == "" {
			srcKind = "compose"
		}
		mCopy := m
		t := target{
			name: name, kind: KindRegistered, srcKind: srcKind,
			project:    "kazi-" + name,
			image:      m.Spec.Source.Image,
			containers: m.Spec.Source.Containers,
			manifest:   &mCopy,
		}

		// Non-compose sources have no compose file: skip the on-disk check and
		// project reuse; their strategies operate on containers directly.
		switch srcKind {
		case "image", "containers":
			return t, nil
		case "template":
			dir, matErr := template.Materialize(m.Spec.Source.Template)
			if matErr != nil {
				return target{}, fmt.Errorf("stack %q: %w", name, matErr)
			}
			cf, cfErr := findComposeFile(dir)
			if cfErr != nil {
				return target{}, fmt.Errorf("stack %q template %q: %w", name, m.Spec.Source.Template, cfErr)
			}
			t.dir = dir
			t.composeFile = cf
			return t, nil
		}

		// compose source: verify the file still exists and reuse a live project.
		if _, statErr := os.Stat(m.Spec.Source.Compose); statErr != nil {
			return target{}, fmt.Errorf("stack %q manifest points at %s, which no longer exists; fix the path or `kazi rm %s`",
				name, m.Spec.Source.Compose, name)
		}
		t.dir = filepath.Dir(m.Spec.Source.Compose)
		t.composeFile = m.Spec.Source.Compose
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
				name: name, kind: KindDiscovered, srcKind: "compose",
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
