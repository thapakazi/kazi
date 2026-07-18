package main

import (
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:           "kazi",
	Short:         "The control plane for your local containers",
	SilenceUsage:  true,
	SilenceErrors: true,
}

func main() {
	os.Exit(Execute())
}
