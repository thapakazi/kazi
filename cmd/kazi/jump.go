package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

var jumpPrint bool

var jumpCmd = &cobra.Command{
	Use:   "jump <stack> --print",
	Short: "Print a stack's project directory (use kj to cd; see shell-init)",
	Args:  exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		eng, err := buildEngine()
		if err != nil {
			return err
		}
		dir, err := eng.Jump(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		fmt.Println(dir)
		return nil
	},
}

// kjFunction is the zoxide-pattern shell hook: eval "$(kazi shell-init)"
// in .zshrc defines kj, which cd's to a stack's project directory.
const kjFunction = `kj() { local d; d="$(kazi jump "$1" --print)" && cd "$d"; }`

var shellInitCmd = &cobra.Command{
	Use:   "shell-init",
	Short: `Emit the kj shell function (add eval "$(kazi shell-init)" to .zshrc)`,
	Args:  exactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println(kjFunction)
		return nil
	},
}

func init() {
	jumpCmd.Flags().BoolVar(&jumpPrint, "print", false, "print the directory (default behavior)")
	rootCmd.AddCommand(jumpCmd, shellInitCmd)
}
