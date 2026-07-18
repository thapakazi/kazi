package main

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/thapakazi/kazi/internal/engine"
)

var lsCmd = &cobra.Command{
	Use:   "ls",
	Short: "List registered and discovered stacks",
	Args:  exactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		eng, err := buildEngine()
		if err != nil {
			return err
		}
		stacks, err := eng.List(cmd.Context())
		if err != nil {
			return err
		}
		if jsonOut {
			return printEnvelope("StackList", stacks)
		}
		w := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
		fmt.Fprintln(w, "NAME\tKIND\tSTATUS\tPATH")
		for _, s := range stacks {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", s.Name, s.Kind, statusCell(s), s.Dir)
		}
		return w.Flush()
	},
}

func statusCell(s engine.StackInfo) string {
	if s.Running > 0 {
		return fmt.Sprintf("running %d/%d", s.Running, s.Total)
	}
	return "stopped"
}

func init() { rootCmd.AddCommand(lsCmd) }
