package engine

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/thapakazi/kazi/internal/labels"
	"github.com/thapakazi/kazi/internal/store"
)

// hostGateway is the Caddy dial target for static routes: the Docker host, where
// externally-run services publish their ports.
const hostGateway = "host.docker.internal"

// RouteList returns the configured static routes.
func (e *Engine) RouteList() ([]store.StaticRoute, error) {
	return store.LoadRoutes()
}

// RouteAdd upserts a static route (<host>.localhost → host.docker.internal:<port>)
// and re-syncs the proxy so it takes effect immediately when the proxy is up.
// stack is the optional originating stack (kazi route from) so `kazi urls`/the
// TUI can list the route under it.
func (e *Engine) RouteAdd(ctx context.Context, host string, port int, note, stack string) error {
	if !store.IsDNSLabel(host) {
		return fmt.Errorf("invalid route host %q: must be a DNS label ([a-z0-9-], max 63 chars)", host)
	}
	if port < 1 || port > 65535 {
		return fmt.Errorf("invalid port %d: must be 1–65535", port)
	}
	routes, err := store.LoadRoutes()
	if err != nil {
		return err
	}
	found := false
	for i := range routes {
		if routes[i].Host == host {
			routes[i].Port, routes[i].Note, routes[i].Stack = port, note, stack
			found = true
			break
		}
	}
	if !found {
		routes = append(routes, store.StaticRoute{Host: host, Port: port, Note: note, Stack: stack})
	}
	if err := store.SaveRoutes(routes); err != nil {
		return err
	}
	e.syncProxy(ctx, "", "", nil)
	return nil
}

// staticRouteEndpoints returns the configured static routes as Endpoints for the
// urls view. When stack != "" only that stack's routes are returned; when empty,
// all routes are returned (the global `kazi urls` listing).
func (e *Engine) staticRouteEndpoints(stack string) []Endpoint {
	routes, err := store.LoadRoutes()
	if err != nil {
		return nil
	}
	var out []Endpoint
	for _, r := range routes {
		if stack != "" && r.Stack != stack {
			continue
		}
		out = append(out, Endpoint{
			Stack:   r.Stack,
			Service: r.Host,
			Kind:    "http",
			URL:     "https://" + r.Host + ".localhost",
			Target:  hostGateway + ":" + strconv.Itoa(r.Port),
			Note:    r.Note,
		})
	}
	return out
}

// RouteRemove deletes the static route with the given host and re-syncs.
func (e *Engine) RouteRemove(ctx context.Context, host string) error {
	routes, err := store.LoadRoutes()
	if err != nil {
		return err
	}
	kept := make([]store.StaticRoute, 0, len(routes))
	removed := false
	for _, r := range routes {
		if r.Host == host {
			removed = true
			continue
		}
		kept = append(kept, r)
	}
	if !removed {
		return fmt.Errorf("no route named %q", host)
	}
	if err := store.SaveRoutes(kept); err != nil {
		return err
	}
	e.syncProxy(ctx, "", "", nil)
	return nil
}

// RouteCandidate is a static route suggested from a stack's published ports.
type RouteCandidate struct {
	Host    string `json:"host"`    // suggested *.localhost subdomain
	Port    int    `json:"port"`    // published host port
	Service string `json:"service"` // originating compose service / container
	Target  int    `json:"target"`  // container port behind the published port
}

// RoutesFromStack inspects a stack's containers and suggests one static route per
// host-published port (studio → host.docker.internal:54323, …). Hostnames are
// derived from the service (or container) name; the caller confirms/renames
// before adding. It works for any stack — most usefully a discovered one kazi
// can't reverse-proxy over its own network.
func (e *Engine) RoutesFromStack(ctx context.Context, stack string) ([]RouteCandidate, error) {
	cs, err := e.RT.Ps(ctx)
	if err != nil {
		return nil, err
	}
	var out []RouteCandidate
	seen := map[int]bool{}
	for _, c := range cs {
		if c.Labels[labels.ComposeProject] != stack && c.Labels[labels.Stack] != stack {
			continue
		}
		base := c.Labels[labels.ComposeService]
		if base == "" {
			base = c.Name
		}
		for _, pm := range parsePublishedPorts(c.Ports) {
			if seen[pm.host] {
				continue
			}
			seen[pm.host] = true
			out = append(out, RouteCandidate{
				Host:    routeHost(base, stack),
				Port:    pm.host,
				Service: base,
				Target:  pm.container,
			})
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("stack %q has no host-published ports to route (services must publish a port, e.g. -p 54323:3000)", stack)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Port < out[j].Port })
	return out, nil
}

// portMapping is one published-port entry: host port → container port.
type portMapping struct{ host, container int }

// parsePublishedPorts extracts published mappings from a docker ps Ports string,
// e.g. "0.0.0.0:54323->3000/tcp, 8080/tcp" → [{54323,3000}] (bare/unpublished
// ports are ignored). IPv6 duplicates ("[::]:54323->3000") collapse by host port.
func parsePublishedPorts(s string) []portMapping {
	var out []portMapping
	seen := map[int]bool{}
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		arrow := strings.Index(part, "->")
		if arrow < 0 {
			continue // not published to the host
		}
		left, right := part[:arrow], part[arrow+2:]
		// host port is after the last ':' on the left ("0.0.0.0:54323").
		hp := left
		if i := strings.LastIndexByte(left, ':'); i >= 0 {
			hp = left[i+1:]
		}
		// container port is before the '/proto' on the right ("3000/tcp").
		cp := right
		if i := strings.IndexByte(cp, '/'); i >= 0 {
			cp = cp[:i]
		}
		host, herr := strconv.Atoi(strings.TrimSpace(hp))
		cont, cerr := strconv.Atoi(strings.TrimSpace(cp))
		if herr != nil || cerr != nil || host <= 0 || seen[host] {
			continue
		}
		seen[host] = true
		out = append(out, portMapping{host: host, container: cont})
	}
	return out
}

var routeNonDNS = regexp.MustCompile(`[^a-z0-9-]+`)

// routeHost derives a DNS-label hostname from a service/container name, trimming
// a redundant stack suffix (supabase_studio_<stack> → supabase-studio).
func routeHost(base, stack string) string {
	h := strings.ToLower(base)
	h = routeNonDNS.ReplaceAllString(h, "-")
	h = strings.Trim(h, "-")
	// Drop a trailing "-<stack>" the compose project appended to container names.
	h = strings.TrimSuffix(h, "-"+strings.ToLower(stack))
	h = strings.Trim(h, "-")
	if h == "" || !store.IsDNSLabel(h) {
		h = stack
	}
	return h
}
