package engine

import (
	"context"
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/thapakazi/kazi/internal/compose"
	"github.com/thapakazi/kazi/internal/labels"
	"github.com/thapakazi/kazi/internal/runtime"
	"github.com/thapakazi/kazi/internal/store"
)

// RunImage registers a persistent image-backed stack and starts it.
// name "" → derived from the image ref (basename minus tag, DNS-cleaned).
// ports ("8080:80"), envs ("K=V"), vols ("data:/path") land in
// spec.expose (pinned)/spec.values/spec.volumes; then the image strategy's
// up creates + starts (labels, network, alias) and syncProxy runs.
func (e *Engine) RunImage(ctx context.Context, name, image string, ports, envs, vols []string) (string, error) {
	// Derive name from image if not provided.
	if name == "" {
		name = deriveImageName(image)
	}

	// Validate the derived or given name.
	if !store.IsDNSLabel(name) {
		return "", fmt.Errorf("invalid stack name %q: must be a DNS label ([a-z0-9-], max 63 chars)", name)
	}

	// Reject if name already exists.
	if _, err := store.LoadStack(name); err == nil {
		return "", fmt.Errorf("stack %q already exists", name)
	}

	// Parse ports into ExposeSpec entries.
	// Accepted formats: "hostPort:containerPort" or bare "port" (meaning port:port).
	// Store the full mapping string verbatim so imageStrategy renders it correctly.
	var expose []store.ExposeSpec
	for _, p := range ports {
		parts := strings.SplitN(p, ":", 2)
		switch len(parts) {
		case 1:
			if !isPortNumber(parts[0]) {
				return "", fmt.Errorf("invalid port mapping %q: expected a port number or hostPort:containerPort", p)
			}
		case 2:
			if !isPortNumber(parts[0]) || !isPortNumber(parts[1]) {
				return "", fmt.Errorf("invalid port mapping %q: both sides of ':' must be port numbers", p)
			}
		}
		expose = append(expose, store.ExposeSpec{
			Service: name,
			Port:    p,
		})
	}

	// Parse envs "K=V" into Values map (keys lower-cased to match kazi convention).
	var values map[string]string
	if len(envs) > 0 {
		values = make(map[string]string, len(envs))
		for _, kv := range envs {
			k, v, _ := strings.Cut(kv, "=")
			values[strings.ToLower(k)] = v
		}
	}

	// Build and write the manifest.
	m := store.Manifest{APIVersion: "kazi.dev/v1alpha1", Kind: "Stack"}
	m.Metadata.Name = name
	m.Metadata.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	m.Spec.Source.Image = image
	m.Spec.Expose = expose
	m.Spec.Values = values
	if len(vols) > 0 {
		m.Spec.Volumes = vols
	}
	if err := store.SaveStack(m); err != nil {
		return "", err
	}

	// Delegate to Up which dispatches through imageStrategy.
	if err := e.Up(ctx, name); err != nil {
		// Best-effort cleanup: remove the manifest if Up fails.
		_ = store.DeleteStack(name)
		return "", err
	}

	return name, nil
}

// isPortNumber returns true if s is a valid TCP/UDP port number string (1–65535).
func isPortNumber(s string) bool {
	n, err := strconv.Atoi(s)
	return err == nil && n >= 1 && n <= 65535
}

// nonDNSRe matches characters that are not valid in a DNS label segment.
var nonDNSRe = regexp.MustCompile(`[^a-z0-9-]+`)

// deriveImageName produces a valid DNS label from an image reference.
// Algorithm: take basename (after last '/'), strip tag (after ':') or digest
// (after '@'), lowercase, replace non-DNS chars with '-', collapse repeated
// dashes, trim leading/trailing '-'. Falls back to "app" if result is invalid.
func deriveImageName(imageRef string) string {
	// Take the basename (after the last '/').
	base := path.Base(imageRef)

	// Strip digest (@sha256:...) first, then tag (:tag).
	if i := strings.IndexByte(base, '@'); i >= 0 {
		base = base[:i]
	}
	if i := strings.IndexByte(base, ':'); i >= 0 {
		base = base[:i]
	}

	// Lowercase.
	base = strings.ToLower(base)

	// Replace non-DNS chars (anything not a-z0-9-) with '-'.
	base = nonDNSRe.ReplaceAllString(base, "-")

	// Collapse repeated dashes.
	for strings.Contains(base, "--") {
		base = strings.ReplaceAll(base, "--", "-")
	}

	// Trim leading/trailing dashes.
	base = strings.Trim(base, "-")

	if base == "" || !store.IsDNSLabel(base) {
		return "app"
	}
	return base
}

// Adopt writes a containers-source manifest for existing containers.
// Rejections: unknown container name; compose-labeled container (error names
// its discovered stack: "container %q belongs to compose project %q — use
// `kazi up %s` instead"); name collisions. For each adopted container that
// classifies HTTP (parsePsPorts), best-effort `network connect --alias
// <container>.<name> kazi <container>` (failure = warning, not error), then
// syncProxy.
func (e *Engine) Adopt(ctx context.Context, name string, containers []string) error {
	// Validate name.
	if !store.IsDNSLabel(name) {
		return fmt.Errorf("invalid stack name %q: must be a DNS label ([a-z0-9-], max 63 chars)", name)
	}

	// Reject collision.
	if _, err := store.LoadStack(name); err == nil {
		return fmt.Errorf("stack %q already exists", name)
	}

	// List containers to validate each requested container.
	cs, err := e.RT.Ps(ctx)
	if err != nil {
		return err
	}
	byName := make(map[string]runtime.Container, len(cs))
	for _, c := range cs {
		byName[c.Name] = c
	}

	// Validate containers and reject any compose-project-labeled ones.
	for _, cname := range containers {
		c, ok := byName[cname]
		if !ok {
			return fmt.Errorf("container %q not found — is it running?", cname)
		}
		if proj := c.Labels[labels.ComposeProject]; proj != "" {
			return fmt.Errorf("container %q belongs to compose project %q — use `kazi up %s` instead", cname, proj, proj)
		}
	}

	// Build and write the manifest.
	m := store.Manifest{APIVersion: "kazi.dev/v1alpha1", Kind: "Stack"}
	m.Metadata.Name = name
	m.Metadata.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	m.Spec.Source.Containers = containers
	if err := store.SaveStack(m); err != nil {
		return err
	}

	// Best-effort: for each HTTP container, connect it to the kazi network
	// with the alias <container>.<name>.
	for _, cname := range containers {
		c := byName[cname]
		ports := parsePsPorts(c.Ports)
		_, isHTTP := classifyPorts(ports, e.Cfg)
		if !isHTTP {
			continue
		}
		alias := cname + "." + name
		cmd := e.RT.Cmd(ctx, "network", "connect", "--alias", alias, "kazi", cname)
		if err := compose.Run(cmd, e.Out, e.Err); err != nil {
			fmt.Fprintf(e.Err, "kazi: warning: network connect --alias %s kazi %s: %v\n", alias, cname, err)
		}
	}

	// Sync proxy to pick up new routes.
	e.syncProxy(ctx, "", name, nil)
	return nil
}
