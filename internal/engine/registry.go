package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/thapakazi/kazi/internal/store"
)

// composeNames is the search order inside a directory argument.
var composeNames = []string{"compose.yaml", "compose.yml", "docker-compose.yaml", "docker-compose.yml"}

// Add registers a stack: path is a compose file or a directory containing
// one. Writes the manifest; never touches containers.
func (e *Engine) Add(name, path string) (store.Manifest, error) {
	if _, err := store.LoadStack(name); err == nil {
		return store.Manifest{}, fmt.Errorf("stack %q already exists", name)
	} else if !errors.Is(err, store.ErrNotFound) {
		return store.Manifest{}, err
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return store.Manifest{}, err
	}
	fi, err := os.Stat(abs)
	if err != nil {
		return store.Manifest{}, fmt.Errorf("path %s does not exist", abs)
	}
	if fi.IsDir() {
		found := ""
		for _, n := range composeNames {
			candidate := filepath.Join(abs, n)
			if _, err := os.Stat(candidate); err == nil {
				found = candidate
				break
			}
		}
		if found == "" {
			return store.Manifest{}, fmt.Errorf("no compose file (compose.y(a)ml or docker-compose.y(a)ml) found in %s", abs)
		}
		abs = found
	}

	m := store.Manifest{APIVersion: "kazi.dev/v1alpha1", Kind: "Stack"}
	m.Metadata.Name = name
	m.Spec.Source.Compose = abs
	if err := store.SaveStack(m); err != nil {
		return store.Manifest{}, err
	}
	return m, nil
}

// Remove deregisters a stack — deletes the manifest only, never touches
// containers.
func (e *Engine) Remove(name string) error {
	err := store.DeleteStack(name)
	if errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("%w: %s", ErrStackNotFound, name)
	}
	return err
}

// Jump returns the stack's project directory.
func (e *Engine) Jump(ctx context.Context, name string) (string, error) {
	t, err := e.resolve(ctx, name)
	if err != nil {
		return "", err
	}
	return t.dir, nil
}
