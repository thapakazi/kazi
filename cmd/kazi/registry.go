package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

var addCmd = &cobra.Command{
	Use:   "add <name> <path>",
	Short: "Register a stack from a compose file or directory",
	Args:  exactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		eng, err := buildEngine()
		if err != nil {
			return err
		}
		m, err := eng.Add(args[0], args[1])
		if err != nil {
			return err
		}
		if jsonOut {
			return printResult("add", args[0])
		}
		fmt.Printf("registered %s -> %s\n", m.Metadata.Name, m.Spec.Source.Compose)
		return nil
	},
}

var rmYes bool

var rmCmd = &cobra.Command{
	Use:   "rm <name>",
	Short: "Deregister a stack (never touches containers)",
	Args:  exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		eng, err := buildEngine()
		if err != nil {
			return err
		}
		name := args[0]
		if !rmYes {
			// Confirm only when the stack is currently running.
			if st, err := eng.Status(cmd.Context(), name); err == nil && st.Running > 0 {
				fmt.Fprintf(os.Stderr, "stack %q is running (%d/%d); deregister anyway? [y/N] ", name, st.Running, st.Total)
				var resp string
				fmt.Fscanln(os.Stdin, &resp)
				if !strings.HasPrefix(strings.ToLower(resp), "y") {
					fmt.Fprintln(os.Stderr, "aborted")
					return nil
				}
			}
		}
		if err := eng.Remove(name); err != nil {
			return err
		}
		if jsonOut {
			return printResult("rm", name)
		}
		fmt.Printf("removed %s (containers untouched)\n", name)
		return nil
	},
}

func init() {
	rmCmd.Flags().BoolVar(&rmYes, "yes", false, "skip confirmation")
	rootCmd.AddCommand(addCmd, rmCmd)
}
