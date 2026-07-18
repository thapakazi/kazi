// Package template manages the kazi template catalog: embedded starters,
// on-disk user templates, import from directory or git URL, and pristine
// reset of embedded templates. Values loading, merging, and env-flattening
// live in values.go.
package template

import (
	"embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/thapakazi/kazi/internal/store"
)

//go:embed all:starters
var embeddedStarters embed.FS

// Info describes a template in the catalog.
type Info struct {
	Name        string
	Description string
	Path        string
	Embedded    bool
}

// Dir returns the directory where templates are stored on disk.
func Dir() string {
	return filepath.Join(store.Root(), "templates")
}

// List returns all known templates: embedded starters plus any on-disk
// templates. When both sources have the same name, the on-disk entry wins.
// Results are sorted by name.
func List() ([]Info, error) {
	// Start with embedded starters.
	byName := map[string]Info{}

	entries, err := embeddedStarters.ReadDir("starters")
	if err != nil {
		return nil, fmt.Errorf("reading embedded starters: %w", err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		desc, _, err := loadValuesFromFS(embeddedStarters, filepath.Join("starters", name))
		if err != nil {
			desc = ""
		}
		byName[name] = Info{
			Name:        name,
			Description: desc,
			Embedded:    true,
			// Path is populated only when materialized; List returns empty path for un-materialized embedded templates.
		}
	}

	// On-disk templates under Dir() override embedded ones for same name.
	diskDir := Dir()
	diskEntries, err := os.ReadDir(diskDir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("reading templates dir %s: %w", diskDir, err)
	}
	for _, e := range diskEntries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		tplPath := filepath.Join(diskDir, name)
		desc, _, err := LoadValues(tplPath)
		if err != nil {
			desc = ""
		}
		existing, isEmbedded := byName[name]
		byName[name] = Info{
			Name:        name,
			Description: desc,
			Path:        tplPath,
			Embedded:    isEmbedded && existing.Embedded,
		}
	}

	out := make([]Info, 0, len(byName))
	for _, info := range byName {
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Materialize ensures the named template exists on disk by copying the
// embedded starter on first use. It never overwrites an existing directory.
// Returns the directory path; unknown name → error listing available starters.
func Materialize(name string) (string, error) {
	dest := filepath.Join(Dir(), name)

	// If already on disk, return immediately without touching it.
	if _, err := os.Stat(dest); err == nil {
		return dest, nil
	}

	// Verify this is a known embedded starter.
	embeddedPath := filepath.Join("starters", name)
	if _, err := embeddedStarters.ReadDir(embeddedPath); err != nil {
		available := embeddedNames()
		return "", fmt.Errorf("unknown template %q; available embedded starters: %s", name, strings.Join(available, ", "))
	}

	if err := copyEmbedDir(embeddedPath, dest); err != nil {
		return "", fmt.Errorf("materializing template %q: %w", name, err)
	}
	return dest, nil
}

// Reset deletes the on-disk directory for an embedded template and
// re-materializes the pristine embedded copy. Non-embedded (imported)
// templates and unknown names return errors.
func Reset(name string) error {
	// Check it is a known embedded starter.
	embeddedPath := filepath.Join("starters", name)
	if _, err := embeddedStarters.ReadDir(embeddedPath); err != nil {
		return fmt.Errorf("reset: %q is not a known embedded starter", name)
	}

	// Verify it hasn't been imported as a non-embedded template (no collision).
	// If the disk path exists, we still allow reset for embedded templates.
	// For non-embedded, the starter dir would not exist — but user may have
	// imported a dir with the same name. We detect this by checking List().
	infos, err := List()
	if err != nil {
		return err
	}
	for _, info := range infos {
		if info.Name == name && !info.Embedded {
			return fmt.Errorf("reset: %q is not an embedded template; use 'kazi template import' to manage it", name)
		}
	}

	dest := filepath.Join(Dir(), name)
	// Remove existing directory.
	if err := os.RemoveAll(dest); err != nil {
		return fmt.Errorf("reset: removing %s: %w", dest, err)
	}
	// Re-materialize.
	if _, err := Materialize(name); err != nil {
		return fmt.Errorf("reset: re-materializing %q: %w", name, err)
	}
	return nil
}

// Import copies a directory or shallow-clones a git URL into the catalog as
// the given name. If name is empty, the basename of src is used.
// The source directory (or clone) must contain a compose file.
// Collisions are an error.
func Import(src, name string) (Info, error) {
	if name == "" {
		name = filepath.Base(src)
	}

	dest := filepath.Join(Dir(), name)
	if _, err := os.Stat(dest); err == nil {
		return Info{}, fmt.Errorf("template %q already exists; pass an explicit name to import under a different name", name)
	}

	var srcDir string
	if isGitURL(src) {
		tmp, err := os.MkdirTemp("", "kazi-import-*")
		if err != nil {
			return Info{}, fmt.Errorf("import: creating temp dir: %w", err)
		}
		defer os.RemoveAll(tmp)

		cmd := exec.Command("git", "clone", "--depth", "1", src, tmp)
		cmd.Stdout = os.Stderr
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return Info{}, fmt.Errorf("import: git clone %s: %w", src, err)
		}
		srcDir = tmp
	} else {
		srcDir = src
	}

	// Validate: must contain a compose file.
	if !hasComposeFile(srcDir) {
		return Info{}, fmt.Errorf("import: %s does not contain a compose file (compose.yml, compose.yaml, docker-compose.yml, or docker-compose.yaml)", srcDir)
	}

	if err := copyDir(srcDir, dest); err != nil {
		return Info{}, fmt.Errorf("import: copying %s → %s: %w", srcDir, dest, err)
	}

	desc, _, _ := LoadValues(dest)
	return Info{
		Name:        name,
		Description: desc,
		Path:        dest,
		Embedded:    false,
	}, nil
}

// isGitURL reports whether src looks like a git URL.
func isGitURL(src string) bool {
	return strings.HasPrefix(src, "git@") ||
		strings.HasPrefix(src, "https://") ||
		strings.HasPrefix(src, "http://") ||
		strings.HasPrefix(src, "git://") ||
		strings.HasPrefix(src, "ssh://")
}

// hasComposeFile returns true if dir contains any recognised compose file.
func hasComposeFile(dir string) bool {
	for _, name := range []string{"compose.yml", "compose.yaml", "docker-compose.yml", "docker-compose.yaml"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return true
		}
	}
	return false
}

// embeddedNames returns the sorted list of embedded starter names.
func embeddedNames() []string {
	entries, _ := embeddedStarters.ReadDir("starters")
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names
}

// copyEmbedDir copies a directory tree from the embedded FS into dest.
func copyEmbedDir(src, dest string) error {
	return fs.WalkDir(embeddedStarters, src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dest, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := embeddedStarters.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}

// copyDir copies a directory tree from src (OS path) into dest.
func copyDir(src, dest string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dest, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyFile(path, target)
	})
}

// copyFile copies a single file from src to dest.
func copyFile(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

// loadValuesFromFS reads values.yaml from an embed.FS path, returning
// the description and key-value pairs.
func loadValuesFromFS(fsys embed.FS, dir string) (desc string, vals map[string]string, err error) {
	data, err := fsys.ReadFile(filepath.Join(dir, "values.yaml"))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", map[string]string{}, nil
		}
		return "", nil, err
	}
	return parseValuesYAML(data)
}
