package main

import (
	"fmt"
	"io"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/thapakazi/kazi/internal/tui"
)

var tuiRefresh string

var tuiCmd = &cobra.Command{
	Use:   "tui",
	Short: "Launch the interactive dashboard",
	Args:  exactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		// The TUI is the human skin: it needs a real terminal.
		if !term.IsTerminal(int(os.Stdout.Fd())) {
			return fmt.Errorf("%w: tui requires a terminal (stdout is not a TTY)", ErrUsage)
		}
		eng, err := buildEngine()
		if err != nil {
			// ErrNoRuntime maps to exit 4 via root's exitCode.
			return err
		}
		// The TUI owns an alt-screen; any stray compose output on stdout/stderr
		// would corrupt it. Lifecycle/log output is captured via pipes
		// (ActionStream/LogStream); everything else is discarded, not printed.
		eng.Out = io.Discard
		eng.Err = io.Discard
		refresh := resolveRefresh(eng.Cfg.Spec.TUI.RefreshInterval, tuiRefresh)
		model := tui.New(eng, refresh, tui.WithStatsHistory(eng.Cfg.Spec.TUI.StatsHistory))
		p := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())
		_, err = p.Run()
		return err
	},
}

// resolveRefresh picks the refresh interval: --refresh flag wins, then config,
// then the 2s default. Invalid durations fall back to the default.
func resolveRefresh(configured, flag string) time.Duration {
	const def = 2 * time.Second
	for _, s := range []string{flag, configured} {
		if s == "" {
			continue
		}
		if d, err := time.ParseDuration(s); err == nil && d > 0 {
			return d
		}
	}
	return def
}

func init() {
	tuiCmd.Flags().StringVar(&tuiRefresh, "refresh", "", "refresh interval override (e.g. 1s, 500ms)")
	rootCmd.AddCommand(tuiCmd)
}
