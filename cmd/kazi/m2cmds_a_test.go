package main

import (
	"errors"
	"testing"
)

// TestTryCmdRegistration verifies that try, keep, and gc are registered on rootCmd.
func TestTryCmdRegistration(t *testing.T) {
	want := map[string]bool{"try": false, "keep": false, "gc": false}
	for _, c := range rootCmd.Commands() {
		if _, ok := want[c.Name()]; ok {
			want[c.Name()] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("command %q not registered on rootCmd", name)
		}
	}
}

// TestGcCmdRegistration verifies gc is registered (alias test for clarity).
func TestGcCmdRegistration(t *testing.T) {
	var found bool
	for _, c := range rootCmd.Commands() {
		if c.Name() == "gc" {
			found = true
			break
		}
	}
	if !found {
		t.Error("command \"gc\" not registered on rootCmd")
	}
}

// TestTryFlagDefaults verifies the default values of all try flags.
func TestTryFlagDefaults(t *testing.T) {
	keepFlag := tryCmd.Flags().Lookup("keep")
	if keepFlag == nil {
		t.Fatal("--keep flag not defined on try command")
	}
	if keepFlag.DefValue != "false" {
		t.Errorf("--keep default = %q, want %q", keepFlag.DefValue, "false")
	}

	detachFlag := tryCmd.Flags().Lookup("detach")
	if detachFlag == nil {
		t.Fatal("--detach flag not defined on try command")
	}
	if detachFlag.DefValue != "false" {
		t.Errorf("--detach default = %q, want %q", detachFlag.DefValue, "false")
	}

	setFlag := tryCmd.Flags().Lookup("set")
	if setFlag == nil {
		t.Fatal("--set flag not defined on try command")
	}
	// StringArray default is "[]" in cobra
	if setFlag.DefValue != "[]" {
		t.Errorf("--set default = %q, want %q", setFlag.DefValue, "[]")
	}
}

// TestTryRejectsJSONWithoutDetach verifies that --json without -d returns a usage error.
func TestTryRejectsJSONWithoutDetach(t *testing.T) {
	// Reset flag state.
	origJSON := jsonOut
	origDetach := tryDetach
	defer func() {
		jsonOut = origJSON
		tryDetach = origDetach
	}()

	jsonOut = true
	tryDetach = false

	// Call RunE directly with a nil command (flags already set via package vars).
	err := tryCmd.RunE(tryCmd, []string{"postgres"})
	if err == nil {
		t.Fatal("expected error for --json without -d, got nil")
	}
	if !errors.Is(err, ErrUsage) {
		t.Errorf("expected ErrUsage, got %v", err)
	}
}

// TestGcFlagDefaults verifies the default values of gc flags.
func TestGcFlagDefaults(t *testing.T) {
	dryRunFlag := gcCmd.Flags().Lookup("dry-run")
	if dryRunFlag == nil {
		t.Fatal("--dry-run flag not defined on gc command")
	}
	if dryRunFlag.DefValue != "false" {
		t.Errorf("--dry-run default = %q, want %q", dryRunFlag.DefValue, "false")
	}

	yesFlag := gcCmd.Flags().Lookup("yes")
	if yesFlag == nil {
		t.Fatal("--yes flag not defined on gc command")
	}
	if yesFlag.DefValue != "false" {
		t.Errorf("--yes default = %q, want %q", yesFlag.DefValue, "false")
	}
}

// TestKeepCmdArgs verifies keep requires exactly 1 argument.
func TestKeepCmdArgs(t *testing.T) {
	cases := []struct {
		args    []string
		wantErr bool
	}{
		{[]string{}, true},
		{[]string{"mystack"}, false},
		{[]string{"a", "b"}, true},
	}
	for _, c := range cases {
		err := keepCmd.Args(keepCmd, c.args)
		if (err != nil) != c.wantErr {
			t.Errorf("keepCmd.Args(%v): err=%v, wantErr=%v", c.args, err, c.wantErr)
		}
		if c.wantErr && err != nil && !errors.Is(err, ErrUsage) {
			t.Errorf("keepCmd.Args(%v): want ErrUsage, got %v", c.args, err)
		}
	}
}

// TestTryCmdArgs verifies try requires exactly 1 argument.
func TestTryCmdArgs(t *testing.T) {
	cases := []struct {
		args    []string
		wantErr bool
	}{
		{[]string{}, true},
		{[]string{"postgres"}, false},
		{[]string{"a", "b"}, true},
	}
	for _, c := range cases {
		err := tryCmd.Args(tryCmd, c.args)
		if (err != nil) != c.wantErr {
			t.Errorf("tryCmd.Args(%v): err=%v, wantErr=%v", c.args, err, c.wantErr)
		}
		if c.wantErr && err != nil && !errors.Is(err, ErrUsage) {
			t.Errorf("tryCmd.Args(%v): want ErrUsage, got %v", c.args, err)
		}
	}
}

// TestGcCmdArgs verifies gc requires exactly 0 arguments.
func TestGcCmdArgs(t *testing.T) {
	cases := []struct {
		args    []string
		wantErr bool
	}{
		{[]string{}, false},
		{[]string{"extra"}, true},
	}
	for _, c := range cases {
		err := gcCmd.Args(gcCmd, c.args)
		if (err != nil) != c.wantErr {
			t.Errorf("gcCmd.Args(%v): err=%v, wantErr=%v", c.args, err, c.wantErr)
		}
	}
}
