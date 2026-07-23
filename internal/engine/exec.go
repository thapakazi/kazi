package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
)

// M9 — exec / shell-into-container. Container resolution and command
// construction live here so CLI, TUI, and (later) MCP all reuse one path;
// skins only run the process. Everything shells out to `<runtime> [compose]
// exec` — the Docker API socket is never touched.
//
// This file holds the shared types. ResolveContainer / ExecCommand / Exec
// land in subsequent phases; the exec `*exec.Cmd` builder is factored to wire
// no stdio of its own so M8's backup driver can reuse it with stdout streamed
// to an artifact file.

// ExecOpts carries the docker-exec-style knobs. Its zero value means: login-shell
// probe, current container user/workdir, no extra env, replica index 1 (resolved
// downstream), no TTY.
type ExecOpts struct {
	Shell   string   // --shell / spec.exec.shell; "" ⇒ login-shell probe
	User    string   // --user
	Workdir string   // --workdir
	Env     []string // -e K=V (repeatable), each "K=V"
	Index   int      // --index replica; 0/1 ⇒ first replica
	TTY     bool     // allocate a TTY (always for interactive; capture only with --tty)
}

// ExecResult is the non-interactive capture shape returned by Exec — the wire
// form for `kazi exec --json` and the (later) MCP exec tool.
type ExecResult struct {
	ExitCode int    `json:"exitCode"`
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
}

// Pre-exec resolution failures. Both map to exit 3 in the CLI (stack/service
// not found or not runnable), distinct from a runtime absence (4) or the
// command's own exit code (passthrough).
var (
	ErrServiceNotFound   = errors.New("service not found")
	ErrServiceNotRunning = errors.New("service not running")
)

// resolvedExec is a resolved exec destination for one service. Either it defers
// to compose service resolution (compose/template stacks → `compose … exec
// [--index n] <service>`, useCompose=true) or it names a concrete container
// (image/adopted stacks → `<runtime> exec <container>`). container is always the
// resolved container id — used for the running check, for the direct-exec path,
// and reused by M8's backup driver.
type resolvedExec struct {
	tgt        target
	service    string
	index      int
	container  string
	useCompose bool
}

// ResolveContainer resolves (stack, service, index) to a concrete container id,
// per source. Errors: ErrStackNotFound (unknown stack), ErrServiceNotFound
// (unknown service / replica out of range), ErrServiceNotRunning.
func (e *Engine) ResolveContainer(ctx context.Context, stack, service string, index int) (string, error) {
	r, err := e.resolveExec(ctx, stack, service, index)
	if err != nil {
		return "", err
	}
	return r.container, nil
}

// resolveExec is the shared resolver behind ResolveContainer, ExecCommand, and
// Exec: it finds the stack's target (project/dir/srcKind) and the container
// backing the requested service, choosing the compose-exec vs direct-exec path.
func (e *Engine) resolveExec(ctx context.Context, stack, service string, index int) (resolvedExec, error) {
	if index <= 0 {
		index = 1
	}
	t, err := e.resolve(ctx, stack)
	if err != nil {
		return resolvedExec{}, err
	}
	st, err := e.Status(ctx, stack)
	if err != nil {
		return resolvedExec{}, err
	}
	switch t.srcKind {
	case "compose", "template":
		return e.resolveComposeExec(t, st, service, index)
	case "image":
		return e.resolveImageExec(t, st, stack, service)
	case "containers":
		return e.resolveAdoptedExec(t, st, service)
	default:
		return resolvedExec{}, fmt.Errorf("exec is not supported for stack %q (source %s)", stack, t.srcKind)
	}
}

// resolveComposeExec picks the index-th replica of a compose service. Replicas
// are ordered by container name (docker names them <project>-<service>-<n>).
func (e *Engine) resolveComposeExec(t target, st StackInfo, service string, index int) (resolvedExec, error) {
	if len(st.Containers) == 0 {
		return resolvedExec{}, fmt.Errorf("%w: stack %q has no containers; run `kazi up %s`", ErrServiceNotRunning, t.name, t.name)
	}
	var reps []ContainerInfo
	for _, c := range st.Containers {
		if c.Service == service {
			reps = append(reps, c)
		}
	}
	if len(reps) == 0 {
		return resolvedExec{}, fmt.Errorf("%w: %s/%s (services: %s)", ErrServiceNotFound, t.name, service, serviceList(st))
	}
	sort.Slice(reps, func(i, j int) bool { return reps[i].Name < reps[j].Name })
	if index > len(reps) {
		return resolvedExec{}, fmt.Errorf("%w: %s/%s replica %d, but only %d running", ErrServiceNotFound, t.name, service, index, len(reps))
	}
	picked := reps[index-1]
	if picked.State != "running" {
		return resolvedExec{}, fmt.Errorf("%w: %s/%s is %s; run `kazi up %s`", ErrServiceNotRunning, t.name, service, picked.State, t.name)
	}
	return resolvedExec{tgt: t, service: service, index: index, container: picked.ID, useCompose: true}, nil
}

// resolveImageExec resolves an image stack's single container. The service, if
// given, must name the stack (image stacks expose one service named after it).
func (e *Engine) resolveImageExec(t target, st StackInfo, stack, service string) (resolvedExec, error) {
	if len(st.Containers) == 0 {
		return resolvedExec{}, fmt.Errorf("%w: stack %q has no container; run `kazi up %s`", ErrServiceNotRunning, stack, stack)
	}
	c := st.Containers[0]
	if service != "" && service != stack && service != c.Service {
		return resolvedExec{}, fmt.Errorf("%w: %s/%s (service: %s)", ErrServiceNotFound, stack, service, stack)
	}
	if c.State != "running" {
		return resolvedExec{}, fmt.Errorf("%w: %s is %s; run `kazi up %s`", ErrServiceNotRunning, stack, c.State, stack)
	}
	return resolvedExec{tgt: t, service: service, index: 1, container: c.ID, useCompose: false}, nil
}

// resolveAdoptedExec resolves an adopted stack's service, matched against the
// adopted container's name (or its compose-service label, if any).
func (e *Engine) resolveAdoptedExec(t target, st StackInfo, service string) (resolvedExec, error) {
	for _, c := range st.Containers {
		if c.Name == service || (c.Service != "" && c.Service == service) {
			if c.State != "running" {
				return resolvedExec{}, fmt.Errorf("%w: %s/%s is %s; run `kazi up %s`", ErrServiceNotRunning, t.name, service, c.State, t.name)
			}
			return resolvedExec{tgt: t, service: service, index: 1, container: c.ID, useCompose: false}, nil
		}
	}
	return resolvedExec{}, fmt.Errorf("%w: %s/%s (containers: %s)", ErrServiceNotFound, t.name, service, containerNames(st))
}

// loginShellProbe is lazydocker's login-shell resolver: boot the container
// user's configured shell (field 7 of its /etc/passwd line) via a POSIX sh.
const loginShellProbe = `eval $(grep ^$(id -un): /etc/passwd | cut -d : -f 7-)`

// ExecCommand resolves (stack, service) and builds the INTERACTIVE exec command:
// a TTY is allocated and stdin forwarded. The engine returns a ready *exec.Cmd
// with no stdio wired; the skin runs it — the CLI inheriting the terminal, the
// TUI via tea.ExecProcess. Tail resolution (most explicit wins): an explicit
// argv (`-- cmd`) runs verbatim; else --shell / spec.exec.shell; else the
// login-shell probe.
func (e *Engine) ExecCommand(ctx context.Context, stack, service string, argv []string, opts ExecOpts) (*exec.Cmd, error) {
	r, err := e.resolveExec(ctx, stack, service, opts.Index)
	if err != nil {
		return nil, err
	}
	if opts.Shell == "" {
		opts.Shell = e.Cfg.Spec.Exec.Shell
	}
	return e.execCmd(ctx, r, argv, opts, true, true), nil
}

// Exec runs argv non-interactively inside the resolved service container and
// captures the result. Stdout/stderr are buffered into ExecResult and the
// command's own exit code lands in ExitCode (docker/ssh passthrough) — a
// non-zero command is NOT a kazi error, so err stays nil and callers/`--json`
// disambiguate kazi failure from a non-zero command. A pre-exec failure
// (unknown stack/service, not running, no runtime) returns a non-nil error with
// a zero ExecResult. No TTY unless opts.TTY; stdin is not forwarded (capture).
func (e *Engine) Exec(ctx context.Context, stack, service string, argv []string, opts ExecOpts) (ExecResult, error) {
	r, err := e.resolveExec(ctx, stack, service, opts.Index)
	if err != nil {
		return ExecResult{}, err
	}
	if opts.Shell == "" {
		opts.Shell = e.Cfg.Spec.Exec.Shell
	}
	cmd := e.execCmd(ctx, r, argv, opts, false, opts.TTY)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	res := ExecResult{Stdout: stdout.String(), Stderr: stderr.String()}
	if runErr == nil {
		return res, nil
	}
	var xe *exec.ExitError
	if errors.As(runErr, &xe) {
		// The command ran and exited non-zero: passthrough, not a kazi error.
		res.ExitCode = xe.ExitCode()
		return res, nil
	}
	// Couldn't start the command (missing binary, runtime failure) ⇒ kazi error.
	return res, runErr
}

// execCmd builds the runtime command for a resolved exec destination. It wires
// NO stdio itself — callers attach it (ExecCommand inherits the terminal, Exec
// captures buffers, M8's backup driver streams stdout to a file). stdinAttach
// adds -i; tty additionally adds -t. argv, when non-empty, is the exact command
// (no shell wrapper); otherwise opts.Shell, else the login-shell probe.
func (e *Engine) execCmd(ctx context.Context, r resolvedExec, argv []string, opts ExecOpts, stdinAttach, tty bool) *exec.Cmd {
	flags := execFlags(opts, stdinAttach, tty)
	tail := execTail(argv, opts.Shell)
	if r.useCompose {
		args := []string{"exec"}
		if r.index > 1 {
			args = append(args, "--index", strconv.Itoa(r.index))
		}
		// `compose exec` allocates a pseudo-TTY by default; -T disables it so
		// capture works in a non-terminal (plain `docker exec` defaults the other way).
		if !tty {
			args = append(args, "-T")
		}
		args = append(args, flags...)
		args = append(args, r.service)
		args = append(args, tail...)
		return e.composeCmdFor(ctx, r.tgt, args...)
	}
	args := []string{"exec"}
	args = append(args, flags...)
	args = append(args, r.container)
	args = append(args, tail...)
	return e.RT.Cmd(ctx, args...)
}

// execFlags builds the common `<runtime> exec` option tokens in a stable order.
func execFlags(opts ExecOpts, stdinAttach, tty bool) []string {
	var f []string
	if stdinAttach || tty {
		f = append(f, "-i")
	}
	if tty {
		f = append(f, "-t")
	}
	if opts.User != "" {
		f = append(f, "--user", opts.User)
	}
	if opts.Workdir != "" {
		f = append(f, "--workdir", opts.Workdir)
	}
	for _, kv := range opts.Env {
		f = append(f, "--env", kv)
	}
	return f
}

// execTail is the command run inside the container: an explicit argv verbatim,
// else the chosen shell, else the login-shell probe.
func execTail(argv []string, shell string) []string {
	switch {
	case len(argv) > 0:
		return argv
	case shell != "":
		return []string{shell}
	default:
		return []string{"/bin/sh", "-c", loginShellProbe}
	}
}

// serviceList returns the stack's distinct service names, sorted, for error hints.
func serviceList(st StackInfo) string {
	seen := map[string]bool{}
	var out []string
	for _, c := range st.Containers {
		if c.Service != "" && !seen[c.Service] {
			seen[c.Service] = true
			out = append(out, c.Service)
		}
	}
	sort.Strings(out)
	return strings.Join(out, ", ")
}

// containerNames returns the stack's container names, sorted, for error hints.
func containerNames(st StackInfo) string {
	out := make([]string, 0, len(st.Containers))
	for _, c := range st.Containers {
		out = append(out, c.Name)
	}
	sort.Strings(out)
	return strings.Join(out, ", ")
}
