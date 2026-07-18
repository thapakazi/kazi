package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/thapakazi/kazi/internal/engine"
)

// tryResult is the JSON shape for a successful `kazi try -d --json` call.
type tryResult struct {
	APIVersion string            `json:"apiVersion"`
	Kind       string            `json:"kind"`
	Action     string            `json:"action"`
	Stack      string            `json:"stack"`
	OK         bool              `json:"ok"`
	Endpoints  []engine.Endpoint `json:"endpoints"`
}

var (
	tryKeep   bool
	tryDetach bool
	trySets   []string
)

var tryCmd = &cobra.Command{
	Use:   "try <template>",
	Short: "Spin up an ephemeral template stack (zero residue on Ctrl-C)",
	Args:  exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// --json without -d is a usage error (interactive session + machine output
		// are contradictory).
		if jsonOut && !tryDetach {
			return fmt.Errorf("%w: --json requires -d/--detach for try", ErrUsage)
		}

		eng, err := buildEngine()
		if err != nil {
			return err
		}

		tmpl := args[0]
		opts := engine.TryOpts{
			Detach: tryDetach,
			Keep:   tryKeep,
			Sets:   trySets,
		}

		// Use a signal context so we hear Ctrl-C / SIGTERM.
		sigCtx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
		defer stop()

		name, eps, err := eng.Try(sigCtx, tmpl, opts)
		if err != nil {
			return err
		}

		// Print the URL table.
		if len(eps) > 0 {
			w := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
			fmt.Fprintln(w, "STACK\tSERVICE\tURL\tTARGET\tNOTE")
			for _, ep := range eps {
				fmt.Fprintln(w, endpointRow(ep))
			}
			_ = w.Flush()
		}

		if tryDetach {
			// Detached mode: print the reclaim hint and return immediately.
			if jsonOut {
				return json.NewEncoder(os.Stdout).Encode(tryResult{
					APIVersion: apiVersion,
					Kind:       "Result",
					Action:     "try",
					Stack:      name,
					OK:         true,
					Endpoints:  eps,
				})
			}
			fmt.Printf("detached — reclaim with: kazi gc (or keep with: kazi keep %s)\n", name)
			return nil
		}

		// Foreground mode: stream logs until signal, then tear down.
		fmt.Fprintf(os.Stderr,
			"Ctrl-C to tear down (zero residue)… (second Ctrl-C = abandon; kazi gc reclaims)\n")

		// Stream logs; ignore context-canceled error (that's our signal).
		logErr := eng.Logs(sigCtx, name, "", true, "")
		if logErr != nil && sigCtx.Err() == nil {
			// Real log error, not a cancellation — print it as a warning.
			fmt.Fprintf(os.Stderr, "kazi: warning: logs: %v\n", logErr)
		}

		fmt.Fprintln(os.Stderr, "tearing down…")
		// Use a fresh context — the signal one is already canceled.
		if tdErr := eng.Teardown(context.Background(), name); tdErr != nil {
			return tdErr
		}
		return nil
	},
}

// keepCmd marks a named stack as non-ephemeral (manifest-only, no runtime calls).
var keepCmd = &cobra.Command{
	Use:   "keep <stack>",
	Short: "Mark an ephemeral stack as permanent (manifest-only, no restart)",
	Args:  exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		eng, err := buildEngine()
		if err != nil {
			return err
		}
		name := args[0]
		if err := eng.Keep(name); err != nil {
			return err
		}
		if jsonOut {
			return printResult("keep", name)
		}
		fmt.Printf("kept %s — no longer ephemeral\n", name)
		return nil
	},
}

func init() {
	tryCmd.Flags().BoolVar(&tryKeep, "keep", false, "register as a persistent (non-ephemeral) stack")
	tryCmd.Flags().BoolVarP(&tryDetach, "detach", "d", false, "start detached (don't stream logs)")
	tryCmd.Flags().StringArrayVar(&trySets, "set", nil, "set a template value (k=v), repeatable")

	rootCmd.AddCommand(tryCmd, keepCmd)
}
