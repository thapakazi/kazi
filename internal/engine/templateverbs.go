package engine

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/thapakazi/kazi/internal/template"
)

// TemplateList lists all known templates (embedded starters + on-disk).
func (e *Engine) TemplateList() ([]template.Info, error) {
	return template.List()
}

// TemplateImport copies a directory or shallow-clones a git URL into the
// catalog under the given name (defaults to the source basename).
func (e *Engine) TemplateImport(src, name string) (template.Info, error) {
	return template.Import(src, name)
}

// TemplateReset restores an embedded starter to its pristine embedded state.
func (e *Engine) TemplateReset(name string) error {
	return template.Reset(name)
}

// TemplateNew scaffolds a new template from an OCI image reference by pulling
// the image, inspecting its config, generating compose.yml + values.yaml, and
// opening $EDITOR for review. Returns the template directory path on success.
func (e *Engine) TemplateNew(ctx context.Context, name, imageRef string) (string, error) {
	return template.Scaffold(ctx, e.RT, name, imageRef, e.Out, e.Err)
}

// Eject copies a template's compose.yml byte-for-byte (interpolation intact)
// to dir (defaults to "./<template>/") and writes the effective default values
// as a .env file (sorted KEY=val lines). It returns:
//
//   - dest: the absolute path of the ejected directory
//   - addCmd: the suggested "kazi add <template> <dest>" command string
//   - err: non-nil if dir already exists, template is unknown, or I/O fails
//
// If add is true, Eject calls e.Add to register the ejected stack immediately.
// The ejected stack has no remaining link to the template.
func (e *Engine) Eject(template_, dir string, add bool) (dest, addCmd string, err error) {
	// Resolve default destination directory.
	if dir == "" {
		dir = "./" + template_
	}

	// Resolve to absolute path.
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return "", "", fmt.Errorf("eject: resolving destination path: %w", err)
	}

	// Destination must not exist.
	if _, statErr := os.Stat(absDir); statErr == nil {
		return "", "", fmt.Errorf("eject: destination %s already exists", absDir)
	}

	// Materialize the template to get its source directory.
	tmplDir, err := template.Materialize(template_)
	if err != nil {
		return "", "", fmt.Errorf("eject: %w", err)
	}

	// Find the compose file in the template directory.
	composeFile, err := findComposeFile(tmplDir)
	if err != nil {
		return "", "", fmt.Errorf("eject: template %q: %w", template_, err)
	}

	// Create the destination directory.
	if err := os.MkdirAll(absDir, 0o755); err != nil {
		return "", "", fmt.Errorf("eject: creating destination dir %s: %w", absDir, err)
	}

	// Copy compose.yml byte-for-byte (interpolation intact).
	composeBytes, err := os.ReadFile(composeFile)
	if err != nil {
		os.RemoveAll(absDir)
		return "", "", fmt.Errorf("eject: reading compose file: %w", err)
	}
	destCompose := filepath.Join(absDir, filepath.Base(composeFile))
	if err := os.WriteFile(destCompose, composeBytes, 0o644); err != nil {
		os.RemoveAll(absDir)
		return "", "", fmt.Errorf("eject: writing compose file to destination: %w", err)
	}

	// Copy any other files from the template dir except values.yaml and marker files.
	// (Only the compose.yml is strictly required; additional assets are preserved.)
	entries, err := os.ReadDir(tmplDir)
	if err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			// Skip the compose file (already copied), values.yaml, and embedded marker.
			if name == filepath.Base(composeFile) || name == "values.yaml" || name == ".kazi-embedded" {
				continue
			}
			src := filepath.Join(tmplDir, name)
			dst := filepath.Join(absDir, name)
			if copyErr := copyFileBytes(src, dst); copyErr != nil {
				// best effort — don't abort for auxiliary files
				_ = copyErr
			}
		}
	}

	// Load effective default values and write .env.
	_, vals, loadErr := template.LoadValues(tmplDir)
	if loadErr != nil {
		vals = map[string]string{}
	}
	envLines := template.FlattenEnv(vals)
	envContent := strings.Join(envLines, "\n")
	if len(envLines) > 0 {
		envContent += "\n"
	}
	envPath := filepath.Join(absDir, ".env")
	if err := os.WriteFile(envPath, []byte(envContent), 0o644); err != nil {
		os.RemoveAll(absDir)
		return "", "", fmt.Errorf("eject: writing .env: %w", err)
	}

	addCmd = "kazi add " + template_ + " " + absDir

	if add {
		if _, addErr := e.Add(template_, absDir); addErr != nil {
			// Clean up on add failure.
			os.RemoveAll(absDir)
			return "", "", fmt.Errorf("eject: registering stack: %w", addErr)
		}
	}

	return absDir, addCmd, nil
}

// copyFileBytes copies a single file preserving bytes exactly.
func copyFileBytes(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, fs.FileMode(0o644))
}
