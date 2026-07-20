package tui

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/thapakazi/kazi/internal/engine"
)

// browserOpen launches the OS default handler for a URL. It's a package var so
// tests can substitute a recorder instead of spawning a real browser.
var browserOpen = openURL

// editorOpen returns the tea.Cmd that opens path in the resolved editor
// ($EDITOR → $VISUAL → vi). GUI editors launch detached so the TUI keeps
// rendering; terminal editors (vim, nano, emacs -nw, …) need a TTY, so the TUI
// suspends via tea.ExecProcess and restores when the editor exits. Both report
// an editorOpenedMsg. It's a package var so tests can substitute a recorder.
var editorOpen = openInEditor

// emacsClientPath finds emacsclient on PATH. It's a var so tests can drive the
// editorPlan decision without a real emacsclient installed.
var emacsClientPath = func() (string, bool) {
	p, err := exec.LookPath("emacsclient")
	if err != nil {
		return "", false
	}
	return p, true
}

// openInEditor builds the command to open path (a config file or a project
// directory) and either runs it detached (GUI editors) or suspends the TUI for
// it (terminal editors), per editorPlan.
func openInEditor(path string) tea.Cmd {
	name, args, detach := editorPlan(engine.ResolveEditor(""), path)
	if detach {
		return func() tea.Msg {
			c := exec.Command(name, args...) //nolint:gosec // user editor is intentional
			return editorOpenedMsg{path: path, err: c.Start()}
		}
	}
	c := exec.Command(name, args...) //nolint:gosec // user editor is intentional
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return editorOpenedMsg{path: path, err: err}
	})
}

// guiEditors open their own window and are launched detached. Anything not
// listed here (and not emacs) is treated as a terminal editor: it needs a real
// TTY, so the TUI suspends for it rather than handing it /dev/null.
var guiEditors = map[string]bool{
	"code": true, "code-insiders": true, "codium": true, "vscodium": true,
	"subl": true, "sublime_text": true, "gvim": true, "mvim": true,
	"nvim-qt": true, "neovide": true, "gedit": true, "kate": true,
	"kwrite": true, "zed": true, "cursor": true, "gnome-text-editor": true,
	"xed": true, "pluma": true, "geany": true, "notepad++": true, "notepad": true,
}

// editorPlan resolves how to open path in editor: the command + args to run and
// whether it can run detached (true) or needs a TUI suspend for a TTY (false).
// emacs is special-cased to reuse a running GUI emacs via emacsclient unless a
// terminal frame (-nw/-t) was explicitly requested. Unknown editors default to
// the terminal path, since $EDITOR is most often vim/nano/vi.
func editorPlan(editor, path string) (name string, args []string, detach bool) {
	parts := strings.Fields(editor)
	if len(parts) == 0 {
		parts = []string{"vi"}
	}
	base := filepath.Base(parts[0])
	if base == "emacs" && !hasTerminalFlag(parts[1:]) {
		if client, ok := emacsClientPath(); ok {
			// Reuse a running emacs; -a falls back to the configured emacs when no
			// server is up, -n returns immediately (detached).
			return client, []string{"-n", "-a", editor, path}, true
		}
		return parts[0], append(parts[1:], path), true // plain emacs GUI window
	}
	if guiEditors[base] {
		return parts[0], append(parts[1:], path), true
	}
	// vim, nano, vi, emacs -nw, or anything unknown: hand it a terminal.
	return parts[0], append(parts[1:], path), false
}

// hasTerminalFlag reports whether an emacs/emacsclient invocation explicitly
// asked for a terminal frame (rather than a GUI window).
func hasTerminalFlag(args []string) bool {
	for _, a := range args {
		switch a {
		case "-nw", "--no-window-system", "-t", "-tty", "--tty":
			return true
		}
	}
	return false
}

// openURL opens u with the platform's default handler (macOS `open`, Windows
// url handler, else `xdg-open`). It returns once the child is spawned.
func openURL(u string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", u)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", u)
	default:
		cmd = exec.Command("xdg-open", u)
	}
	return cmd.Start()
}
