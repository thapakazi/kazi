package engine

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/thapakazi/kazi/internal/compose"
	"github.com/thapakazi/kazi/internal/proxy"
	"github.com/thapakazi/kazi/internal/store"
)

// Endpoint describes a single reachable (or soon-to-be-reachable) service endpoint.
type Endpoint struct {
	Stack   string `json:"stack"`
	Service string `json:"service"`
	Kind    string `json:"kind"`           // "http" | "tcp" | "hint"
	URL     string `json:"url,omitempty"`  // https://blog.localhost | localhost:42017
	Target  string `json:"target"`         // web:80 | postgres:5432
	Note    string `json:"note,omitempty"` // nudges
}

// osOverride allows tests to override GOOS without real OS switching.
// When empty, runtime.GOOS is used.
var osOverride string

// realSudo is the default sudo builder: prepend "sudo" to the command.
// Exposed as a var so tests can stub it without losing the default.
var realSudo = func(cmd []string) []string {
	return append([]string{"sudo"}, cmd...)
}

// sudoRun is the active sudo builder; tests replace this to avoid real sudo.
var sudoRun = realSudo

// hostCmd builds a command that runs directly on the HOST — never through
// the container runtime. Trust's sudo/security/update-ca-certificates
// calls are host-side; routing them through rt.Cmd would prefix the
// runtime binary (`docker sudo security ...`). A var so tests can record
// invocations without a real sudo prompt.
var hostCmd = func(ctx context.Context, args []string) *exec.Cmd {
	return exec.CommandContext(ctx, args[0], args[1:]...)
}

// goos returns the effective OS name.
func goos() string {
	if osOverride != "" {
		return osOverride
	}
	return runtime.GOOS
}

// Expose upserts (or removes) a spec.expose entry for the named service,
// allocates (or frees) a host port, saves the manifest, and if the stack is
// currently running triggers a targeted `compose up -d --no-deps <svc>` with
// the rebuilt override. Returns the host port (0 on remove).
//
// Rules:
//   - Stack must be registered; unregistered → error.
//   - remove=false: upsert entry (port "auto" when port==0, else "<port>"); allocate immediately.
//   - remove=true: drop entry; Free allocation; same targeted recreate.
//   - Container port is auto-detected from compose config when the service exposes exactly one
//     port and port==0; otherwise the pinned value is used as the container port directly.
func (e *Engine) Expose(ctx context.Context, stack, service string, port int, remove bool) (int, error) {
	// 1. Load registered manifest (errors for unregistered stacks).
	m, err := store.LoadStack(stack)
	if err != nil {
		return 0, fmt.Errorf("expose: stack %q not registered: %w", stack, err)
	}

	// 2. Load port state.
	ps, err := proxy.LoadPorts()
	if err != nil {
		return 0, fmt.Errorf("expose: loading port state: %w", err)
	}

	// 3. Determine port range from config.
	lo, hi := 42000, 42999
	if e.Cfg.Spec.Ports.Range != "" {
		var parseErr error
		lo, hi, parseErr = proxy.ParseRange(e.Cfg.Spec.Ports.Range)
		if parseErr != nil {
			return 0, parseErr
		}
	}

	if remove {
		// Drop expose entry from manifest.
		newExpose := m.Spec.Expose[:0]
		for _, ex := range m.Spec.Expose {
			if ex.Service != service {
				newExpose = append(newExpose, ex)
			}
		}
		m.Spec.Expose = newExpose
		if err := store.SaveStack(m); err != nil {
			return 0, fmt.Errorf("expose --remove: saving manifest: %w", err)
		}
		// Free allocation.
		ps.Free(stack, service)
		// Targeted recreate if running.
		if err := e.exposeRecreate(ctx, m, stack, service); err != nil {
			fmt.Fprintf(e.Err, "kazi: warning: expose recreate failed: %v\n", err)
		}
		return 0, nil
	}

	// Upsert mode: auto-detect container port from compose config if needed.
	containerPort := port
	if containerPort == 0 {
		cp, detectErr := e.detectContainerPort(ctx, m, stack, service)
		if detectErr != nil {
			return 0, detectErr
		}
		containerPort = cp
	}

	// Upsert manifest expose entry.
	portStr := "auto"
	if port != 0 {
		portStr = strconv.Itoa(port)
	}
	found := false
	for i, ex := range m.Spec.Expose {
		if ex.Service == service {
			m.Spec.Expose[i].Port = portStr
			found = true
			break
		}
	}
	if !found {
		m.Spec.Expose = append(m.Spec.Expose, store.ExposeSpec{Service: service, Port: portStr})
	}
	if err := store.SaveStack(m); err != nil {
		return 0, fmt.Errorf("expose: saving manifest: %w", err)
	}

	// Allocate host port.
	pinned := 0
	if port != 0 {
		pinned = port
	}
	hostPort, err := ps.Allocate(stack, service, containerPort, pinned, lo, hi)
	if err != nil {
		return 0, fmt.Errorf("expose: allocating port for %q/%q: %w", stack, service, err)
	}

	// Targeted recreate if running.
	if err := e.exposeRecreate(ctx, m, stack, service); err != nil {
		fmt.Fprintf(e.Err, "kazi: warning: expose recreate failed: %v\n", err)
	}

	return hostPort, nil
}

// detectContainerPort returns the single container port exposed by the service,
// or an error if zero or more than one port is exposed.
func (e *Engine) detectContainerPort(ctx context.Context, m store.Manifest, stack, service string) (int, error) {
	composeFile := m.Spec.Source.Compose
	dir := filepath.Dir(composeFile)
	project := "kazi-" + stack
	files := []string{composeFile}
	jsonOut, err := compose.Output(e.RT.ComposeCmd(ctx, project, dir, files, "config", "--format", "json"))
	if err != nil {
		return 0, fmt.Errorf("expose: reading compose config for %q: %w", stack, err)
	}
	svcs, err := compose.ParseConfig([]byte(jsonOut))
	if err != nil {
		return 0, fmt.Errorf("expose: parsing compose config for %q: %w", stack, err)
	}
	for _, s := range svcs {
		if s.Name == service {
			if len(s.Ports) == 1 {
				return s.Ports[0], nil
			}
			return 0, fmt.Errorf("expose: service %q in stack %q exposes %d ports; pin one with an explicit port", service, stack, len(s.Ports))
		}
	}
	return 0, fmt.Errorf("expose: service %q not found in compose config for stack %q", service, stack)
}

// exposeRecreate runs `compose up -d --no-deps <svc>` with the rebuilt override
// if the stack is currently running. Errors are best-effort (not returned).
func (e *Engine) exposeRecreate(ctx context.Context, m store.Manifest, stack, service string) error {
	// Check if stack is running by looking at the snapshot.
	stacks, _, err := e.snapshot(ctx)
	if err != nil {
		return err
	}
	running := false
	for _, s := range stacks {
		if s.Name == stack && s.Running > 0 {
			running = true
			break
		}
	}
	if !running {
		return nil
	}

	composeFile := m.Spec.Source.Compose
	dir := filepath.Dir(composeFile)

	t := target{
		name:        stack,
		kind:        KindRegistered,
		dir:         dir,
		project:     "kazi-" + stack,
		composeFile: composeFile,
	}

	overridePath, _, buildErr := e.buildOverride(ctx, t, &m)
	if buildErr != nil {
		return buildErr
	}
	defer os.Remove(overridePath)

	files := []string{composeFile, overridePath}
	return compose.Run(
		e.RT.ComposeCmd(ctx, t.project, t.dir, files, "up", "-d", "--no-deps", service),
		e.Out, e.Err,
	)
}

// Urls returns endpoints for the named stack (or all stacks when stack=="").
// Each endpoint is one of:
//   - "http": https URL from the proxy routing plan
//   - "tcp":  localhost:<hostPort> if allocated or compose-published, else a nudge
//   - "hint": NeedsDecl advice with the ready-to-paste spec.proxy snippet
//
// Endpoints are sorted: stack → kind → service.
func (e *Engine) Urls(ctx context.Context, stack string) ([]Endpoint, error) {
	manifests, err := store.ListStacks()
	if err != nil {
		return nil, err
	}

	// Load port state once.
	ps, loadErr := proxy.LoadPorts()
	if loadErr != nil {
		return nil, fmt.Errorf("urls: loading port state: %w", loadErr)
	}

	var endpoints []Endpoint

	for _, m := range manifests {
		name := m.Metadata.Name
		if stack != "" && name != stack {
			continue
		}
		// Skip system stacks.
		if m.Spec.System {
			continue
		}

		// Non-compose sources: image/adopted stacks route from strategy data
		// (honoring a pinned spec.proxy.http_port); template stacks materialize a
		// compose file. Compose stacks fall through to the block below.
		switch m.Spec.Source.Kind() {
		case "image", "containers":
			t, rerr := e.resolve(ctx, name)
			if rerr != nil {
				continue
			}
			for _, r := range e.desiredRoutes(ctx, t) {
				endpoints = append(endpoints, Endpoint{
					Stack: name, Service: r.Service, Kind: "http",
					URL:    "https://" + r.Hostname,
					Target: fmt.Sprintf("%s:%d", r.Service, r.Port),
				})
			}
			endpoints = append(endpoints, imagePublishedEndpoints(name, m)...)
			continue
		case "template":
			t, rerr := e.resolve(ctx, name)
			if rerr != nil || t.composeFile == "" {
				continue
			}
			jsonOut, configErr := compose.Output(e.composeCmdFor(ctx, t, "config", "--format", "json"))
			if configErr != nil {
				fmt.Fprintf(e.Err, "kazi: warning: reading compose config for %s: %v\n", name, configErr)
				continue
			}
			svcs, parseErr := compose.ParseConfig([]byte(jsonOut))
			if parseErr != nil {
				fmt.Fprintf(e.Err, "kazi: warning: reading compose config for %s: %v\n", name, parseErr)
				continue
			}
			endpoints = append(endpoints, e.planEndpoints(name, m.Spec.Proxy, svcs, ps)...)
			continue
		}

		composeFile := m.Spec.Source.Compose
		if composeFile == "" {
			continue
		}
		dir := filepath.Dir(composeFile)
		project := "kazi-" + name
		jsonOut, configErr := compose.Output(e.RT.ComposeCmd(ctx, project, dir, []string{composeFile}, "config", "--format", "json"))
		if configErr != nil {
			fmt.Fprintf(e.Err, "kazi: warning: reading compose config for %s: %v\n", name, configErr)
			continue
		}
		svcs, parseErr := compose.ParseConfig([]byte(jsonOut))
		if parseErr != nil {
			fmt.Fprintf(e.Err, "kazi: warning: reading compose config for %s: %v\n", name, parseErr)
			continue
		}

		endpoints = append(endpoints, e.planEndpoints(name, m.Spec.Proxy, svcs, ps)...)
	}

	// Static routes (kazi route): scoped to the stack, or all when stack == "".
	// These cover externally-run/discovered stacks kazi can't reverse-proxy
	// directly but exposes via host.docker.internal.
	endpoints = append(endpoints, e.staticRouteEndpoints(stack)...)

	// Sort: stack → kind (http < hint < tcp) → service.
	sortEndpoints(endpoints)
	return endpoints, nil
}

// planEndpoints turns a compose-derived routing plan into HTTP endpoints, a
// needs-declaration hint, and TCP endpoints (published, kazi-allocated, or a
// nudge). Shared by the compose and template urls paths.
func (e *Engine) planEndpoints(name string, decl *store.ProxySpec, svcs []compose.ServiceInfo, ps *proxy.PortState) []Endpoint {
	plan := proxy.BuildPlan(name, decl, svcs, e.Cfg.Spec.Proxy.HTTPPorts, e.Cfg.Spec.Proxy.TCPPorts)
	var endpoints []Endpoint

	for _, r := range plan.Routes {
		endpoints = append(endpoints, Endpoint{
			Stack: name, Service: r.Service, Kind: "http",
			URL:    "https://" + r.Hostname,
			Target: fmt.Sprintf("%s:%d", r.Service, r.Port),
		})
	}

	if plan.NeedsDecl {
		snippet := "spec:\n  proxy:\n    service: <primary-service>  # run `kazi urls` after adding\n"
		endpoints = append(endpoints, Endpoint{
			Stack: name, Service: "", Kind: "hint", Target: name,
			Note: fmt.Sprintf("multiple HTTP services detected; add to your manifest:\n%s", snippet),
		})
	}

	svcMap := map[string]compose.ServiceInfo{}
	for _, s := range svcs {
		svcMap[s.Name] = s
	}
	for _, tcp := range plan.TCP {
		si := svcMap[tcp.Service]
		switch {
		case si.Published[tcp.Port] != 0:
			endpoints = append(endpoints, Endpoint{
				Stack: name, Service: tcp.Service, Kind: "tcp",
				URL:    fmt.Sprintf("localhost:%d", si.Published[tcp.Port]),
				Target: fmt.Sprintf("%s:%d", tcp.Service, tcp.Port),
			})
		case hasAlloc(ps, name, tcp.Service):
			alloc, _ := ps.Lookup(name, tcp.Service)
			endpoints = append(endpoints, Endpoint{
				Stack: name, Service: tcp.Service, Kind: "tcp",
				URL:    fmt.Sprintf("localhost:%d", alloc.HostPort),
				Target: fmt.Sprintf("%s:%d", tcp.Service, tcp.Port),
			})
		default:
			endpoints = append(endpoints, Endpoint{
				Stack: name, Service: tcp.Service, Kind: "tcp",
				Target: fmt.Sprintf("%s:%d", tcp.Service, tcp.Port),
				Note:   fmt.Sprintf("not reachable from host — run: kazi expose %s %s", name, tcp.Service),
			})
		}
	}
	return endpoints
}

// hasAlloc reports whether a kazi port allocation exists for stack/service.
func hasAlloc(ps *proxy.PortState, stack, service string) bool {
	_, ok := ps.Lookup(stack, service)
	return ok
}

// imagePublishedEndpoints reports the host-published ports of an image stack
// (spec.expose "host:container" | "port") as TCP endpoints.
func imagePublishedEndpoints(name string, m store.Manifest) []Endpoint {
	var out []Endpoint
	for _, exp := range m.Spec.Expose {
		if exp.Port == "" || exp.Port == "auto" {
			continue
		}
		host, container := exp.Port, exp.Port
		if h, c, ok := strings.Cut(exp.Port, ":"); ok {
			host, container = h, c
		}
		out = append(out, Endpoint{
			Stack: name, Service: exp.Service, Kind: "tcp",
			URL:    "localhost:" + host,
			Target: exp.Service + ":" + container,
		})
	}
	return out
}

// sortEndpoints orders endpoints: stack → kind (http < hint < tcp) → service.
func sortEndpoints(endpoints []Endpoint) {
	kindOrder := map[string]int{"http": 0, "hint": 1, "tcp": 2}
	sort.Slice(endpoints, func(i, j int) bool {
		a, b := endpoints[i], endpoints[j]
		if a.Stack != b.Stack {
			return a.Stack < b.Stack
		}
		ka, kb := kindOrder[a.Kind], kindOrder[b.Kind]
		if ka != kb {
			return ka < kb
		}
		return a.Service < b.Service
	})
}

// Trust extracts the root CA from the kazi-proxy container and installs it
// into the system trust store.
//
// Cert extraction: `<rt> exec kazi-proxy cat /data/caddy/pki/authorities/local/root.crt`
//
// Darwin install:
//
//	sudo security add-trusted-cert -d -r trustRoot -k /Library/Keychains/System.keychain <tmpfile>
//
// Darwin uninstall:
//
//	sudo security delete-certificate -c "Caddy Local Authority" /Library/Keychains/System.keychain
//
// Linux install:
//
//	sudo cp <tmpfile> /usr/local/share/ca-certificates/kazi-local-ca.crt
//	sudo update-ca-certificates
//
// Linux uninstall:
//
//	sudo rm /usr/local/share/ca-certificates/kazi-local-ca.crt
//	sudo update-ca-certificates --fresh
//
// The GOOS switch uses the package-level osOverride var (empty = use runtime.GOOS)
// to allow tests to exercise either branch without building for another OS.
//
// The sudo command is constructed via the sudoRun seam (a func var) so tests can
// stub it out without a real sudo prompt being issued.
func (e *Engine) Trust(ctx context.Context, uninstall bool) error {
	os_ := goos()

	if uninstall {
		return e.trustUninstall(ctx, os_)
	}

	// Extract cert.
	certBytes, err := compose.Output(e.RT.Cmd(ctx,
		"exec", proxy.ContainerName, "cat",
		"/data/caddy/pki/authorities/local/root.crt",
	))
	if err != nil {
		return fmt.Errorf("trust: extracting root CA from proxy: %w", err)
	}

	// Write to temp file.
	tmp, err := os.CreateTemp("", "kazi-root-ca-*.crt")
	if err != nil {
		return fmt.Errorf("trust: creating temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := tmp.WriteString(certBytes); err != nil {
		tmp.Close()
		return fmt.Errorf("trust: writing cert to temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("trust: closing temp file: %w", err)
	}

	// Install into system trust store.
	switch os_ {
	case "darwin":
		args := sudoRun([]string{
			"security", "add-trusted-cert",
			"-d", "-r", "trustRoot",
			"-k", "/Library/Keychains/System.keychain",
			tmpPath,
		})
		if err := compose.Run(hostCmd(ctx, args), e.Out, e.Err); err != nil {
			return fmt.Errorf("trust: installing cert on darwin: %w", err)
		}
	case "linux":
		dest := "/usr/local/share/ca-certificates/kazi-local-ca.crt"
		cpArgs := sudoRun([]string{"cp", tmpPath, dest})
		if err := compose.Run(hostCmd(ctx, cpArgs), e.Out, e.Err); err != nil {
			return fmt.Errorf("trust: copying cert on linux: %w", err)
		}
		updateArgs := sudoRun([]string{"update-ca-certificates"})
		if err := compose.Run(hostCmd(ctx, updateArgs), e.Out, e.Err); err != nil {
			return fmt.Errorf("trust: updating ca-certificates on linux: %w", err)
		}
	default:
		return fmt.Errorf("trust: unsupported OS %q; manually install %s into your system trust store", os_, tmpPath)
	}
	return nil
}

func (e *Engine) trustUninstall(ctx context.Context, os_ string) error {
	switch os_ {
	case "darwin":
		args := sudoRun([]string{
			"security", "delete-certificate",
			"-c", "Caddy Local Authority",
			"/Library/Keychains/System.keychain",
		})
		if err := compose.Run(hostCmd(ctx, args), e.Out, e.Err); err != nil {
			return fmt.Errorf("trust --uninstall: removing cert on darwin: %w", err)
		}
	case "linux":
		dest := "/usr/local/share/ca-certificates/kazi-local-ca.crt"
		rmArgs := sudoRun([]string{"rm", dest})
		if err := compose.Run(hostCmd(ctx, rmArgs), e.Out, e.Err); err != nil {
			return fmt.Errorf("trust --uninstall: removing cert on linux: %w", err)
		}
		updateArgs := sudoRun([]string{"update-ca-certificates", "--fresh"})
		if err := compose.Run(hostCmd(ctx, updateArgs), e.Out, e.Err); err != nil {
			return fmt.Errorf("trust --uninstall: updating ca-certificates on linux: %w", err)
		}
	default:
		return fmt.Errorf("trust --uninstall: unsupported OS %q", os_)
	}
	return nil
}
