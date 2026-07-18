package main

import (
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:           "kazi",
	Short:         "Compose-preferred, runtime-agnostic local stack manager",
	SilenceUsage:  true,
	SilenceErrors: true,
}

func main() {
	os.Exit(Execute())
}
