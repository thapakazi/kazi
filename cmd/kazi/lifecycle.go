package main

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/thapakazi/kazi/internal/engine"
)

// lifecycle builds up/down/restart: same shape, different engine verb.
func lifecycle(verb, short string, fn func(*engine.Engine, context.Context, string) error) *cobra.Command {
	return &cobra.Command{
		Use:   verb + " <stack>",
		Short: short,
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := buildEngine()
			if err != nil {
				return err
			}
			if err := fn(eng, cmd.Context(), args[0]); err != nil {
				return err
			}
			if jsonOut {
				return printResult(verb, args[0])
			}
			return nil
		},
	}
}

var (
	logsFollow bool
	logsTail   string
)

var logsCmd = &cobra.Command{
	Use:   "logs <stack> [service]",
	Short: "Stream compose logs for a stack",
	Args:  rangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		eng, err := buildEngine()
		if err != nil {
			return err
		}
		service := ""
		if len(args) == 2 {
			service = args[1]
		}
		return eng.Logs(cmd.Context(), args[0], service, logsFollow, logsTail)
	},
}

func init() {
	logsCmd.Flags().BoolVarP(&logsFollow, "follow", "f", false, "follow log output")
	logsCmd.Flags().StringVar(&logsTail, "tail", "", "number of lines to show from the end")
	rootCmd.AddCommand(
		lifecycle("up", "Bring a stack up (detached, idempotent)", (*engine.Engine).Up),
		lifecycle("down", "Stop and remove a stack's containers", (*engine.Engine).Down),
		lifecycle("restart", "Restart a stack", (*engine.Engine).Restart),
		logsCmd,
	)
}
