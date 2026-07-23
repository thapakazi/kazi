package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/thapakazi/kazi/internal/engine"
	"github.com/thapakazi/kazi/internal/runtime"
	"github.com/thapakazi/kazi/internal/store"
	"github.com/thapakazi/kazi/internal/template"
)

const apiVersion = "kazi.dev/v1alpha1"

var ErrUsage = errors.New("usage error")

var jsonOut bool

func init() {
	rootCmd.PersistentFlags().BoolVar(&jsonOut, "json", false, "machine-readable output")
	rootCmd.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		return fmt.Errorf("%w: %v", ErrUsage, err)
	})
}

func exactArgs(n int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) != n {
			return fmt.Errorf("%w: expected %d argument(s), got %d", ErrUsage, n, len(args))
		}
		return nil
	}
}

func rangeArgs(min, max int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) < min || len(args) > max {
			return fmt.Errorf("%w: expected %d to %d argument(s), got %d", ErrUsage, min, max, len(args))
		}
		return nil
	}
}

func buildEngine() (*engine.Engine, error) {
	cfg, err := store.LoadConfig()
	if err != nil {
		return nil, err
	}
	rt, err := runtime.Detect(cfg.Spec.Runtime)
	if err != nil {
		return nil, err
	}
	return engine.New(rt, cfg, os.Stdout, os.Stderr), nil
}

type envelope struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Items      any    `json:"items"`
}

func printEnvelope(kind string, items any) error {
	return json.NewEncoder(os.Stdout).Encode(envelope{APIVersion: apiVersion, Kind: kind, Items: items})
}

type result struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Action     string `json:"action"`
	Stack      string `json:"stack"`
	OK         bool   `json:"ok"`
}

func printResult(action, stack string) error {
	return json.NewEncoder(os.Stdout).Encode(result{APIVersion: apiVersion, Kind: "Result", Action: action, Stack: stack, OK: true})
}

func exitCode(err error) int {
	var ee *exitError
	switch {
	case err == nil:
		return 0
	case errors.As(err, &ee):
		return ee.code
	case errors.Is(err, ErrUsage):
		return 2
	case errors.Is(err, engine.ErrStackNotFound),
		errors.Is(err, engine.ErrServiceNotFound),
		errors.Is(err, engine.ErrServiceNotRunning):
		return 3
	case errors.Is(err, runtime.ErrNoRuntime):
		return 4
	default:
		return 1
	}
}

func errCode(err error) string {
	switch {
	case errors.Is(err, ErrUsage):
		return "usage"
	case errors.Is(err, engine.ErrStackNotFound):
		return "stack_not_found"
	case errors.Is(err, engine.ErrServiceNotFound):
		return "service_not_found"
	case errors.Is(err, engine.ErrServiceNotRunning):
		return "service_not_running"
	case errors.Is(err, runtime.ErrNoRuntime):
		return "no_runtime"
	case errors.Is(err, template.ErrAborted):
		return "aborted"
	default:
		return "runtime_failure"
	}
}

type jsonError struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func Execute() int {
	err := rootCmd.Execute()
	if err == nil {
		return 0
	}
	// exec passthrough: the container command's stdout/stderr already went out
	// and its exit code is carried verbatim — no "kazi:" prefix, no JSON error.
	var ee *exitError
	if errors.As(err, &ee) {
		return ee.code
	}
	if jsonOut {
		var je jsonError
		je.Error.Code = errCode(err)
		je.Error.Message = err.Error()
		json.NewEncoder(os.Stderr).Encode(je) //nolint:errcheck
	} else {
		fmt.Fprintln(os.Stderr, "kazi:", err)
	}
	return exitCode(err)
}
