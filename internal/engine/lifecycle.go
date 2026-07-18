package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/thapakazi/kazi/internal/compose"
	"github.com/thapakazi/kazi/internal/labels"
	"github.com/thapakazi/kazi/internal/proxy"
	"github.com/thapakazi/kazi/internal/runtime"
	"github.com/thapakazi/kazi/internal/store"
	"github.com/thapakazi/kazi/internal/template"
)

// findComposeFile returns the absolute path to the compose file in dir,
// searching in the standard name order. Extracted so both Add and Up can use it.
func findComposeFile(dir string) (string, error) {
	for _, n := range composeNames {
		candidate := filepath.Join(dir, n)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("no compose file (compose.y(a)ml or docker-compose.y(a)ml) found in %s", dir)
}

// Up brings a stack up detached, dispatching per source kind. For compose /
// template stacks kazi injects its labels through a generated override file
// (pure compose spec, portable across runtimes); compose up -d is idempotent.
// Image stacks create-or-start their container; adopted-container stacks start
// their named containers.
func (e *Engine) Up(ctx context.Context, name string) error {
	t, err := e.resolve(ctx, name)
	if err != nil {
		return err
	}
	// Discovered compose stacks must locate their compose file lazily.
	if t.kind == KindDiscovered && t.composeFile == "" {
		found, ferr := findComposeFile(t.dir)
		if ferr != nil {
			return ferr
		}
		t.composeFile = found
	}
	return strategyFor(t.srcKind).up(ctx, e, t)
}

// buildOverride parses compose config --format json, builds the routing plan,
// allocates ports for expose entries, then renders the override file.
// Caller is responsible for removing the temp file.
func (e *Engine) buildOverride(ctx context.Context, t target, m *store.Manifest) (path string, plan proxy.Plan, err error) {
	jsonOut, err := compose.Output(e.composeCmdFor(ctx, t, "config", "--format", "json"))
	if err != nil {
		return "", proxy.Plan{}, e.frame(err, "config", t.name)
	}

	svcs, err := compose.ParseConfig([]byte(jsonOut))
	if err != nil {
		return "", proxy.Plan{}, fmt.Errorf("parsing compose config for %q: %w", t.name, err)
	}

	// Determine proxy declaration from manifest (nil for discovered)
	var decl *store.ProxySpec
	if m != nil {
		decl = m.Spec.Proxy
	}

	plan = proxy.BuildPlan(t.name, decl, svcs, e.Cfg.Spec.Proxy.HTTPPorts, e.Cfg.Spec.Proxy.TCPPorts)

	// Build the OverrideService list
	svcMap := map[string]compose.ServiceInfo{}
	for _, s := range svcs {
		svcMap[s.Name] = s
	}

	// Handle port allocations for expose specs
	lo, hi := 42000, 42999
	if m != nil && e.Cfg.Spec.Ports.Range != "" {
		var parseErr error
		lo, hi, parseErr = proxy.ParseRange(e.Cfg.Spec.Ports.Range)
		if parseErr != nil {
			return "", proxy.Plan{}, parseErr
		}
	}

	var portBindings map[string][]string
	if m != nil {
		ps, loadErr := proxy.LoadPorts()
		if loadErr != nil {
			return "", proxy.Plan{}, fmt.Errorf("loading port state: %w", loadErr)
		}

		if len(m.Spec.Expose) > 0 {
			portBindings = map[string][]string{}

			for _, exp := range m.Spec.Expose {
				si, ok := svcMap[exp.Service]
				if !ok {
					return "", proxy.Plan{}, fmt.Errorf("service %q in expose spec not found in compose config", exp.Service)
				}

				containerPort := 0
				if exp.Port != "auto" {
					// parse fixed port
					var n int
					if _, scanErr := fmt.Sscanf(exp.Port, "%d", &n); scanErr != nil || n <= 0 {
						return "", proxy.Plan{}, fmt.Errorf("invalid expose port %q for service %q", exp.Port, exp.Service)
					}
					containerPort = n
				} else {
					// auto-detect: service must expose exactly one port
					if len(si.Ports) != 1 {
						return "", proxy.Plan{}, fmt.Errorf("service %q exposes %d ports; pin one with spec.expose port", exp.Service, len(si.Ports))
					}
					containerPort = si.Ports[0]
				}

				pinned := 0
				hostPort, allocErr := ps.Allocate(t.name, exp.Service, containerPort, pinned, lo, hi)
				if allocErr != nil {
					return "", proxy.Plan{}, fmt.Errorf("allocating port for %q/%q: %w", t.name, exp.Service, allocErr)
				}
				portBindings[exp.Service] = append(portBindings[exp.Service], fmt.Sprintf("%d:%d", hostPort, containerPort))
			}
		}

		// Reconcile: free allocations for services that no longer appear in
		// spec.expose (user hand-edited the manifest).  Only for registered
		// stacks (m != nil); discovered stacks have nil manifests and are left
		// untouched.
		exposedServices := map[string]bool{}
		for _, exp := range m.Spec.Expose {
			exposedServices[exp.Service] = true
		}
		changed := false
		for _, alloc := range ps.Services(t.name) {
			if !exposedServices[alloc.Service] {
				ps.Free(t.name, alloc.Service)
				changed = true
			}
		}
		if changed {
			_ = ps.Save()
		}
	}

	// Build OverrideService list
	ephemeral := m != nil && m.Spec.Ephemeral
	var overrideSvcs []proxy.OverrideService
	for _, s := range svcs {
		routable := plan.Routable[s.Name]
		os_ := proxy.OverrideService{
			Name:      s.Name,
			Routable:  routable,
			Alias:     s.Name + "." + t.name,
			Networks:  s.Networks,
			Ephemeral: ephemeral,
		}
		if portBindings != nil {
			os_.Ports = portBindings[s.Name]
		}
		overrideSvcs = append(overrideSvcs, os_)
	}

	overrideBytes := proxy.RenderOverride(t.name, overrideSvcs)

	f, createErr := os.CreateTemp("", "kazi-override-*.yml")
	if createErr != nil {
		return "", proxy.Plan{}, createErr
	}
	if _, writeErr := f.Write(overrideBytes); writeErr != nil {
		f.Close()
		os.Remove(f.Name())
		return "", proxy.Plan{}, writeErr
	}
	if closeErr := f.Close(); closeErr != nil {
		os.Remove(f.Name())
		return "", proxy.Plan{}, closeErr
	}

	return f.Name(), plan, nil
}

// Down stops a stack, dispatching per source. Compose stacks run
// `compose down` (never -v here); image stacks stop their container; adopted
// stacks stop their named containers. Public Down passes no extra args.
func (e *Engine) Down(ctx context.Context, name string) error {
	t, err := e.resolve(ctx, name)
	if err != nil {
		return err
	}
	return strategyFor(t.srcKind).down(ctx, e, t)
}

// Restart restarts a stack per source.
func (e *Engine) Restart(ctx context.Context, name string) error {
	t, err := e.resolve(ctx, name)
	if err != nil {
		return err
	}
	return strategyFor(t.srcKind).restart(ctx, e, t)
}

// Logs streams a stack's logs per source; service may be empty for all
// services (compose) or to pick the first container (adopted).
func (e *Engine) Logs(ctx context.Context, name, service string, follow bool, tail string) error {
	t, err := e.resolve(ctx, name)
	if err != nil {
		return err
	}
	return strategyFor(t.srcKind).logs(ctx, e, t, service, follow, tail)
}

// desiredRoutes builds the reverse-proxy routes for a non-compose stack from
// strategy data (no `compose config`). Compose/template stacks return nil —
// their routes come from BuildPlan over the parsed config.
//
//   - image: alias <stack>.<stack>, host <stack>.localhost, port from the
//     image's exposed-port classification (nil if not HTTP-routable).
//   - containers: one route per adopted container that classifies HTTP; a
//     single HTTP container gets the bare <stack>.localhost, otherwise
//     <container>.<stack>.localhost.
func (e *Engine) desiredRoutes(ctx context.Context, t target) []proxy.Route {
	switch t.srcKind {
	case "image":
		port, ok := e.imageRoute(ctx, t)
		if !ok {
			return nil
		}
		return []proxy.Route{{
			Stack: t.name, Service: t.name,
			Hostname: t.name + ".localhost",
			Alias:    t.name + "." + t.name,
			Port:     port,
		}}
	case "containers":
		cs, err := e.RT.Ps(ctx)
		if err != nil {
			return nil
		}
		byName := map[string]runtime.Container{}
		for _, c := range cs {
			byName[c.Name] = c
		}
		type httpC struct {
			name string
			port int
		}
		var httpCs []httpC
		for _, name := range t.containers {
			c, ok := byName[name]
			if !ok {
				continue
			}
			port, isHTTP := classifyPorts(parsePsPorts(c.Ports), e.Cfg)
			if isHTTP {
				httpCs = append(httpCs, httpC{name: name, port: port})
			}
		}
		var routes []proxy.Route
		for _, h := range httpCs {
			host := h.name + "." + t.name + ".localhost"
			if len(httpCs) == 1 {
				host = t.name + ".localhost"
			}
			routes = append(routes, proxy.Route{
				Stack: t.name, Service: h.name,
				Hostname: host,
				Alias:    h.name + "." + t.name,
				Port:     h.port,
			})
		}
		return routes
	default:
		return nil
	}
}

// files returns the -f list for lifecycle verbs: the manifest's compose
// file for registered stacks, nothing for discovered (compose finds the
// file in the project directory).
func (t target) files() []string {
	if t.composeFile != "" {
		return []string{t.composeFile}
	}
	return nil
}

// frame wraps a compose failure with one trailing line of context (what
// failed, which stack, which runtime) without swallowing streamed output.
func (e *Engine) frame(err error, verb, stack string) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("`%s compose %s` failed for stack %q: %w", e.RT.Name(), verb, stack, err)
}

// syncProxy recomputes desired routes across ALL stacks and drives the
// Caddyfile generate→validate→reload loop. It NEVER returns an error:
// failures are written as warnings to e.Err.
//
// exclude: a stack name to skip (e.g. the stack just downed).
// extraStack / extraPlan: an extra plan to merge in (e.g. the stack just
// upped, which may not yet have running containers in the snapshot).
func (e *Engine) syncProxy(ctx context.Context, exclude string, extraStack string, extraPlan *proxy.Plan) {
	err := e.doSyncProxy(ctx, exclude, extraStack, extraPlan)
	if err != nil {
		fmt.Fprintf(e.Err, "kazi: warning: proxy sync failed: %v\n", err)
	}
}

func (e *Engine) doSyncProxy(ctx context.Context, exclude string, extraStack string, extraPlan *proxy.Plan) error {
	// Ensure system manifest is registered.
	if _, loadErr := store.LoadStack(proxy.StackName); errors.Is(loadErr, store.ErrNotFound) {
		if saveErr := store.SaveStack(proxy.SystemManifest()); saveErr != nil {
			return fmt.Errorf("registering system manifest: %w", saveErr)
		}
	}

	// Take a snapshot of running containers.
	cs, err := e.RT.Ps(ctx)
	if err != nil {
		return fmt.Errorf("listing containers: %w", err)
	}

	// Collect stacks with running kazi.managed containers (keyed by compose project name).
	// We need to map project names → stack names and dirs.
	manifests, _ := store.ListStacks()
	manifestByName := map[string]store.Manifest{}
	for _, m := range manifests {
		manifestByName[m.Metadata.Name] = m
	}

	// Build set of stacks that have ≥1 running kazi.managed container.
	type stackMeta struct {
		name string
		dir  string
	}
	activeStacks := map[string]stackMeta{} // stack-name -> meta

	for _, c := range cs {
		if c.Labels[labels.Managed] != "true" {
			continue
		}
		if c.State != "running" {
			continue
		}
		stackName := c.Labels[labels.Stack]
		if stackName == "" {
			stackName = c.Labels[labels.ComposeProject]
		}
		if stackName == "" || stackName == proxy.StackName || stackName == exclude {
			continue
		}
		dir := c.Labels[labels.ComposeWorkingDir]
		activeStacks[stackName] = stackMeta{name: stackName, dir: dir}
	}

	// Determine if proxy is running.
	proxyRunning := false
	for _, c := range cs {
		if c.Name == proxy.ContainerName && c.State == "running" {
			proxyRunning = true
			break
		}
	}

	// Collect routes from each active stack.
	var allRoutes []proxy.Route

	for _, meta := range activeStacks {
		if meta.name == proxy.StackName {
			continue
		}
		m, hasManifest := manifestByName[meta.name]

		// Non-compose registered stacks route from strategy data, not from
		// `compose config`. Build a minimal target and reuse desiredRoutes.
		if hasManifest {
			switch m.Spec.Source.Kind() {
			case "image":
				t := target{name: meta.name, srcKind: "image", image: m.Spec.Source.Image, manifest: &m}
				allRoutes = append(allRoutes, e.desiredRoutes(ctx, t)...)
				continue
			case "containers":
				t := target{name: meta.name, srcKind: "containers", containers: m.Spec.Source.Containers, manifest: &m}
				allRoutes = append(allRoutes, e.desiredRoutes(ctx, t)...)
				continue
			}
		}

		var decl *store.ProxySpec
		if hasManifest {
			decl = m.Spec.Proxy
		}

		// Compose stacks: locate the compose file (manifest first, else dir).
		// Template stacks materialize on resolve; their manifest has no compose
		// path, so re-resolve via the template to get dir + env.
		composeFile := ""
		dir := meta.dir
		project := "kazi-" + meta.name
		var envCmd func(args ...string) *exec.Cmd
		if hasManifest && m.Spec.Source.Compose != "" {
			composeFile = m.Spec.Source.Compose
			dir = filepath.Dir(composeFile)
		} else if hasManifest && m.Spec.Source.Template != "" {
			tdir, matErr := template.Materialize(m.Spec.Source.Template)
			if matErr != nil {
				continue
			}
			cf, cfErr := findComposeFile(tdir)
			if cfErr != nil {
				continue
			}
			composeFile, dir = cf, tdir
			mCopy := m
			t := target{name: meta.name, srcKind: "template", dir: tdir, composeFile: cf, project: project, manifest: &mCopy}
			envCmd = func(args ...string) *exec.Cmd { return e.composeCmdFor(ctx, t, args...) }
		} else if meta.dir != "" {
			if found, ferr := findComposeFile(meta.dir); ferr == nil {
				composeFile = found
			}
		}

		if composeFile == "" {
			continue
		}

		var cmd *exec.Cmd
		if envCmd != nil {
			cmd = envCmd("config", "--format", "json")
		} else {
			cmd = e.RT.ComposeCmd(ctx, project, dir, []string{composeFile}, "config", "--format", "json")
		}
		jsonOut, configErr := compose.Output(cmd)
		if configErr != nil {
			// best effort: skip this stack
			continue
		}
		svcs, parseErr := compose.ParseConfig([]byte(jsonOut))
		if parseErr != nil {
			continue
		}

		p := proxy.BuildPlan(meta.name, decl, svcs, e.Cfg.Spec.Proxy.HTTPPorts, e.Cfg.Spec.Proxy.TCPPorts)
		allRoutes = append(allRoutes, p.Routes...)
	}

	// Merge in the extra plan (the stack being up'ed).
	if extraStack != "" && extraPlan != nil && extraStack != exclude {
		// Deduplicate: remove any existing routes for extraStack.
		var merged []proxy.Route
		for _, r := range allRoutes {
			if r.Stack != extraStack {
				merged = append(merged, r)
			}
		}
		merged = append(merged, extraPlan.Routes...)
		allRoutes = merged
	}

	// Ensure proxy stack files exist before calling Sync.
	if err := proxy.EnsureStackFiles(); err != nil {
		return fmt.Errorf("ensuring proxy stack files: %w", err)
	}

	return proxy.Sync(ctx, e.RT, allRoutes, proxyRunning)
}
