package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/thapakazi/kazi/internal/compose"
	"github.com/thapakazi/kazi/internal/proxy"
	"github.com/thapakazi/kazi/internal/store"
)

// RemoveContainer force-removes a single container by name (`<runtime> rm -f`).
// It backs the TUI d:remove on an unmanaged loose container — kazi's
// control-plane cleanup for containers it doesn't otherwise manage. It never
// touches manifests; the caller is responsible for choosing the target.
func (e *Engine) RemoveContainer(ctx context.Context, name string) error {
	return e.frameCmd(compose.Run(e.RT.Cmd(ctx, "rm", "-f", name), e.Out, e.Err), "rm", name)
}

// composeNames is the search order inside a directory argument.
var composeNames = []string{"compose.yaml", "compose.yml", "docker-compose.yaml", "docker-compose.yml"}

// Add registers a stack: path is a compose file or a directory containing
// one. Writes the manifest; never touches containers.
func (e *Engine) Add(name, path string) (store.Manifest, error) {
	// DNS-label check: stack names become hostnames in M1.
	if !store.IsDNSLabel(name) {
		return store.Manifest{}, fmt.Errorf("invalid stack name %q: must be a DNS label ([a-z0-9-], max 63 chars) since names become hostnames", name)
	}

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
		found, ferr := findComposeFile(abs)
		if ferr != nil {
			return store.Manifest{}, ferr
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
	m, err := store.LoadStack(name)
	if errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("%w: %s", ErrStackNotFound, name)
	}
	if err != nil {
		return err
	}

	// Reject removal of system stacks.
	if m.Spec.System {
		return fmt.Errorf("%q is a kazi system stack and cannot be removed", name)
	}

	// Free port allocations before deleting the manifest.
	ps, loadErr := proxy.LoadPorts()
	if loadErr == nil {
		ps.FreeStack(name)
	}

	if err := store.DeleteStack(name); err != nil {
		return err
	}

	// Sync proxy to remove any routes for this stack.
	e.syncProxy(context.Background(), name, "", nil)
	return nil
}

// SetHostname sets a stack's custom *.localhost subdomain in its manifest
// (spec.proxy.hostname) and re-syncs the proxy so a running stack picks up the
// new URL live (the container's network alias is unchanged). An empty host
// clears the override (back to the stack name); a non-empty host must be a DNS
// label. Headless callers can set the same field via `kazi edit`.
func (e *Engine) SetHostname(name, host string) error {
	m, err := store.LoadStack(name)
	if errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("%w: %s", ErrStackNotFound, name)
	}
	if err != nil {
		return err
	}
	if host != "" && !store.IsDNSLabel(host) {
		return fmt.Errorf("invalid hostname %q: must be a DNS label ([a-z0-9-], max 63 chars)", host)
	}
	if m.Spec.Proxy == nil {
		m.Spec.Proxy = &store.ProxySpec{}
	}
	m.Spec.Proxy.Hostname = host
	if err := store.SaveStack(m); err != nil {
		return err
	}
	// Re-sync so a running stack's route reflects the new hostname immediately.
	e.syncProxy(context.Background(), "", "", nil)
	return nil
}

// Jump returns the stack's project directory.
func (e *Engine) Jump(ctx context.Context, name string) (string, error) {
	t, err := e.resolve(ctx, name)
	if err != nil {
		return "", err
	}
	return t.dir, nil
}
