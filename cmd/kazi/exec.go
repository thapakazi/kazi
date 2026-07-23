package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/thapakazi/kazi/internal/engine"
)

var (
	execShell   string
	execUser    string
	execWorkdir string
	execEnv     []string
	execIndex   int
	execTTY     bool
)

// exitError carries a container command's own exit status out through cobra
// without the "kazi:" prefix — `kazi exec` passes the command's code through
// (docker/ssh convention). Execute() special-cases it: no stderr line, just the code.
type exitError struct{ code int }

func (e *exitError) Error() string { return fmt.Sprintf("command exited with status %d", e.code) }

var execCmd = &cobra.Command{
	Use:   "exec <stack> <service> [-- <cmd>...]",
	Short: "Run a command or interactive shell inside a service container",
	Long: "With no command, opens an interactive login shell (needs a terminal).\n" +
		"With -- <cmd>, runs the command non-interactively and exits with its code.",
	Args: cobra.ArbitraryArgs, // validated in parseExecArgs (accounts for --)
	RunE: func(cmd *cobra.Command, args []string) error {
		stack, service, command, err := parseExecArgs(args, cmd.ArgsLenAtDash())
		if err != nil {
			return err
		}
		opts := engine.ExecOpts{
			Shell: execShell, User: execUser, Workdir: execWorkdir,
			Env: execEnv, Index: execIndex, TTY: execTTY,
		}
		eng, err := buildEngine()
		if err != nil {
			return err
		}

		// Interactive shell: no command given.
		if len(command) == 0 {
			if jsonOut {
				return fmt.Errorf("%w: --json needs a command (-- <cmd>); interactive shells can't be captured", ErrUsage)
			}
			if !isTTY(os.Stdin) {
				return fmt.Errorf("%w: an interactive shell needs a terminal; pass a command (e.g. -- sh -c '...')", ErrUsage)
			}
			c, err := eng.ExecCommand(cmd.Context(), stack, service, nil, opts)
			if err != nil {
				return err
			}
			c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
			return runPassthrough(c)
		}

		// Non-interactive capture.
		res, err := eng.Exec(cmd.Context(), stack, service, command, opts)
		if err != nil {
			return err
		}
		if jsonOut {
			// The envelope carries the command's exitCode; the process exits 0
			// (kazi succeeded) so agents disambiguate kazi failure from a non-zero command.
			return printExecResult(res)
		}
		os.Stdout.WriteString(res.Stdout)
		os.Stderr.WriteString(res.Stderr)
		if res.ExitCode != 0 {
			return &exitError{code: res.ExitCode}
		}
		return nil
	},
}

// parseExecArgs splits cobra args at the `--` marker (dash = ArgsLenAtDash, -1
// when absent): everything before is positional (<stack> <service>), everything
// after is the command argv.
func parseExecArgs(args []string, dash int) (stack, service string, command []string, err error) {
	positional := args
	if dash >= 0 {
		positional = args[:dash]
		// A bare `--` with nothing after ⇒ no command (interactive), not [].
		if dash < len(args) {
			command = args[dash:]
		}
	}
	if len(positional) != 2 {
		return "", "", nil, fmt.Errorf("%w: expected <stack> <service> [-- <cmd>...], got %d positional arg(s)", ErrUsage, len(positional))
	}
	return positional[0], positional[1], command, nil
}

// runPassthrough runs an interactive command with inherited stdio, surfacing the
// command's own exit code via exitError (not a kazi failure).
func runPassthrough(c *exec.Cmd) error {
	if err := c.Run(); err != nil {
		var xe *exec.ExitError
		if errors.As(err, &xe) {
			return &exitError{code: xe.ExitCode()}
		}
		return err
	}
	return nil
}

func printExecResult(res engine.ExecResult) error {
	return json.NewEncoder(os.Stdout).Encode(struct {
		APIVersion string `json:"apiVersion"`
		Kind       string `json:"kind"`
		engine.ExecResult
	}{apiVersion, "ExecResult", res})
}

func isTTY(f *os.File) bool { return term.IsTerminal(int(f.Fd())) }

func init() {
	execCmd.Flags().StringVar(&execShell, "shell", "", "shell to launch (overrides the login-shell probe)")
	execCmd.Flags().StringVarP(&execUser, "user", "u", "", "username or UID to run as")
	execCmd.Flags().StringVarP(&execWorkdir, "workdir", "w", "", "working directory inside the container")
	execCmd.Flags().StringArrayVarP(&execEnv, "env", "e", nil, "set environment variables (K=V, repeatable)")
	execCmd.Flags().IntVar(&execIndex, "index", 0, "replica index for a scaled service (default 1)")
	execCmd.Flags().BoolVarP(&execTTY, "tty", "t", false, "allocate a TTY for a captured command")
	rootCmd.AddCommand(execCmd)
}
