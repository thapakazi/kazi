package main

import (
	"os"

	"github.com/spf13/cobra"
)

// Build metadata, injected at release time via -ldflags -X (see .goreleaser.yaml).
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

var rootCmd = &cobra.Command{
	Use:           "kazi",
	Short:         "The control plane for your local containers",
	Version:       version,
	SilenceUsage:  true,
	SilenceErrors: true,
}

func init() {
	rootCmd.SetVersionTemplate("kazi {{.Version}} (commit " + commit + ", built " + date + ")\n")
}

func main() {
	os.Exit(Execute())
}
