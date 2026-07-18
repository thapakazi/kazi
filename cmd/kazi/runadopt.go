package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	runName  string
	runPorts []string
	runEnvs  []string
	runVols  []string
)

var runCmd = &cobra.Command{
	Use:   "run <image>",
	Short: "Run an ad-hoc image-backed stack",
	Args:  exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		eng, err := buildEngine()
		if err != nil {
			return err
		}
		image := args[0]
		name, err := eng.RunImage(cmd.Context(), runName, image, runPorts, runEnvs, runVols)
		if err != nil {
			return err
		}
		if jsonOut {
			return printResult("run", name)
		}
		fmt.Printf("running %s (%s) — kazi urls %s for endpoints\n", name, image, name)
		return nil
	},
}

var adoptCmd = &cobra.Command{
	Use:   "adopt <name> <container>...",
	Short: "Adopt existing containers into a named stack",
	Args:  rangeArgs(2, 99),
	RunE: func(cmd *cobra.Command, args []string) error {
		eng, err := buildEngine()
		if err != nil {
			return err
		}
		name := args[0]
		containers := args[1:]
		if err := eng.Adopt(cmd.Context(), name, containers); err != nil {
			return err
		}
		if jsonOut {
			return printResult("adopt", name)
		}
		fmt.Printf("adopted %d container(s) into %s\n", len(containers), name)
		return nil
	},
}

var ejectAdd bool

var ejectCmd = &cobra.Command{
	Use:   "eject <template> [dir]",
	Short: "Eject a template into an editable compose directory",
	Args:  rangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		eng, err := buildEngine()
		if err != nil {
			return err
		}
		tmpl := args[0]
		dir := ""
		if len(args) == 2 {
			dir = args[1]
		}
		dest, addCmd, err := eng.Eject(tmpl, dir, ejectAdd)
		if err != nil {
			return err
		}
		if jsonOut {
			type ejectResult struct {
				APIVersion string `json:"apiVersion"`
				Kind       string `json:"kind"`
				Action     string `json:"action"`
				Template   string `json:"template"`
				Dest       string `json:"dest"`
				AddCommand string `json:"addCommand"`
				Registered bool   `json:"registered,omitempty"`
				OK         bool   `json:"ok"`
			}
			r := ejectResult{
				APIVersion: apiVersion,
				Kind:       "Result",
				Action:     "eject",
				Template:   tmpl,
				Dest:       dest,
				AddCommand: addCmd,
				OK:         true,
			}
			if ejectAdd {
				r.Registered = true
			}
			return json.NewEncoder(os.Stdout).Encode(r)
		}
		if ejectAdd {
			fmt.Printf("ejected %s → %s\nregistered: %s\n", tmpl, dest, addCmd)
		} else {
			fmt.Printf("ejected %s → %s\n%s\n", tmpl, dest, addCmd)
		}
		return nil
	},
}

func init() {
	runCmd.Flags().StringVar(&runName, "name", "", "stack name (default: derived from image)")
	runCmd.Flags().StringArrayVarP(&runPorts, "publish", "p", nil, "publish port mapping (hostPort:containerPort)")
	runCmd.Flags().StringArrayVarP(&runEnvs, "env", "e", nil, "environment variable (K=V)")
	runCmd.Flags().StringArrayVarP(&runVols, "volume", "v", nil, "volume mount (vol:/path)")

	ejectCmd.Flags().BoolVar(&ejectAdd, "add", false, "register the ejected stack with kazi add")

	rootCmd.AddCommand(runCmd, adoptCmd, ejectCmd)
}
