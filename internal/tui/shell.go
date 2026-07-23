package tui

import (
	"context"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/thapakazi/kazi/internal/engine"
)

// shellCmd suspends the TUI into an interactive shell and reports a
// shellExitedMsg when it returns. It's a package var so tests substitute a
// recorder for the real tea.ExecProcess suspend.
var shellCmd = runShell

// runShell wraps cmd (optionally with a return pause) and hands it to
// tea.ExecProcess, which suspends the dashboard, gives the child the real
// terminal, and restores on exit.
func runShell(cmd *exec.Cmd, stack, service string, pause bool) tea.Cmd {
	wrapped := wrapReturnPause(cmd, pause)
	return tea.ExecProcess(wrapped, func(err error) tea.Msg {
		return shellExitedMsg{stack: stack, service: service, err: err}
	})
}

// wrapReturnPause, when pause is true, rebuilds cmd as a host `sh -c` invocation
// that runs the original argv, then prints a prompt and waits for enter so the
// shell's final output stays on screen before the dashboard repaints. The
// inner command's exit status is preserved. cmd.Env/Dir carry over so template
// stacks' compose interpolation still resolves. pause=false runs cmd unchanged.
func wrapReturnPause(cmd *exec.Cmd, pause bool) *exec.Cmd {
	if !pause {
		return cmd
	}
	inner := shellJoin(cmd.Args)
	script := inner + `; __kazi_ec=$?; printf '\n\033[2m[kazi] press enter to return\033[0m '; read __kazi_x; exit $__kazi_ec`
	w := exec.Command("sh", "-c", script) //nolint:gosec // argv is single-quoted below
	w.Env = cmd.Env
	w.Dir = cmd.Dir
	return w
}

// shellJoin single-quotes each argv element so the host shell runs it literally —
// no expansion of the $(...)/pipes that belong to the container command.
func shellJoin(args []string) string {
	q := make([]string, len(args))
	for i, a := range args {
		q[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
	}
	return strings.Join(q, " ")
}

// shellTargetsCmd resolves the stack's running services for the shell picker.
func shellTargetsCmd(eng Engine, stack string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), cmdTimeout)
		defer cancel()
		st, err := eng.Status(ctx, stack)
		if err != nil {
			return shellTargetsMsg{stack: stack, err: err}
		}
		return shellTargetsMsg{stack: stack, services: runningShellTargets(st)}
	}
}

// runningShellTargets is the distinct set of services (in listing order) that
// have a running container — the values passed to ExecCommand. Image/compose
// services carry a Service label; adopted containers fall back to their name.
func runningShellTargets(st engine.StackInfo) []string {
	var out []string
	seen := map[string]bool{}
	for _, c := range st.Containers {
		if c.State != "running" {
			continue
		}
		svc := c.Service
		if svc == "" {
			svc = c.Name
		}
		if svc != "" && !seen[svc] {
			seen[svc] = true
			out = append(out, svc)
		}
	}
	return out
}

// shellInto builds the interactive shell command for one service and suspends
// into it, pausing on return unless returnImmediately is set. The command is
// bound to context.Background() (not a timeout ctx) because tea.ExecProcess runs
// it after this returns — a cancel here would kill the shell the instant it starts.
func (m Model) shellInto(stack, service string) tea.Cmd {
	c, err := m.eng.ExecCommand(context.Background(), stack, service, nil, engine.ExecOpts{})
	if err != nil {
		return m.setToast("shell: " + err.Error())
	}
	return shellCmd(c, stack, service, !m.returnImmediately)
}

// shellChoose dispatches the shell service picker selection at i.
func (m Model) shellChoose(i int) (tea.Model, tea.Cmd) {
	if i < 0 || i >= len(m.modal.values) {
		return m, nil
	}
	service := m.modal.values[i]
	stack := m.modal.stack
	m.modal = modalState{}
	return m, m.shellInto(stack, service)
}
