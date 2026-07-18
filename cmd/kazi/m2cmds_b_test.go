package main

import (
	"errors"
	"testing"

	"github.com/spf13/cobra"
)

// TestM2BCmdRegistration verifies that run, adopt, eject, and template (with
// its subcommands) are registered on rootCmd.
func TestM2BCmdRegistration(t *testing.T) {
	t.Run("top-level commands", func(t *testing.T) {
		want := map[string]bool{
			"run":      false,
			"adopt":    false,
			"eject":    false,
			"template": false,
		}
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
	})

	t.Run("template subcommands", func(t *testing.T) {
		var tCmd *cobra.Command
		for _, c := range rootCmd.Commands() {
			if c.Name() == "template" {
				tCmd = c
				break
			}
		}
		if tCmd == nil {
			t.Fatal("template command not found")
		}

		want := map[string]bool{
			"ls":     false,
			"new":    false,
			"import": false,
			"reset":  false,
		}
		for _, c := range tCmd.Commands() {
			if _, ok := want[c.Name()]; ok {
				want[c.Name()] = true
			}
		}
		for name, found := range want {
			if !found {
				t.Errorf("template subcommand %q not registered", name)
			}
		}
	})
}

// TestM2BRunFlagShapes verifies run command flags and their defaults.
func TestM2BRunFlagShapes(t *testing.T) {
	nameFlag := runCmd.Flags().Lookup("name")
	if nameFlag == nil {
		t.Fatal("--name flag not defined on run command")
	}
	if nameFlag.DefValue != "" {
		t.Errorf("--name default = %q, want empty string", nameFlag.DefValue)
	}

	pFlag := runCmd.Flags().Lookup("publish")
	if pFlag == nil {
		t.Fatal("-p/--publish flag not defined on run command")
	}

	eFlag := runCmd.Flags().Lookup("env")
	if eFlag == nil {
		t.Fatal("-e/--env flag not defined on run command")
	}

	vFlag := runCmd.Flags().Lookup("volume")
	if vFlag == nil {
		t.Fatal("-v/--volume flag not defined on run command")
	}
}

// TestM2BAdoptMinArgs verifies adopt rejects fewer than 2 arguments with ErrUsage.
func TestM2BAdoptMinArgs(t *testing.T) {
	// 0 args: should fail with ErrUsage
	if err := adoptCmd.Args(adoptCmd, []string{}); !errors.Is(err, ErrUsage) {
		t.Errorf("adopt with 0 args: want ErrUsage, got %v", err)
	}
	// 1 arg: should fail with ErrUsage (need at least name + 1 container)
	if err := adoptCmd.Args(adoptCmd, []string{"mystack"}); !errors.Is(err, ErrUsage) {
		t.Errorf("adopt with 1 arg: want ErrUsage, got %v", err)
	}
	// 2 args: should succeed
	if err := adoptCmd.Args(adoptCmd, []string{"mystack", "container1"}); err != nil {
		t.Errorf("adopt with 2 args: want nil, got %v", err)
	}
	// 3 args: should succeed
	if err := adoptCmd.Args(adoptCmd, []string{"mystack", "c1", "c2"}); err != nil {
		t.Errorf("adopt with 3 args: want nil, got %v", err)
	}
}

// TestM2BEjectFlags verifies eject command flags.
func TestM2BEjectFlags(t *testing.T) {
	addFlag := ejectCmd.Flags().Lookup("add")
	if addFlag == nil {
		t.Fatal("--add flag not defined on eject command")
	}
	if addFlag.DefValue != "false" {
		t.Errorf("--add default = %q, want %q", addFlag.DefValue, "false")
	}
}

// TestM2BEjectArgs verifies eject accepts 1 or 2 args.
func TestM2BEjectArgs(t *testing.T) {
	// 0 args: should fail
	if err := ejectCmd.Args(ejectCmd, []string{}); !errors.Is(err, ErrUsage) {
		t.Errorf("eject with 0 args: want ErrUsage, got %v", err)
	}
	// 1 arg: should succeed
	if err := ejectCmd.Args(ejectCmd, []string{"postgres"}); err != nil {
		t.Errorf("eject with 1 arg: want nil, got %v", err)
	}
	// 2 args: should succeed
	if err := ejectCmd.Args(ejectCmd, []string{"postgres", "./mydir"}); err != nil {
		t.Errorf("eject with 2 args: want nil, got %v", err)
	}
	// 3 args: should fail
	if err := ejectCmd.Args(ejectCmd, []string{"postgres", "./mydir", "extra"}); !errors.Is(err, ErrUsage) {
		t.Errorf("eject with 3 args: want ErrUsage, got %v", err)
	}
}

// TestM2BTemplateNewRequiresFromImage verifies that "template new" returns
// ErrUsage when --from-image is missing.
func TestM2BTemplateNewRequiresFromImage(t *testing.T) {
	// Save and restore the flag value.
	saved := templateNewFromImage
	templateNewFromImage = ""
	defer func() { templateNewFromImage = saved }()

	// Simulate RunE with no --from-image set.
	err := templateNewCmd.RunE(templateNewCmd, []string{"mytemplate"})
	if !errors.Is(err, ErrUsage) {
		t.Errorf("template new without --from-image: want ErrUsage, got %v", err)
	}
}

// TestM2BTemplateNewFromImageFlag verifies the --from-image flag is registered.
func TestM2BTemplateNewFromImageFlag(t *testing.T) {
	f := templateNewCmd.Flags().Lookup("from-image")
	if f == nil {
		t.Fatal("--from-image flag not defined on template new command")
	}
	if f.DefValue != "" {
		t.Errorf("--from-image default = %q, want empty string", f.DefValue)
	}
}

// TestM2BTemplateLsArgs verifies template ls requires no arguments.
func TestM2BTemplateLsArgs(t *testing.T) {
	// 0 args: should pass the arg check.
	if err := templateLsCmd.Args(templateLsCmd, []string{}); err != nil {
		t.Errorf("template ls with 0 args: want nil, got %v", err)
	}
	// 1 arg: should fail.
	if err := templateLsCmd.Args(templateLsCmd, []string{"extra"}); !errors.Is(err, ErrUsage) {
		t.Errorf("template ls with 1 arg: want ErrUsage, got %v", err)
	}
}

// TestM2BTemplateResetFlags verifies template reset flags.
func TestM2BTemplateResetFlags(t *testing.T) {
	f := templateResetCmd.Flags().Lookup("yes")
	if f == nil {
		t.Fatal("--yes flag not defined on template reset command")
	}
	if f.DefValue != "false" {
		t.Errorf("--yes default = %q, want %q", f.DefValue, "false")
	}
}

// TestM2BTemplateImportArgs verifies template import accepts 1 or 2 args.
func TestM2BTemplateImportArgs(t *testing.T) {
	if err := templateImportCmd.Args(templateImportCmd, []string{}); !errors.Is(err, ErrUsage) {
		t.Errorf("template import with 0 args: want ErrUsage, got %v", err)
	}
	if err := templateImportCmd.Args(templateImportCmd, []string{"./mydir"}); err != nil {
		t.Errorf("template import with 1 arg: want nil, got %v", err)
	}
	if err := templateImportCmd.Args(templateImportCmd, []string{"./mydir", "myname"}); err != nil {
		t.Errorf("template import with 2 args: want nil, got %v", err)
	}
	if err := templateImportCmd.Args(templateImportCmd, []string{"a", "b", "c"}); !errors.Is(err, ErrUsage) {
		t.Errorf("template import with 3 args: want ErrUsage, got %v", err)
	}
}
