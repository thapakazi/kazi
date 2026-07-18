package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/thapakazi/kazi/internal/engine"
)

// gcReport is the JSON shape for `kazi gc --json`.
type gcReport struct {
	APIVersion string           `json:"apiVersion"`
	Kind       string           `json:"kind"`
	Items      []engine.GcItem  `json:"items"`
	Reclaimed  []engine.GcItem  `json:"reclaimed"`
}

var (
	gcDryRun bool
	gcYes    bool
)

var gcCmd = &cobra.Command{
	Use:   "gc",
	Short: "Reclaim ephemeral stacks, orphaned containers, and stale port allocations",
	Args:  exactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		eng, err := buildEngine()
		if err != nil {
			return err
		}

		items, err := eng.GcPlan(cmd.Context())
		if err != nil {
			return err
		}

		if len(items) == 0 {
			if jsonOut {
				return json.NewEncoder(os.Stdout).Encode(gcReport{
					APIVersion: apiVersion,
					Kind:       "GcReport",
					Items:      []engine.GcItem{},
					Reclaimed:  []engine.GcItem{},
				})
			}
			fmt.Println("nothing to reclaim")
			return nil
		}

		// Print the plan table.
		if !jsonOut {
			w := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
			fmt.Fprintln(w, "KIND\tNAME\tREASON")
			for _, it := range items {
				fmt.Fprintf(w, "%s\t%s\t%s\n", it.Kind, it.Name, it.Reason)
			}
			_ = w.Flush()
		}

		// --dry-run: stop after printing the table.
		if gcDryRun {
			if jsonOut {
				return json.NewEncoder(os.Stdout).Encode(gcReport{
					APIVersion: apiVersion,
					Kind:       "GcReport",
					Items:      items,
					Reclaimed:  []engine.GcItem{},
				})
			}
			return nil
		}

		// Confirm unless --yes.
		if !gcYes {
			fmt.Fprintf(os.Stderr, "reclaim %d item(s)? [y/N] ", len(items))
			var resp string
			fmt.Fscanln(os.Stdin, &resp)
			if !strings.HasPrefix(strings.ToLower(resp), "y") {
				fmt.Fprintln(os.Stderr, "aborted")
				return nil
			}
		}

		reclaimed, runErr := eng.GcRun(cmd.Context(), items)

		if jsonOut {
			report := gcReport{
				APIVersion: apiVersion,
				Kind:       "GcReport",
				Items:      items,
				Reclaimed:  reclaimed,
			}
			if report.Reclaimed == nil {
				report.Reclaimed = []engine.GcItem{}
			}
			if encErr := json.NewEncoder(os.Stdout).Encode(report); encErr != nil {
				return encErr
			}
			return runErr
		}

		if runErr != nil {
			// Print reclaimed so far before returning the error.
			for _, r := range reclaimed {
				fmt.Printf("reclaimed %s %s\n", r.Kind, r.Name)
			}
			return runErr
		}

		for _, r := range reclaimed {
			fmt.Printf("reclaimed %s %s\n", r.Kind, r.Name)
		}
		return nil
	},
}

func init() {
	gcCmd.Flags().BoolVar(&gcDryRun, "dry-run", false, "print what would be reclaimed without acting")
	gcCmd.Flags().BoolVar(&gcYes, "yes", false, "skip confirmation prompt")

	rootCmd.AddCommand(gcCmd)
}
