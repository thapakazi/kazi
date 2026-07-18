package main

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/thapakazi/kazi/internal/engine"
)

// endpointRow formats a single Endpoint as a tab-separated row for tabwriter.
// Empty fields are rendered as "-".
func endpointRow(ep engine.Endpoint) string {
	dash := func(s string) string {
		if s == "" {
			return "-"
		}
		return s
	}
	return fmt.Sprintf("%s\t%s\t%s\t%s\t%s",
		dash(ep.Stack),
		dash(ep.Service),
		dash(ep.URL),
		dash(ep.Target),
		dash(ep.Note),
	)
}

var urlsCmd = &cobra.Command{
	Use:   "urls [stack]",
	Short: "List reachable endpoints for stacks",
	Args:  rangeArgs(0, 1),
	RunE: func(cmd *cobra.Command, args []string) error {
		eng, err := buildEngine()
		if err != nil {
			return err
		}
		stackArg := ""
		if len(args) == 1 {
			stackArg = args[0]
		}
		eps, err := eng.Urls(cmd.Context(), stackArg)
		if err != nil {
			return err
		}
		if jsonOut {
			return printEnvelope("EndpointList", eps)
		}
		w := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
		fmt.Fprintln(w, "STACK\tSERVICE\tURL\tTARGET\tNOTE")
		for _, ep := range eps {
			fmt.Fprintln(w, endpointRow(ep))
		}
		return w.Flush()
	},
}

var (
	exposePort   int
	exposeRemove bool
)

var exposeCmd = &cobra.Command{
	Use:   "expose <stack> <service>",
	Short: "Expose a TCP service on a stable host port",
	Args:  exactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		eng, err := buildEngine()
		if err != nil {
			return err
		}
		stack, service := args[0], args[1]
		hostPort, err := eng.Expose(cmd.Context(), stack, service, exposePort, exposeRemove)
		if err != nil {
			return err
		}
		if jsonOut {
			action := "expose"
			if exposeRemove {
				action = "unexpose"
			}
			return printResult(action, stack)
		}
		if exposeRemove {
			fmt.Printf("removed exposure for %s/%s\n", stack, service)
		} else {
			fmt.Printf("exposed %s/%s on localhost:%d (stable across down/up)\n", stack, service, hostPort)
		}
		return nil
	},
}

var trustUninstall bool

var trustCmd = &cobra.Command{
	Use:   "trust",
	Short: "Install kazi's local CA into the system trust store",
	Args:  exactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		eng, err := buildEngine()
		if err != nil {
			return err
		}
		if trustUninstall {
			fmt.Fprintln(os.Stderr, "uninstalling kazi's local CA from the system trust store (sudo may prompt)...")
		} else {
			fmt.Fprintln(os.Stderr, "installing kazi's local CA into the system trust store (sudo may prompt)...")
		}
		if err := eng.Trust(cmd.Context(), trustUninstall); err != nil {
			return err
		}
		if !trustUninstall {
			fmt.Fprintln(os.Stderr, "note: Firefox uses its own trust store — set security.enterprise_roots.enabled to true")
		}
		return nil
	},
}

func init() {
	exposeCmd.Flags().IntVar(&exposePort, "port", 0, "host port to pin (0 = auto-allocate)")
	exposeCmd.Flags().BoolVar(&exposeRemove, "remove", false, "remove the exposure")
	trustCmd.Flags().BoolVar(&trustUninstall, "uninstall", false, "remove kazi's CA from the system trust store")
	rootCmd.AddCommand(urlsCmd, exposeCmd, trustCmd)
}
