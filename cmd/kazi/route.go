package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

// routeCmd groups static-route management: expose any host-published port at
// <host>.localhost via the kazi proxy (works for services kazi doesn't manage).
var routeCmd = &cobra.Command{
	Use:   "route",
	Short: "Route <host>.localhost to a published host port (external services)",
}

var routeAddNote string

var routeAddCmd = &cobra.Command{
	Use:   "add <host> <port>",
	Short: "Add or update a static route (<host>.localhost → host.docker.internal:<port>)",
	Args:  exactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		port, err := strconv.Atoi(args[1])
		if err != nil {
			return fmt.Errorf("%w: port must be a number, got %q", ErrUsage, args[1])
		}
		eng, err := buildEngine()
		if err != nil {
			return err
		}
		host := args[0]
		if err := eng.RouteAdd(cmd.Context(), host, port, routeAddNote, ""); err != nil {
			return err
		}
		if jsonOut {
			return printResult("route-add", host)
		}
		fmt.Printf("https://%s.localhost → host.docker.internal:%d\n", host, port)
		return nil
	},
}

var routeLsCmd = &cobra.Command{
	Use:   "ls",
	Short: "List static routes",
	Args:  exactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		eng, err := buildEngine()
		if err != nil {
			return err
		}
		routes, err := eng.RouteList()
		if err != nil {
			return err
		}
		if jsonOut {
			return printEnvelope("RouteList", routes)
		}
		if len(routes) == 0 {
			fmt.Println("no routes — add one with: kazi route add <host> <port>")
			return nil
		}
		w := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
		fmt.Fprintln(w, "URL\tTARGET\tNOTE")
		for _, r := range routes {
			fmt.Fprintf(w, "https://%s.localhost\thost.docker.internal:%d\t%s\n", r.Host, r.Port, r.Note)
		}
		return w.Flush()
	},
}

var routeRmCmd = &cobra.Command{
	Use:   "rm <host>",
	Short: "Remove a static route",
	Args:  exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		eng, err := buildEngine()
		if err != nil {
			return err
		}
		if err := eng.RouteRemove(cmd.Context(), args[0]); err != nil {
			return err
		}
		if jsonOut {
			return printResult("route-rm", args[0])
		}
		fmt.Printf("removed route %s\n", args[0])
		return nil
	},
}

var routeFromYes bool

var routeFromCmd = &cobra.Command{
	Use:   "from <stack>",
	Short: "Suggest routes from a stack's published ports (e.g. a discovered stack)",
	Args:  exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		eng, err := buildEngine()
		if err != nil {
			return err
		}
		stack := args[0]
		candidates, err := eng.RoutesFromStack(cmd.Context(), stack)
		if err != nil {
			return err
		}

		if jsonOut {
			return printEnvelope("RouteCandidates", candidates)
		}

		w := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
		fmt.Fprintln(w, "HOST\tURL\tPUBLISHED\tSERVICE")
		for _, c := range candidates {
			fmt.Fprintf(w, "%s\thttps://%s.localhost\t%d→%d\t%s\n", c.Host, c.Host, c.Port, c.Target, c.Service)
		}
		w.Flush()

		if !routeFromYes {
			fmt.Fprintf(os.Stderr, "\nadd these %d route(s)? [Y/n] ", len(candidates))
			line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
			if strings.HasPrefix(strings.ToLower(strings.TrimSpace(line)), "n") {
				fmt.Fprintln(os.Stderr, "aborted — no routes added")
				return nil
			}
		}
		for _, c := range candidates {
			note := fmt.Sprintf("%s from %s", c.Service, stack)
			if err := eng.RouteAdd(cmd.Context(), c.Host, c.Port, note, stack); err != nil {
				fmt.Fprintf(os.Stderr, "kazi: warning: route %s: %v\n", c.Host, err)
				continue
			}
			fmt.Printf("https://%s.localhost → host.docker.internal:%d\n", c.Host, c.Port)
		}
		return nil
	},
}

func init() {
	routeAddCmd.Flags().StringVar(&routeAddNote, "note", "", "optional note for the route")
	routeFromCmd.Flags().BoolVarP(&routeFromYes, "yes", "y", false, "add all suggested routes without prompting")
	routeCmd.AddCommand(routeAddCmd, routeLsCmd, routeRmCmd, routeFromCmd)
	rootCmd.AddCommand(routeCmd)
}
