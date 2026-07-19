package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/thapakazi/kazi/internal/compose"
	"github.com/thapakazi/kazi/internal/store"
)

// EditTarget is one user-owned file the edit flow can open in $EDITOR: its
// absolute path, a kind label ("manifest"|"compose"), and a validator to run
// after the editor saves. Both skins (CLI edit, TUI e) drive the same targets;
// only the editor-process launch differs.
type EditTarget struct {
	Path     string                          `json:"path"`
	Kind     string                          `json:"kind"`
	Validate func(ctx context.Context) error `json:"-"`
}

// EditTargets returns the user-owned files editable for a registered stack, in
// preference order: the kazi manifest always, plus the compose file for
// compose-backed stacks. Template/image/containers stacks have no user-owned
// compose file, so only the manifest is returned. It errors if the stack has no
// manifest — discovered stacks aren't editable (there is nothing kazi owns).
func (e *Engine) EditTargets(name string) ([]EditTarget, error) {
	m, err := store.LoadStack(name)
	if errors.Is(err, store.ErrNotFound) {
		return nil, fmt.Errorf("%w: %s", ErrStackNotFound, name)
	}
	if err != nil {
		return nil, err
	}

	manifestPath := store.StackPath(name)
	targets := []EditTarget{{
		Path: manifestPath,
		Kind: "manifest",
		Validate: func(ctx context.Context) error {
			return store.ValidateManifestFile(manifestPath)
		},
	}}

	// Only compose-backed stacks have a user-owned compose file to edit.
	if m.Spec.Source.Compose != "" {
		composePath := m.Spec.Source.Compose
		targets = append(targets, EditTarget{
			Path: composePath,
			Kind: "compose",
			Validate: func(ctx context.Context) error {
				return e.validateComposeFile(ctx, composePath)
			},
		})
	}
	return targets, nil
}

// ManifestTarget returns the manifest EditTarget for a stack (the default edit
// target). The manifest is always the first entry of EditTargets.
func (e *Engine) ManifestTarget(name string) (EditTarget, error) {
	targets, err := e.EditTargets(name)
	if err != nil {
		return EditTarget{}, err
	}
	return targets[0], nil
}

// ComposeTarget returns the compose EditTarget for a stack, or an error naming
// the right alternative when the stack has no user-owned compose file
// (template/image/containers). Deterministic — kazi never guesses a compose
// file for a non-compose stack.
func (e *Engine) ComposeTarget(name string) (EditTarget, error) {
	targets, err := e.EditTargets(name)
	if err != nil {
		return EditTarget{}, err
	}
	for _, t := range targets {
		if t.Kind == "compose" {
			return t, nil
		}
	}
	return EditTarget{}, fmt.Errorf(
		"stack %q has no user-owned compose file; edit its values via `kazi edit %s` (the manifest), or the template via `kazi template`",
		name, name)
}

// validateComposeFile runs `<runtime> compose config --quiet` over composePath,
// the same check `kazi template` uses. A non-zero exit means the compose file
// no longer parses.
func (e *Engine) validateComposeFile(ctx context.Context, composePath string) error {
	dir := filepath.Dir(composePath)
	cmd := e.RT.ComposeCmd(ctx, "kazi-edit-check", dir, []string{composePath}, "config", "--quiet")
	if _, err := compose.Output(cmd); err != nil {
		return fmt.Errorf("compose config failed: %w", err)
	}
	return nil
}

// ResolveEditor picks the editor binary for the edit flow: an explicit override
// wins, then $EDITOR, then $VISUAL, then "vi". The process launch itself stays
// in the skins (CLI execs it; the TUI suspends via tea.ExecProcess).
func ResolveEditor(override string) string {
	if override != "" {
		return override
	}
	if ed := os.Getenv("EDITOR"); ed != "" {
		return ed
	}
	if ed := os.Getenv("VISUAL"); ed != "" {
		return ed
	}
	return "vi"
}
