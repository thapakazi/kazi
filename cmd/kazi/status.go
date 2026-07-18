package main

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/thapakazi/kazi/internal/engine"
)

var statusCmd = &cobra.Command{
	Use:   "status [stack]",
	Short: "Global dashboard, or per-service detail for one stack",
	Args:  rangeArgs(0, 1),
	RunE: func(cmd *cobra.Command, args []string) error {
		eng, err := buildEngine()
		if err != nil {
			return err
		}
		if len(args) == 1 {
			st, err := eng.Status(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if jsonOut {
				return printEnvelope("StackStatus", []engine.StackInfo{st})
			}
			fmt.Printf("%s (%s) — %s — %s\n", st.Name, st.Kind, statusCell(st), st.Dir)
			w := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
			fmt.Fprintln(w, "SERVICE\tSTATE\tHEALTH\tPORTS")
			for _, c := range st.Containers {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", c.Service, c.State, c.Health, c.Ports)
			}
			return w.Flush()
		}
		stacks, err := eng.List(cmd.Context())
		if err != nil {
			return err
		}
		if jsonOut {
			return printEnvelope("StackStatusList", stacks)
		}
		w := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
		for _, kind := range []engine.Kind{engine.KindRegistered, engine.KindDiscovered} {
			for _, s := range stacks {
				if s.Kind == kind {
					fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", s.Name, s.Kind, statusCell(s), s.Dir)
				}
			}
		}
		return w.Flush()
	},
}

func init() { rootCmd.AddCommand(statusCmd) }
