// Package tui is kazi's bubbletea dashboard skin. It is a pure view/controller
// over the internal/engine facade: it reads via the Engine interface below and
// (in wave 2) will write via the same facade. No orchestration logic lives here.
package tui

import (
	"context"
	"io"

	"github.com/thapakazi/kazi/internal/engine"
	"github.com/thapakazi/kazi/internal/store"
	"github.com/thapakazi/kazi/internal/template"
)

// Engine is the read seam the TUI (and its tests) depend on. It lists exactly
// the engine reads the dashboard needs; *engine.Engine satisfies it. Keeping
// this an interface lets tests drive the model with a fake engine and lets
// wave-2 action wiring grow behind the same boundary.
type Engine interface {
	List(ctx context.Context) ([]engine.StackInfo, error)
	Ps(ctx context.Context) ([]engine.ContainerInfo, error)
	Status(ctx context.Context, name string) (engine.StackInfo, error)
	Describe(ctx context.Context, name string) (engine.StackDetail, error)
	Urls(ctx context.Context, name string) ([]engine.Endpoint, error)

	// StackEnv returns the environment of every container in a stack (Env tab).
	// Env is fixed at container creation, so the dashboard caches it per stack.
	StackEnv(ctx context.Context, name string) ([]engine.ContainerEnv, error)
	GcDebris(ctx context.Context) int
	TemplateList() ([]template.Info, error)

	// LogStream follows a stack's logs, returning a reader over the combined
	// output plus a cancel func to stop following. The Logs tab tails it live;
	// opts carries tail/since for the m5-log features.
	LogStream(ctx context.Context, name, service string, opts engine.LogStreamOpts) (io.ReadCloser, context.CancelFunc, error)

	// Remove deregisters a registered stack (deletes its manifest; never touches
	// containers). Backs the guarded d:remove → r (deregister) action.
	Remove(name string) error

	// RemoveContainer force-removes a single unmanaged loose container by name
	// (docker rm -f). Backs d:remove on an unmanaged row.
	RemoveContainer(ctx context.Context, name string) error

	// Adopt brings loose containers under kazi as a named stack. Backs a:adopt on
	// an unmanaged row.
	Adopt(ctx context.Context, name string, containers []string) error

	// ActionStream runs a lifecycle verb (up/down/restart) with its compose
	// output captured to a reader (so it never scribbles over the TUI) and the
	// final error on the channel at EOF. Backs the s:menu lifecycle actions and
	// the Action panel.
	ActionStream(ctx context.Context, action, name string) (io.ReadCloser, <-chan error)

	// Add registers a compose-backed stack (name + compose path). Backs the
	// n:new create form; identical to the `kazi add` engine path.
	Add(name, path string) (store.Manifest, error)

	// EditTargets resolves the user-owned files editable for a stack (manifest
	// always; compose file for compose-backed stacks). Backs the e:edit flow.
	EditTargets(name string) ([]engine.EditTarget, error)

	// TryValues loads a template's values (with must-change flags) for the t:try
	// values form. Try launches the ephemeral stack via `try -d --set …`; with
	// opts.Name + Keep it registers a named persistent template stack (create form).
	TryValues(tmpl string) ([]engine.TryValue, error)
	Try(ctx context.Context, tmpl string, opts engine.TryOpts) (string, []engine.Endpoint, error)

	// RunImage registers and starts an image-backed stack (create form → image),
	// including an optional custom hostname / pinned HTTP port for routing.
	RunImage(ctx context.Context, name, image string, opts engine.RunOpts) (string, error)

	// SetHostname sets a stack's custom *.localhost subdomain (create form → url
	// field). Empty clears the override back to the stack name.
	SetHostname(name, host string) error

	// Keep promotes a watched ephemeral stack to persistent; Teardown reclaims
	// (gc) it. Back the k:keep / g:gc actions on a live ephemeral stack.
	Keep(name string) error
	Teardown(ctx context.Context, name string) error

	// RoutesFromStack suggests static routes from a stack's published ports;
	// RouteAdd registers one. Back the s-menu "route" action (expose an
	// externally-run stack's ports as *.localhost URLs).
	RoutesFromStack(ctx context.Context, stack string) ([]engine.RouteCandidate, error)
	RouteAdd(ctx context.Context, host string, port int, note, stack string) error
}

// Compile-time assertion that the real engine satisfies the TUI seam.
var _ Engine = (*engine.Engine)(nil)
