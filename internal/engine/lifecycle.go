package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/thapakazi/kazi/internal/compose"
	"github.com/thapakazi/kazi/internal/labels"
	"github.com/thapakazi/kazi/internal/proxy"
	"github.com/thapakazi/kazi/internal/store"
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

// Up brings a stack up detached. For registered stacks kazi injects its
// labels through a generated override file (pure compose spec, portable
// across runtimes). compose up -d is already idempotent — an
// already-running stack exits 0.
func (e *Engine) Up(ctx context.Context, name string) error {
	t, err := e.resolve(ctx, name)
	if err != nil {
		return err
	}

	// For discovered stacks, we need to find the compose file now
	if t.kind == KindDiscovered && t.composeFile == "" {
		found, ferr := findComposeFile(t.dir)
		if ferr != nil {
			return ferr
		}
		t.composeFile = found
	}

	var overridePath string
	var plan proxy.Plan

	if t.kind == KindRegistered || t.kind == KindDiscovered {
		var buildErr error
		var mPtr *store.Manifest
		if lm, err := store.LoadStack(name); err == nil {
			mPtr = &lm
		}
		overridePath, plan, buildErr = e.buildOverride(ctx, t, mPtr)
		if buildErr != nil {
			return buildErr
		}
		defer os.Remove(overridePath)
	}

	// Ensure the kazi network exists before compose up when any service is routable
	if len(plan.Routable) > 0 {
		if err := proxy.EnsureNetwork(ctx, e.RT); err != nil {
			return err
		}
	}

	var files []string
	if t.composeFile != "" {
		files = []string{t.composeFile}
	}
	if overridePath != "" {
		files = append(files, overridePath)
	}

	if err := e.frame(compose.Run(e.RT.ComposeCmd(ctx, t.project, t.dir, files, "up", "-d"), e.Out, e.Err), "up", name); err != nil {
		return err
	}

	// syncProxy with the extra plan we just computed so the route appears
	// even when no containers are yet visible in the snapshot
	e.syncProxy(ctx, "", name, &plan)
	return nil
}

// buildOverride parses compose config --format json, builds the routing plan,
// allocates ports for expose entries, then renders the override file.
// Caller is responsible for removing the temp file.
func (e *Engine) buildOverride(ctx context.Context, t target, m *store.Manifest) (path string, plan proxy.Plan, err error) {
	jsonOut, err := compose.Output(e.RT.ComposeCmd(ctx, t.project, t.dir, []string{t.composeFile}, "config", "--format", "json"))
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
	var overrideSvcs []proxy.OverrideService
	for _, s := range svcs {
		routable := plan.Routable[s.Name]
		os_ := proxy.OverrideService{
			Name:     s.Name,
			Routable: routable,
			Alias:    s.Name + "." + t.name,
			Networks: s.Networks,
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

// Down stops and removes a stack's containers. Never passes -v in M0.
func (e *Engine) Down(ctx context.Context, name string) error {
	t, err := e.resolve(ctx, name)
	if err != nil {
		return err
	}
	if err := e.frame(compose.Run(e.RT.ComposeCmd(ctx, t.project, t.dir, t.files(), "down"), e.Out, e.Err), "down", name); err != nil {
		return err
	}
	// exclude this stack from the desired routes (it just went down)
	e.syncProxy(ctx, name, "", nil)
	return nil
}

func (e *Engine) Restart(ctx context.Context, name string) error {
	t, err := e.resolve(ctx, name)
	if err != nil {
		return err
	}
	if err := e.frame(compose.Run(e.RT.ComposeCmd(ctx, t.project, t.dir, t.files(), "restart"), e.Out, e.Err), "restart", name); err != nil {
		return err
	}
	e.syncProxy(ctx, "", "", nil)
	return nil
}

// Logs streams compose logs; service may be empty for all services.
func (e *Engine) Logs(ctx context.Context, name, service string, follow bool, tail string) error {
	t, err := e.resolve(ctx, name)
	if err != nil {
		return err
	}
	args := []string{"logs"}
	if follow {
		args = append(args, "-f")
	}
	if tail != "" {
		args = append(args, "--tail", tail)
	}
	if service != "" {
		args = append(args, service)
	}
	return e.frame(compose.Run(e.RT.ComposeCmd(ctx, t.project, t.dir, t.files(), args...), e.Out, e.Err), "logs", name)
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
		var decl *store.ProxySpec
		if m, ok := manifestByName[meta.name]; ok {
			decl = m.Spec.Proxy
		}

		// For registered stacks: get compose file from manifest.
		// For discovered stacks: find compose file in dir.
		composeFile := ""
		if m, ok := manifestByName[meta.name]; ok && m.Spec.Source.Compose != "" {
			composeFile = m.Spec.Source.Compose
		} else if meta.dir != "" {
			// best-effort: try to find compose file
			if found, ferr := findComposeFile(meta.dir); ferr == nil {
				composeFile = found
			}
		}

		if composeFile == "" {
			continue
		}

		project := "kazi-" + meta.name
		jsonOut, configErr := compose.Output(e.RT.ComposeCmd(ctx, project, meta.dir, []string{composeFile}, "config", "--format", "json"))
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
