package main

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

var psCmd = &cobra.Command{
	Use:   "ps",
	Short: "Every container on the runtime, annotated with stack and kind",
	Args:  exactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		eng, err := buildEngine()
		if err != nil {
			return err
		}
		cs, err := eng.Ps(cmd.Context())
		if err != nil {
			return err
		}
		if jsonOut {
			return printEnvelope("ContainerList", cs)
		}
		w := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
		fmt.Fprintln(w, "NAME\tIMAGE\tSTATE\tSTACK\tKIND\tPORTS")
		for _, c := range cs {
			stack := c.Stack
			if stack == "" {
				stack = "-"
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", c.Name, c.Image, c.State, stack, c.Kind, c.Ports)
		}
		return w.Flush()
	},
}

func init() { rootCmd.AddCommand(psCmd) }
