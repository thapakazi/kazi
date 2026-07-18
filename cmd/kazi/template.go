package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/thapakazi/kazi/internal/template"
)

// templateCmd is the parent command for template operations.
var templateCmd = &cobra.Command{
	Use:   "template",
	Short: "Manage the template catalog",
}

// templateLsCmd lists all known templates in the catalog.
var templateLsCmd = &cobra.Command{
	Use:   "ls",
	Short: "List templates in the catalog",
	Args:  exactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		eng, err := buildEngine()
		if err != nil {
			return err
		}
		infos, err := eng.TemplateList()
		if err != nil {
			return err
		}
		if jsonOut {
			return printEnvelope("TemplateList", infos)
		}
		w := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
		fmt.Fprintln(w, "NAME\tDESCRIPTION\tSOURCE")
		for _, info := range infos {
			src := "custom"
			if info.Embedded {
				src = "embedded"
			}
			fmt.Fprintf(w, "%s\t%s\t%s\n", info.Name, info.Description, src)
		}
		return w.Flush()
	},
}

var templateNewFromImage string

// templateNewCmd scaffolds a new template from an OCI image reference.
// The re-edit loop lives here (CLI responsibility per the documented invariant):
// on ErrInvalidTemplate, prompt "re-edit? [Y/n]", re-run OpenEditor + ValidateCompose;
// if declined, remove the template dir and exit 1.
var templateNewCmd = &cobra.Command{
	Use:   "new <name>",
	Short: "Scaffold a new template from an OCI image",
	Args:  exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if templateNewFromImage == "" {
			return fmt.Errorf("%w: --from-image is required", ErrUsage)
		}
		eng, err := buildEngine()
		if err != nil {
			return err
		}
		name := args[0]
		imageRef := templateNewFromImage

		dir, err := eng.TemplateNew(cmd.Context(), name, imageRef)
		if err == nil {
			fmt.Printf("created template %q at %s\n", name, dir)
			return nil
		}
		if !errors.Is(err, template.ErrInvalidTemplate) {
			return err
		}

		// Re-edit loop: ErrInvalidTemplate — the template dir exists but failed validation.
		// Scaffold intentionally leaves the dir in place for us to offer re-edit.
		templateDir := filepath.Join(template.Dir(), name)
		composePath := filepath.Join(templateDir, "compose.yml")

		for errors.Is(err, template.ErrInvalidTemplate) {
			fmt.Fprintf(os.Stderr, "template validation failed: %v\nre-edit? [Y/n] ", err)
			line, scanErr := bufio.NewReader(os.Stdin).ReadString('\n')
			declined := scanErr != nil || strings.HasPrefix(strings.ToLower(strings.TrimSpace(line)), "n")
			if declined {
				// User declined re-edit: remove the dir (CLI's cleanup responsibility).
				os.RemoveAll(templateDir)
				return fmt.Errorf("%w after validation failure", template.ErrAborted)
			}
			// Re-open editor on the compose.yml.
			if openErr := template.OpenEditor(composePath); openErr != nil {
				os.RemoveAll(templateDir)
				return fmt.Errorf("editor error: %w", openErr)
			}
			// Re-validate.
			err = template.ValidateCompose(cmd.Context(), eng.RT, templateDir)
		}
		if err != nil {
			return err
		}
		fmt.Printf("created template %q at %s\n", name, templateDir)
		return nil
	},
}

// templateImportCmd imports a directory or git URL into the catalog.
var templateImportCmd = &cobra.Command{
	Use:   "import <src> [name]",
	Short: "Import a directory or git URL as a template",
	Args:  rangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		eng, err := buildEngine()
		if err != nil {
			return err
		}
		src := args[0]
		name := ""
		if len(args) == 2 {
			name = args[1]
		}
		info, err := eng.TemplateImport(src, name)
		if err != nil {
			return err
		}
		if jsonOut {
			return printEnvelope("TemplateList", []interface{}{info})
		}
		fmt.Printf("imported template %q from %s\n", info.Name, src)
		return nil
	},
}

var templateResetYes bool

// templateResetCmd resets an embedded template to its pristine state.
var templateResetCmd = &cobra.Command{
	Use:   "reset <name>",
	Short: "Reset an embedded template to its pristine state",
	Args:  exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		eng, err := buildEngine()
		if err != nil {
			return err
		}
		name := args[0]
		if !templateResetYes {
			fmt.Fprintf(os.Stderr, "reset template %q to pristine state? [y/N] ", name)
			var resp string
			fmt.Fscanln(os.Stdin, &resp)
			if !strings.HasPrefix(strings.ToLower(resp), "y") {
				fmt.Fprintln(os.Stderr, "aborted")
				return nil
			}
		}
		if err := eng.TemplateReset(name); err != nil {
			return err
		}
		if jsonOut {
			return printResult("template-reset", name)
		}
		fmt.Printf("reset template %q to pristine state\n", name)
		return nil
	},
}

func init() {
	templateNewCmd.Flags().StringVar(&templateNewFromImage, "from-image", "", "OCI image reference to scaffold from (required)")
	templateResetCmd.Flags().BoolVar(&templateResetYes, "yes", false, "skip confirmation")

	templateCmd.AddCommand(templateLsCmd, templateNewCmd, templateImportCmd, templateResetCmd)
	rootCmd.AddCommand(templateCmd)
}
