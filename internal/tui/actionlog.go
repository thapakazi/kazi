package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/thapakazi/kazi/internal/store"
)

// actionHistoryLines is how many trailing log lines the panel loads as history.
const actionHistoryLines = 200

// actionLogPath is the append-only action log under the kazi config root
// (honouring KAZI_CONFIG_DIR).
func actionLogPath() string {
	return filepath.Join(store.Root(), "actions.log")
}

// appendActionLogCmd persists a finished action's captured output (with a dated
// header) so it can be reviewed later — including across sessions — via the
// Action panel. Failures are silent: a missing log must never disrupt the UI.
func appendActionLogCmd(title string, lines []string) tea.Cmd {
	if len(lines) == 0 {
		return nil
	}
	captured := append([]string(nil), lines...) // snapshot; the model keeps mutating
	return func() tea.Msg {
		p := actionLogPath()
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return nil
		}
		f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return nil
		}
		defer f.Close()
		fmt.Fprintf(f, "=== %s — %s ===\n", time.Now().Format("2006-01-02 15:04:05"), title)
		for _, ln := range captured {
			fmt.Fprintln(f, ln)
		}
		return nil
	}
}

// loadActionHistoryCmd reads the tail of the action log for the panel's initial
// history view.
func loadActionHistoryCmd() tea.Cmd {
	return func() tea.Msg {
		b, err := os.ReadFile(actionLogPath())
		if err != nil {
			return actionHistoryMsg{}
		}
		lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
		if len(lines) > actionHistoryLines {
			lines = lines[len(lines)-actionHistoryLines:]
		}
		return actionHistoryMsg{lines: lines}
	}
}
