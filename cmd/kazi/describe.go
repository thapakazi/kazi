package main

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/thapakazi/kazi/internal/engine"
)

var describeStack string

var describeCmd = &cobra.Command{
	Use:   "describe [stack]",
	Short: "Show everything about one stack: status, manifest, endpoints",
	Args:  rangeArgs(0, 1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := describeStack
		if len(args) == 1 {
			if name != "" && name != args[0] {
				return fmt.Errorf("%w: both positional %q and -s %q given", ErrUsage, args[0], name)
			}
			name = args[0]
		}
		if name == "" {
			return fmt.Errorf("%w: a stack name is required (positional or -s)", ErrUsage)
		}
		eng, err := buildEngine()
		if err != nil {
			return err
		}
		d, err := eng.Describe(cmd.Context(), name)
		if err != nil {
			return err
		}
		if jsonOut {
			return printEnvelope("StackDetail", []engine.StackDetail{d})
		}
		fmt.Printf("Name:     %s\n", d.Name)
		fmt.Printf("Kind:     %s%s\n", d.Kind, map[bool]string{true: " (system)", false: ""}[d.System])
		fmt.Printf("Status:   %s\n", statusCell(d.StackInfo))
		fmt.Printf("Path:     %s\n", d.Dir)
		fmt.Printf("Project:  %s\n", d.Project)
		if d.Source != "" {
			fmt.Printf("Compose:  %s\n", d.Source)
		}
		if d.Proxy != nil {
			fmt.Printf("Proxy:    service=%s", d.Proxy.Service)
			if d.Proxy.HTTPPort != 0 {
				fmt.Printf(" http_port=%d", d.Proxy.HTTPPort)
			}
			if d.Proxy.Enabled != nil && !*d.Proxy.Enabled {
				fmt.Printf(" enabled=false")
			}
			fmt.Println()
		}
		for _, ex := range d.Expose {
			fmt.Printf("Expose:   %s (port: %s)\n", ex.Service, ex.Port)
		}
		if len(d.Containers) > 0 {
			fmt.Println("\nServices:")
			w := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
			fmt.Fprintln(w, "  SERVICE\tSTATE\tHEALTH\tPORTS")
			for _, c := range d.Containers {
				fmt.Fprintf(w, "  %s\t%s\t%s\t%s\n", c.Service, c.State, c.Health, c.Ports)
			}
			w.Flush()
		}
		if len(d.Endpoints) > 0 {
			fmt.Println("\nEndpoints:")
			w := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
			fmt.Fprintln(w, "  KIND\tURL\tTARGET\tNOTE")
			for _, ep := range d.Endpoints {
				fmt.Fprintf(w, "  %s\t%s\t%s\t%s\n", ep.Kind, dash(ep.URL), dash(ep.Target), dash(ep.Note))
			}
			w.Flush()
		}
		return nil
	},
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func init() {
	describeCmd.Flags().StringVarP(&describeStack, "stack", "s", "", "stack name (alternative to the positional argument)")
	rootCmd.AddCommand(describeCmd)
}
