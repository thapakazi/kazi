package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/thapakazi/kazi/internal/engine"
)

var (
	editCompose bool
	editEditor  string
	editRestart bool
)

// errEditAborted is returned when the user declines the re-edit prompt after an
// invalid save; the original file is restored byte-for-byte first.
var errEditAborted = errors.New("edit aborted")

// editCmd opens a stack's manifest (or, with --compose, its compose file) in
// $EDITOR, validates on save, and offers re-edit/abort on invalid input. It is
// interactive: no --json, and a non-TTY invocation is a usage error (exit 2).
var editCmd = &cobra.Command{
	Use:   "edit <stack>",
	Short: "Edit a stack's manifest (or its compose file with --compose) in $EDITOR",
	Args:  exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if jsonOut {
			return fmt.Errorf("%w: edit is an interactive editor launch; --json is not supported", ErrUsage)
		}
		if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
			return fmt.Errorf("%w: edit launches $EDITOR and needs a terminal (stdin/stdout must be a TTY)", ErrUsage)
		}

		eng, err := buildEngine()
		if err != nil {
			return err
		}
		name := args[0]

		var target engine.EditTarget
		if editCompose {
			target, err = eng.ComposeTarget(name)
		} else {
			target, err = eng.ManifestTarget(name)
		}
		if err != nil {
			return err
		}

		editor := engine.ResolveEditor(editEditor)
		if _, lookErr := exec.LookPath(strings.Fields(editor)[0]); lookErr != nil {
			return fmt.Errorf("%w: editor %q not found — set $EDITOR/$VISUAL or pass --editor", ErrUsage, editor)
		}

		if err := editWithRecovery(cmd.Context(), eng, editor, target); err != nil {
			return err
		}
		fmt.Printf("saved %s (%s)\n", target.Path, target.Kind)

		return maybeRestart(cmd.Context(), eng, name)
	},
}

// editWithRecovery snapshots the file, opens it in $EDITOR, and validates on
// return. On invalid input it offers re-edit (reopen the same buffer) or abort
// (restore the original bytes, write nothing). An editor crash is treated as an
// abort.
func editWithRecovery(ctx context.Context, eng *engine.Engine, editor string, target engine.EditTarget) error {
	orig, err := os.ReadFile(target.Path)
	if err != nil {
		return fmt.Errorf("cannot read %s: %w", target.Path, err)
	}
	for {
		if launchErr := launchEditor(editor, target.Path); launchErr != nil {
			restore(target.Path, orig)
			return fmt.Errorf("editor exited with error (changes discarded): %w", launchErr)
		}
		if target.Validate == nil {
			return nil
		}
		vErr := target.Validate(ctx)
		if vErr == nil {
			return nil
		}
		fmt.Fprintf(os.Stderr, "%s validation failed: %v\nre-edit? [Y/n] ", target.Kind, vErr)
		line, scanErr := bufio.NewReader(os.Stdin).ReadString('\n')
		declined := scanErr != nil || strings.HasPrefix(strings.ToLower(strings.TrimSpace(line)), "n")
		if declined {
			if wErr := restore(target.Path, orig); wErr != nil {
				return fmt.Errorf("restoring original after abort: %w", wErr)
			}
			return fmt.Errorf("%w: %s left unchanged", errEditAborted, target.Path)
		}
	}
}

// restore writes orig back to path (best-effort byte-for-byte revert).
func restore(path string, orig []byte) error {
	return os.WriteFile(path, orig, 0o644)
}

// launchEditor execs $EDITOR on path with the terminal attached. The editor
// string may carry arguments (e.g. "code --wait"), split on whitespace. It is a
// var so tests can stub the interactive editor launch.
var launchEditor = func(editor, path string) error {
	parts := strings.Fields(editor)
	c := exec.Command(parts[0], append(parts[1:], path)...) //nolint:gosec // user-provided editor is intentional
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	return c.Run()
}

// maybeRestart prints the M3-style "restart to take effect" notice when the
// edited stack is running and, with --restart, restarts it; otherwise it offers
// to. kazi never silently recreates on edit.
func maybeRestart(ctx context.Context, eng *engine.Engine, name string) error {
	st, err := eng.Status(ctx, name)
	if err != nil || st.Running == 0 {
		return nil // stopped, or status unavailable: nothing to restart
	}
	if editRestart {
		fmt.Fprintf(os.Stderr, "%s is running — restarting to apply changes…\n", name)
		return eng.Restart(ctx, name)
	}
	fmt.Fprintf(os.Stderr, "%s is running — restart to take effect: kazi restart %s\nrestart now? [y/N] ", name, name)
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(line)), "y") {
		return eng.Restart(ctx, name)
	}
	return nil
}

func init() {
	editCmd.Flags().BoolVar(&editCompose, "compose", false, "edit the stack's compose file instead of its kazi manifest")
	editCmd.Flags().StringVar(&editEditor, "editor", "", "editor binary to use (overrides $EDITOR/$VISUAL)")
	editCmd.Flags().BoolVar(&editRestart, "restart", false, "restart the stack after a successful edit if it is running")
	rootCmd.AddCommand(editCmd)
}
