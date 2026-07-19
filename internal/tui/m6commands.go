package tui

import (
	"context"
	"os/exec"
	"regexp"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/thapakazi/kazi/internal/engine"
)

var dnsClean = regexp.MustCompile(`[^a-z0-9-]+`)

// dnsName derives a valid DNS-label stack name from a container name (lowercase,
// non-label chars → '-', collapsed, trimmed, capped at 63).
func dnsName(s string) string {
	s = dnsClean.ReplaceAllString(strings.ToLower(s), "-")
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	s = strings.Trim(s, "-")
	if len(s) > 63 {
		s = strings.Trim(s[:63], "-")
	}
	if s == "" {
		s = "adopted"
	}
	return s
}

// adoptCmd brings an unmanaged loose container under kazi as a stack named after
// it (a:adopt). Errors (e.g. a compose-project member) surface as a toast.
func adoptCmd(eng Engine, container string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), tryTimeout)
		defer cancel()
		name := dnsName(container)
		return actionDoneMsg{action: "adopt", stack: name, err: eng.Adopt(ctx, name, []string{container})}
	}
}

// tryTimeout bounds a try/gc engine call; compose up/teardown can take longer
// than a plain read.
const tryTimeout = 90 * time.Second

// applyHostname sets a custom *.localhost subdomain after a create when the
// user set a url different from the stack name; empty/default is a no-op.
func applyHostname(eng Engine, name, host string) error {
	if host == "" || host == name {
		return nil
	}
	return eng.SetHostname(name, host)
}

// addCmd registers a compose-backed stack via the engine Add path (create form,
// compose source), then applies the optional custom hostname.
func addCmd(eng Engine, name, path, host string) tea.Cmd {
	return func() tea.Msg {
		if _, err := eng.Add(name, path); err != nil {
			return createDoneMsg{name: name, err: err}
		}
		return createDoneMsg{name: name, err: applyHostname(eng, name, host)}
	}
}

// createTemplateCmd registers and starts a named, persistent template stack
// (create form, template source) via Try with an explicit name + Keep.
func createTemplateCmd(eng Engine, name, tmpl string, sets []string, host string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), tryTimeout)
		defer cancel()
		if _, _, err := eng.Try(ctx, tmpl, engine.TryOpts{Name: name, Keep: true, Detach: true, Sets: sets}); err != nil {
			return createDoneMsg{name: name, err: err}
		}
		return createDoneMsg{name: name, err: applyHostname(eng, name, host)}
	}
}

// runImageCmd registers and starts an image-backed stack (create form, image
// source) via RunImage, forwarding port/env/volume bindings plus an optional
// custom hostname and pinned HTTP route port (both set before up).
func runImageCmd(eng Engine, name, image string, ports, env, vols []string, host string, httpPort int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), tryTimeout)
		defer cancel()
		_, err := eng.RunImage(ctx, name, image, engine.RunOpts{
			Ports: ports, Envs: env, Vols: vols, Hostname: host, HTTPPort: httpPort,
		})
		return createDoneMsg{name: name, err: err}
	}
}

// tryCmd launches an ephemeral stack via `try -d --set …` (t:try form). The
// sets are the exact --set flags the CLI takes; nothing new on the engine side.
func tryCmd(eng Engine, tmpl string, sets []string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), tryTimeout)
		defer cancel()
		name, _, err := eng.Try(ctx, tmpl, engine.TryOpts{Detach: true, Sets: sets})
		return tryDoneMsg{name: name, err: err}
	}
}

// tryValuesCmd loads a template's values for the try form (Catalog t:try).
func tryValuesCmd(eng Engine, tmpl string) tea.Cmd {
	return func() tea.Msg {
		vals, err := eng.TryValues(tmpl)
		if err != nil {
			return errMsg{err}
		}
		return tryValuesMsg{tmpl: tmpl, values: vals}
	}
}

// routeFromCmd suggests static routes from a stack's published ports and adds
// them all (s-menu → route). Reports the count via a toast.
func routeFromCmd(eng Engine, stack string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), tryTimeout)
		defer cancel()
		cands, err := eng.RoutesFromStack(ctx, stack)
		if err != nil {
			return actionDoneMsg{action: "route", stack: stack, err: err}
		}
		for _, c := range cands {
			if aerr := eng.RouteAdd(ctx, c.Host, c.Port, c.Service+" from "+stack, stack); aerr != nil {
				return actionDoneMsg{action: "route", stack: stack, err: aerr}
			}
		}
		return routeDoneMsg{stack: stack, count: len(cands)}
	}
}

// removeContainerCmd force-removes an unmanaged loose container (d:remove on an
// unmanaged row). The result toasts + refreshes via actionDoneMsg.
func removeContainerCmd(eng Engine, name string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), tryTimeout)
		defer cancel()
		return actionDoneMsg{action: "remove", stack: name, err: eng.RemoveContainer(ctx, name)}
	}
}

// keepCmd promotes a watched ephemeral stack to persistent (k:keep).
func keepCmd(eng Engine, name string) tea.Cmd {
	return func() tea.Msg {
		return actionDoneMsg{action: "keep", stack: name, err: eng.Keep(name)}
	}
}

// gcCmd reclaims a watched ephemeral stack (g:gc) via Teardown — a zero-residue
// tear-down of exactly that stack.
func gcCmd(eng Engine, name string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), tryTimeout)
		defer cancel()
		return actionDoneMsg{action: "gc", stack: name, err: eng.Teardown(ctx, name)}
	}
}

// editTargetsCmd resolves the editable targets for a stack (e:edit).
func editTargetsCmd(eng Engine, name string) tea.Cmd {
	return func() tea.Msg {
		targets, err := eng.EditTargets(name)
		return editTargetsMsg{stack: name, targets: targets, err: err}
	}
}

// editValidateCmd runs a saved target's validator off the UI goroutine.
func editValidateCmd(target engine.EditTarget) tea.Cmd {
	return func() tea.Msg {
		if target.Validate == nil {
			return editValidatedMsg{err: nil}
		}
		ctx, cancel := context.WithTimeout(context.Background(), cmdTimeout)
		defer cancel()
		return editValidatedMsg{err: target.Validate(ctx)}
	}
}

// editorExec is the seam that suspends the TUI to $EDITOR on path and reports
// the editor's exit as an editorReturnedMsg. Tests replace it so the edit flow
// can be driven without a real terminal.
var editorExec = func(path string) tea.Cmd {
	editor := engine.ResolveEditor("")
	parts := strings.Fields(editor)
	c := exec.Command(parts[0], append(parts[1:], path)...) //nolint:gosec // user editor is intentional
	return tea.ExecProcess(c, func(err error) tea.Msg { return editorReturnedMsg{err: err} })
}
