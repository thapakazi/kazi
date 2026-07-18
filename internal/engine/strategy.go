package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"

	"github.com/thapakazi/kazi/internal/compose"
	"github.com/thapakazi/kazi/internal/labels"
	"github.com/thapakazi/kazi/internal/proxy"
	"github.com/thapakazi/kazi/internal/store"
	"github.com/thapakazi/kazi/internal/template"
)

// strategy encapsulates the per-source lifecycle behaviour. compose and
// template stacks share composeStrategy; image and containers stacks have
// their own. down carries extraArgs so teardown (T5) can pass
// `-v --rmi local`; the public Down passes none.
type strategy interface {
	up(ctx context.Context, e *Engine, t target) error
	down(ctx context.Context, e *Engine, t target, extraArgs ...string) error
	restart(ctx context.Context, e *Engine, t target) error
	logs(ctx context.Context, e *Engine, t target, service string, follow bool, tail string) error
}

// strategyFor maps a source kind to its lifecycle strategy. Unknown/empty
// kinds fall back to compose (discovered stacks are always compose).
func strategyFor(kind string) strategy {
	switch kind {
	case "image":
		return imageStrategy{}
	case "containers":
		return containersStrategy{}
	default: // "compose", "template", ""
		return composeStrategy{}
	}
}

// composeEnv returns the extra env to append to every compose invocation of a
// template stack: template LoadValues defaults ← manifest Spec.Values, flattened
// via FlattenEnv. Compose and discovered stacks get no extra env (nil).
func (e *Engine) composeEnv(t target) []string {
	if t.srcKind != "template" || t.manifest == nil {
		return nil
	}
	_, defaults, err := template.LoadValues(t.dir)
	if err != nil {
		defaults = map[string]string{}
	}
	merged := make(map[string]string, len(defaults))
	for k, v := range defaults {
		merged[k] = v
	}
	for k, v := range t.manifest.Spec.Values {
		merged[strings.ToLower(k)] = v
	}
	return template.FlattenEnv(merged)
}

// composeCmdFor builds a compose command for t, appending the template env
// (defaults ← manifest values) to cmd.Env for template stacks. Used by every
// compose-strategy verb so interpolation is consistent across up/down/logs/etc.
func (e *Engine) composeCmdFor(ctx context.Context, t target, args ...string) *exec.Cmd {
	cmd := e.RT.ComposeCmd(ctx, t.project, t.dir, t.files(), args...)
	if env := e.composeEnv(t); len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	return cmd
}

// --- composeStrategy: compose & template stacks -----------------------------

type composeStrategy struct{}

func (composeStrategy) up(ctx context.Context, e *Engine, t target) error {
	overridePath, plan, buildErr := e.buildOverride(ctx, t, t.manifest)
	if buildErr != nil {
		return buildErr
	}
	defer os.Remove(overridePath)

	if len(plan.Routable) > 0 {
		if err := proxy.EnsureNetwork(ctx, e.RT); err != nil {
			return err
		}
	}

	// The generated override lives outside the manifest, so build the -f list
	// explicitly (composeCmdFor uses t.files(), which excludes it). Template env
	// is still appended below so ${VAR:-default} interpolation resolves.
	var files []string
	if t.composeFile != "" {
		files = []string{t.composeFile}
	}
	if overridePath != "" {
		files = append(files, overridePath)
	}
	cmd := e.RT.ComposeCmd(ctx, t.project, t.dir, files, "up", "-d")
	if env := e.composeEnv(t); len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	if err := e.frame(compose.Run(cmd, e.Out, e.Err), "up", t.name); err != nil {
		return err
	}
	// syncProxy with the plan we just computed so the route appears even when no
	// containers are yet visible in the snapshot.
	e.syncProxy(ctx, "", t.name, &plan)
	return nil
}

func (composeStrategy) down(ctx context.Context, e *Engine, t target, extraArgs ...string) error {
	args := append([]string{"down"}, extraArgs...)
	if err := e.frame(compose.Run(e.composeCmdFor(ctx, t, args...), e.Out, e.Err), "down", t.name); err != nil {
		return err
	}
	e.syncProxy(ctx, t.name, "", nil)
	return nil
}

func (composeStrategy) restart(ctx context.Context, e *Engine, t target) error {
	if err := e.frame(compose.Run(e.composeCmdFor(ctx, t, "restart"), e.Out, e.Err), "restart", t.name); err != nil {
		return err
	}
	e.syncProxy(ctx, "", "", nil)
	return nil
}

func (composeStrategy) logs(ctx context.Context, e *Engine, t target, service string, follow bool, tail string) error {
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
	return e.frame(compose.Run(e.composeCmdFor(ctx, t, args...), e.Out, e.Err), "logs", t.name)
}

// --- imageStrategy: `kazi run` image-backed single-container stacks ----------

type imageStrategy struct{}

// containerName is the deterministic container name for an image stack.
func imageContainerName(stack string) string { return "kazi-" + stack }

func (imageStrategy) up(ctx context.Context, e *Engine, t target) error {
	cname := imageContainerName(t.name)

	// If the container already exists (running or stopped) → start it.
	cs, err := e.RT.Ps(ctx)
	if err != nil {
		return err
	}
	for _, c := range cs {
		if c.Name == cname {
			if err := e.frameCmd(compose.Run(e.RT.Cmd(ctx, "start", cname), e.Out, e.Err), "start", t.name); err != nil {
				return err
			}
			e.syncProxyForImage(ctx, t)
			return nil
		}
	}

	// Fresh: classify from the image's exposed ports.
	_, routable := e.imageRoute(ctx, t)
	if routable {
		if err := proxy.EnsureNetwork(ctx, e.RT); err != nil {
			return err
		}
	}

	args := []string{"run", "-d", "--name", cname,
		"--label", labels.Managed + "=true",
		"--label", labels.Stack + "=" + t.name,
	}
	if t.manifest != nil && t.manifest.Spec.Ephemeral {
		args = append(args, "--label", labels.Ephemeral+"=true")
	}
	if routable {
		args = append(args, "--network", proxy.NetworkName,
			"--network-alias", t.name+"."+t.name)
	}
	// -e from Spec.Values (upper-cased keys, sorted for determinism).
	if t.manifest != nil {
		for _, kv := range sortedEnv(t.manifest.Spec.Values) {
			args = append(args, "-e", kv)
		}
		// -p from Spec.Expose pinned entries.
		// Port field stores the full mapping ("8080:80") or bare port ("8080").
		// Bare port means hostPort:containerPort are both the same number.
		for _, exp := range t.manifest.Spec.Expose {
			if exp.Port != "" && exp.Port != "auto" {
				if strings.Contains(exp.Port, ":") {
					args = append(args, "-p", exp.Port)
				} else {
					args = append(args, "-p", exp.Port+":"+exp.Port)
				}
			}
		}
		// -v from Spec.Volumes.
		for _, v := range t.manifest.Spec.Volumes {
			args = append(args, "-v", v)
		}
	}
	args = append(args, t.image)

	if err := e.frameCmd(compose.Run(e.RT.Cmd(ctx, args...), e.Out, e.Err), "run", t.name); err != nil {
		return err
	}
	e.syncProxyForImage(ctx, t)
	return nil
}

func (imageStrategy) down(ctx context.Context, e *Engine, t target, extraArgs ...string) error {
	cname := imageContainerName(t.name)
	if len(extraArgs) > 0 {
		// Teardown path (e.g. ephemeral gc passes "-v --rmi local"): remove the
		// container forcefully so it doesn't linger as a stopped container.
		// Anonymous volumes go with -f; named volumes would need compose rm -v
		// but image stacks don't use compose, so -f is the best we can do.
		if err := e.frameCmd(compose.Run(e.RT.Cmd(ctx, "rm", "-f", cname), e.Out, e.Err), "rm", t.name); err != nil {
			return err
		}
	} else {
		// Normal down path: stop only — the manifest owns identity and the
		// container can be restarted via `kazi up`.
		if err := e.frameCmd(compose.Run(e.RT.Cmd(ctx, "stop", cname), e.Out, e.Err), "stop", t.name); err != nil {
			return err
		}
	}
	e.syncProxy(ctx, t.name, "", nil)
	return nil
}

func (imageStrategy) restart(ctx context.Context, e *Engine, t target) error {
	if err := e.frameCmd(compose.Run(e.RT.Cmd(ctx, "restart", imageContainerName(t.name)), e.Out, e.Err), "restart", t.name); err != nil {
		return err
	}
	e.syncProxy(ctx, "", "", nil)
	return nil
}

func (imageStrategy) logs(ctx context.Context, e *Engine, t target, service string, follow bool, tail string) error {
	args := []string{"logs"}
	if follow {
		args = append(args, "-f")
	}
	if tail != "" {
		args = append(args, "--tail", tail)
	}
	args = append(args, imageContainerName(t.name))
	return e.frameCmd(compose.Run(e.RT.Cmd(ctx, args...), e.Out, e.Err), "logs", t.name)
}

// imageRoute returns the HTTP port and routability of an image stack, derived
// from the image's exposed ports.
func (e *Engine) imageRoute(ctx context.Context, t target) (httpPort int, ok bool) {
	out, err := compose.Output(e.RT.Cmd(ctx, "image", "inspect", "--format", "{{json .Config.ExposedPorts}}", t.image))
	if err != nil {
		return 0, false
	}
	ports := parseExposedPorts([]byte(out))
	return classifyPorts(ports, e.Cfg)
}

// syncProxyForImage recomputes routes including this image stack's route.
func (e *Engine) syncProxyForImage(ctx context.Context, t target) {
	var extra *proxy.Plan
	routes := e.desiredRoutes(ctx, t)
	if len(routes) > 0 {
		extra = &proxy.Plan{Routes: routes, Routable: map[string]bool{}}
	}
	e.syncProxy(ctx, "", t.name, extra)
}

// --- containersStrategy: adopted hand-run containers ------------------------

type containersStrategy struct{}

func (containersStrategy) up(ctx context.Context, e *Engine, t target) error {
	if len(t.containers) == 0 {
		return fmt.Errorf("stack %q has no adopted containers", t.name)
	}
	args := append([]string{"start"}, t.containers...)
	if err := e.frameCmd(compose.Run(e.RT.Cmd(ctx, args...), e.Out, e.Err), "start", t.name); err != nil {
		return err
	}
	e.syncProxy(ctx, "", "", nil)
	return nil
}

func (containersStrategy) down(ctx context.Context, e *Engine, t target, extraArgs ...string) error {
	if len(t.containers) == 0 {
		return fmt.Errorf("stack %q has no adopted containers", t.name)
	}
	args := append([]string{"stop"}, t.containers...)
	if err := e.frameCmd(compose.Run(e.RT.Cmd(ctx, args...), e.Out, e.Err), "stop", t.name); err != nil {
		return err
	}
	e.syncProxy(ctx, t.name, "", nil)
	return nil
}

func (containersStrategy) restart(ctx context.Context, e *Engine, t target) error {
	if len(t.containers) == 0 {
		return fmt.Errorf("stack %q has no adopted containers", t.name)
	}
	args := append([]string{"restart"}, t.containers...)
	if err := e.frameCmd(compose.Run(e.RT.Cmd(ctx, args...), e.Out, e.Err), "restart", t.name); err != nil {
		return err
	}
	e.syncProxy(ctx, "", "", nil)
	return nil
}

func (containersStrategy) logs(ctx context.Context, e *Engine, t target, service string, follow bool, tail string) error {
	if len(t.containers) == 0 {
		return fmt.Errorf("stack %q has no adopted containers", t.name)
	}
	// Follow the first container, or the named one when it matches.
	target := t.containers[0]
	if service != "" {
		for _, c := range t.containers {
			if c == service {
				target = c
				break
			}
		}
	}
	args := []string{"logs"}
	if follow {
		args = append(args, "-f")
	}
	if tail != "" {
		args = append(args, "--tail", tail)
	}
	args = append(args, target)
	return e.frameCmd(compose.Run(e.RT.Cmd(ctx, args...), e.Out, e.Err), "logs", t.name)
}

// --- shared helpers ---------------------------------------------------------

// frameCmd wraps a non-compose runtime failure with trailing context.
func (e *Engine) frameCmd(err error, verb, stack string) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("`%s %s` failed for stack %q: %w", e.RT.Name(), verb, stack, err)
}

// sortedEnv renders a values map as sorted "KEY=val" entries with upper-cased
// keys — the -e form for image runs.
func sortedEnv(vals map[string]string) []string {
	out := make([]string, 0, len(vals))
	for k, v := range vals {
		out = append(out, strings.ToUpper(k)+"="+v)
	}
	sort.Strings(out)
	return out
}

// classifyPorts reuses proxy.Classify with a synthetic ServiceInfo so image
// and containers stacks can be classified without a compose config.
func classifyPorts(ports []int, cfg store.Config) (httpPort int, ok bool) {
	if len(ports) == 0 {
		return 0, false
	}
	svc := compose.ServiceInfo{Name: "svc", Ports: ports}
	class, port := proxy.Classify(svc, cfg.Spec.Proxy.HTTPPorts, cfg.Spec.Proxy.TCPPorts)
	if class == proxy.ClassHTTP {
		return port, true
	}
	return 0, false
}

// parseExposedPorts extracts the container-side ports from a docker image
// inspect `.Config.ExposedPorts` map ({"5432/tcp":{}} → [5432]), sorted.
func parseExposedPorts(b []byte) []int {
	b = []byte(strings.TrimSpace(string(b)))
	if len(b) == 0 || string(b) == "null" {
		return nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		return nil
	}
	var ports []int
	for k := range m {
		spec := k
		if i := strings.IndexByte(spec, '/'); i >= 0 {
			spec = spec[:i]
		}
		if n, err := strconv.Atoi(spec); err == nil && n > 0 {
			ports = append(ports, n)
		}
	}
	sort.Ints(ports)
	return ports
}

// parsePsPorts extracts container-side ports from a docker ps `Ports` string,
// e.g. "0.0.0.0:8080->80/tcp, 6379/tcp" → [80, 6379]. Deduplicated, sorted.
func parsePsPorts(s string) []int {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	seen := map[int]bool{}
	var ports []int
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		// Take the container-side port: text after "->" if present, else the whole.
		if i := strings.Index(part, "->"); i >= 0 {
			part = part[i+2:]
		}
		// Strip the "/tcp" | "/udp" suffix.
		if i := strings.IndexByte(part, '/'); i >= 0 {
			part = part[:i]
		}
		// Strip any host:port prefix (e.g. "0.0.0.0:5432").
		if i := strings.LastIndexByte(part, ':'); i >= 0 {
			part = part[i+1:]
		}
		if n, err := strconv.Atoi(strings.TrimSpace(part)); err == nil && n > 0 && !seen[n] {
			seen[n] = true
			ports = append(ports, n)
		}
	}
	sort.Ints(ports)
	return ports
}
