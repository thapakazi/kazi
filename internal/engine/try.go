package engine

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/thapakazi/kazi/internal/proxy"
	"github.com/thapakazi/kazi/internal/store"
	"github.com/thapakazi/kazi/internal/template"
)

// TryOpts controls the behaviour of a Try session.
type TryOpts struct {
	Detach bool
	Keep   bool
	Sets   []string // --set k=v overrides
}

// Try materializes a template, picks a free DNS name, writes an ephemeral
// manifest (source.template, ephemeral: !Keep, values: --set overrides only,
// createdAt: RFC3339 UTC), brings the stack up, and returns the name and its
// endpoint URLs. It does NOT block; the CLI is responsible for foreground
// session choreography (Logs + Teardown on signal).
func (e *Engine) Try(ctx context.Context, tmpl string, opts TryOpts) (string, []Endpoint, error) {
	// 1. Ensure the template exists (materialize embedded on first use).
	if _, err := template.Materialize(tmpl); err != nil {
		return "", nil, fmt.Errorf("try: template %q: %w", tmpl, err)
	}

	// 2. Load template defaults so we can compute the override-only values.
	_, defaults, err := template.LoadValues(templateDir(tmpl))
	if err != nil {
		defaults = map[string]string{}
	}

	// 3. Build the merged map (defaults ← Sets) then reduce to overrides only.
	merged, err := template.MergeValues(defaults, opts.Sets)
	if err != nil {
		return "", nil, fmt.Errorf("try: --set: %w", err)
	}
	overrides := map[string]string{}
	for k, v := range merged {
		if d, ok := defaults[k]; !ok || d != v {
			overrides[k] = v
		}
	}

	// 4. Pick a free stack name: tmpl, then tmpl-2 … tmpl-9.
	name, err := pickName(tmpl)
	if err != nil {
		return "", nil, fmt.Errorf("try: %w", err)
	}

	// 5. Write the manifest.
	m := store.Manifest{APIVersion: "kazi.dev/v1alpha1", Kind: "Stack"}
	m.Metadata.Name = name
	m.Metadata.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	m.Spec.Source.Template = tmpl
	m.Spec.Ephemeral = !opts.Keep
	if len(overrides) > 0 {
		m.Spec.Values = overrides
	}
	if err := store.SaveStack(m); err != nil {
		return "", nil, fmt.Errorf("try: saving manifest: %w", err)
	}

	// 6. Bring the stack up.
	if err := e.Up(ctx, name); err != nil {
		// Clean up: remove manifest on up failure so gc doesn't see a ghost.
		_ = store.DeleteStack(name)
		return "", nil, fmt.Errorf("try: up: %w", err)
	}

	// 7. Collect endpoints (best-effort; empty is fine for callers).
	eps, _ := e.Urls(ctx, name)

	return name, eps, nil
}

// Teardown performs a full ephemeral cleanup in gc-safe order:
//  1. compose down -v --rmi local
//  2. FreeStack (port allocations)
//  3. delete manifest (ONLY if step 1 succeeded — gc invariant)
//  4. syncProxy
//
// Steps 2 and 4 are always attempted. The manifest is deleted LAST and only
// when the down succeeded, so a partial failure (e.g. down fails) leaves
// gc-recoverable state (manifest remains → gc can retry).
func (e *Engine) Teardown(ctx context.Context, name string) error {
	t, resolveErr := e.resolve(ctx, name)

	var errs []error
	downOK := false

	// Step 1: compose down -v --rmi local.
	if resolveErr == nil {
		if downErr := strategyFor(t.srcKind).down(ctx, e, t, "-v", "--rmi", "local"); downErr != nil {
			errs = append(errs, fmt.Errorf("down: %w", downErr))
		} else {
			downOK = true
		}
	} else {
		errs = append(errs, fmt.Errorf("resolve: %w", resolveErr))
	}

	// Step 2: free port allocations (always attempted).
	if ps, loadErr := proxy.LoadPorts(); loadErr == nil {
		ps.FreeStack(name)
	}

	// Step 3: delete manifest ONLY when down succeeded (gc invariant).
	if downOK {
		if delErr := store.DeleteStack(name); delErr != nil && !errors.Is(delErr, store.ErrNotFound) {
			errs = append(errs, fmt.Errorf("delete manifest: %w", delErr))
		}
	}

	// Step 4: sync proxy to remove the stack's routes (always attempted).
	e.syncProxy(ctx, name, "", nil)

	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

// Keep flips spec.ephemeral to false for the named stack. This is a
// manifest-only edit; no runtime calls are made and no containers are touched.
func (e *Engine) Keep(name string) error {
	m, err := store.LoadStack(name)
	if err != nil {
		return fmt.Errorf("keep: %w", err)
	}
	m.Spec.Ephemeral = false
	if err := store.SaveStack(m); err != nil {
		return fmt.Errorf("keep: saving manifest: %w", err)
	}
	return nil
}

// pickName returns name if no manifest exists for it, otherwise tries
// name-2 … name-9. Returns an error if all candidates are taken.
func pickName(name string) (string, error) {
	if _, err := store.LoadStack(name); errors.Is(err, store.ErrNotFound) {
		return name, nil
	}
	for i := 2; i <= 9; i++ {
		candidate := fmt.Sprintf("%s-%d", name, i)
		if _, err := store.LoadStack(candidate); errors.Is(err, store.ErrNotFound) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("all candidate names for template %q are taken (tried %s through %s-9)", name, name, name)
}

// templateDir returns the on-disk directory for a materialized template.
// It calls Materialize which is idempotent (no-op if already on disk).
func templateDir(tmpl string) string {
	dir, err := template.Materialize(tmpl)
	if err != nil {
		return ""
	}
	return dir
}
