# kazi M9 — Exec / Shell-into-Container Implementation Plan

**Date:** 2026-07-20 · **Tracks:** [M9 design spec](2026-07-20-kazi-m9-design.md) · **Status:** ready to implement (TDD)

M9 is the **exec foundation**: service→container resolution and exec command
construction that CLI, TUI, and (later) MCP all reuse. It is sequenced **before
[M8 backup](2026-07-20-kazi-m8-implementation-plan.md)** because M8's `exec`-driver
logical dumps and its http-driver curl fallback both sit on the same
`ResolveContainer` + exec-command builder this milestone delivers (see *Reuse for M8*).

Follows the repo rules: resolution + command construction live in the engine facade;
skins only *run* the process; everything shells out to `<runtime> [compose] exec` —
never the Docker socket.

## Design fidelity (what the spec locks)

- **`exec` only, no `attach`.** New shell/process; attach is out of scope.
- **Login-shell probe** (lazydocker): `/bin/sh -c 'eval $(grep ^$(id -un): /etc/passwd | cut -d : -f 7-)'`.
  Overridable by `--shell`, by `-- <cmd>`, or by config `spec.exec.shell`.
- **Skin split:** engine resolves the container and *builds* the command; the CLI runs
  it inheriting stdio, the TUI runs it via `tea.ExecProcess`. Non-interactive capture
  (`Exec`) lives in the engine for scripts/MCP.
- **Exit codes:** pre-exec failures use kazi codes (not-found `3`, no-runtime `4`,
  interactive-without-TTY `2`); once the command runs, its own exit code passes through
  verbatim. `--json` carries kazi-success separately so scripts disambiguate.

## Reuse for M8 (why this comes first)

M8's logical backup drivers do **not** duplicate resolution/exec — they call the
primitives built here:

- `ResolveContainer(ctx, stack, service, index)` → container id, per source.
- The internal exec **`*exec.Cmd` builder** — M8 reuses it with **stdout redirected to
  the artifact file** (a `pg_dump` can be multi-GB, so M8 must *stream to file*, not
  capture into `ExecResult.Stdout`).

Design consequence for this plan: factor the builder as an unexported
`execCmd(ctx, t, service, argv, opts) (*exec.Cmd, error)` that wires **no stdio** itself.
`ExecCommand` (interactive) adds `-it`; `Exec` (capture) wires buffers; M8's streaming
wrapper wires `cmd.Stdout = file`. Keep that seam clean and M8 Phase 2 is a thin add.

## Runtime interface: reuse, don't grow

`Runtime.Cmd(ctx, args...)` builds `<bin> exec …`; `Runtime.ComposeCmd(...)` builds
`<bin> compose … exec …`. Both already exist — **no interface change**, no `fake.go`
change beyond what its recorded-`Cmd`/`ComposeCmd` calls already capture. The M8/M9 specs
speak of a "runtime exec constructor"; in this codebase that is the existing generic
`Cmd`/`ComposeCmd`, and the exec argv is assembled in the engine.

## Package layout

```
internal/engine/
  exec.go        # ResolveContainer, ExecCommand, Exec, execCmd (shared builder), ExecOpts, ExecResult
  exec_test.go
cmd/kazi/
  exec.go        # thin: TTY detect → interactive vs capture; -- splitting; --json envelope
internal/tui/
  (extend keys.go/update.go/messages.go)  # s:shell contextual binding + suspend/return-pause
internal/store/
  (extend config)                          # spec.exec.shell, spec.tui.returnImmediately
```

## Types (Phase 0)

```go
type ExecOpts struct {
    Shell    string   // --shell / spec.exec.shell; "" ⇒ login-shell probe
    User     string   // --user
    Workdir  string   // --workdir
    Env      []string // -e K=V (repeatable)
    Index    int      // --index replica, default 1
    TTY      bool     // allocate a TTY (interactive always; capture only with --tty)
}
type ExecResult struct {
    ExitCode int    `json:"exitCode"`
    Stdout   string `json:"stdout"`
    Stderr   string `json:"stderr"`
}
```

`store` config additions (additive, `KnownFields(true)` still passes):
`ExecConfig{ Shell string }` under `spec.exec`; `TUIConfig.ReturnImmediately bool` under
`spec.tui`. Golden round-trip test.

## Phased, test-first work

Each phase = failing unit test (fake runtime + golden argv) → implement → green.

**Phase 0 — types & config.** `ExecOpts`/`ExecResult`; `spec.exec.shell` +
`spec.tui.returnImmediately`. Config round-trip golden.

**Phase 1 — `ResolveContainer` (per-source).** Reuse `resolve()`→`target`. Branches:
- **compose:** don't pre-resolve to an id — compose resolves the service itself; return a
  sentinel that tells `execCmd` to use `compose … exec [--index n] <service>`. (Keeps the
  compose path free of a `ps` round-trip and honors `--index`.)
- **image:** the stack's single container id (from `Ps` + compose/kazi labels).
- **adopted (`containers`):** `service` must name an adopted container; else clean error.
Tests: each source; service-not-found ⇒ `ErrStackNotFound`-family; service present but
**not running** ⇒ actionable "run `kazi up <stack>`" error; multi-replica default index 1.

**Phase 2 — `execCmd` builder + `ExecCommand`.** The shared unexported builder assembles
argv for both paths:
- compose: `compose -p kazi-<name> --project-directory <dir> exec [-it] [--index n]
  [--user u] [--workdir d] [-e K=V]... <service> <tail>`
- `<runtime> exec`: `exec [-it] [--user u] [--workdir d] [-e K=V]... <container> <tail>`
where `<tail>` is: the login-shell probe (no `--`, no `--shell`), or `--shell <path>` as
the shell, or the verbatim `-- argv` (no shell wrapper). `ExecCommand` returns the
interactive `*exec.Cmd` (`-it`) for skins to run with inherited stdio; it wires no buffers.
Golden argv tests for every combination (probe vs `--shell` vs `-- argv`; each source;
`--user`/`--workdir`/`-e`/`--index`; TTY on/off).

**Phase 3 — `Exec` (non-interactive capture).** Wraps `execCmd` (no TTY unless
`opts.TTY`), runs via `compose.Output`-style capture into `ExecResult{ExitCode, Stdout,
Stderr}`. Exit-code passthrough: the command's own code lands in `ExitCode`; a *pre-exec*
failure returns a kazi error instead. Tests: exit-code passthrough (`true`→0, `false`→1),
stdout/stderr capture, pre-exec error distinct from command failure.

**Phase 4 — CLI (`cmd/kazi/exec.go`).** Thin. Split positional args at `--` via
`cmd.ArgsLenAtDash()`: before = `<stack> <service>`, after = command argv. Branch on
presence of `--` **and** TTY (`term.IsTerminal(os.Stdin.Fd())`):
- no `--`, TTY ⇒ `ExecCommand`, run inheriting stdio (interactive shell).
- no `--`, no TTY ⇒ exit `2` with "pass `-- <cmd>`" hint.
- `-- <cmd>` ⇒ `Exec`; print stdout/stderr, exit with the command's code; `--json` wraps
  the `ExecResult` envelope. Pre-exec errors map through the existing `exitCode`/`errCode`
  (`3`/`4`/`2`). Register flags mirroring `docker exec`: `--shell --user --workdir -e
  --index --tty`. Tests via `root_test.go` harness.

**Phase 5 — TUI shell (DONE, menu-based).** Reconciled with the built TUI, where `s` is
already the stack action menu and there is no per-service-row selection. Shipped as a
**`shell` item in the stack quick-actions menu** (running, non-system stacks): choosing it
resolves running services and either suspends straight in (one) or opens a transient service
picker (`modalShellChoose`, many). Suspend via `tea.ExecProcess` (same contract as M6's editor
launch, `shellCmd` package-var for test substitution); the return pause is a host `sh -c`
wrapper (`wrapReturnPause`) that runs the single-quoted argv then waits for enter unless
`spec.tui.returnImmediately`. Picker renders as a list in `renderModal`. Unit-covered via the
`shellCmd` recorder (menu gating, single/multi dispatch, pause honored/skipped, picker render).
**5b dropped:** a per-container cursor in the Services tab was considered and deemed
unnecessary — the menu + picker cover the need.

**Phase 6 — MCP `exec` tool — BLOCKED, deferred.** ⚠️ There is **no MCP server in the repo
yet** (roadmap slated `kazi mcp` for M3; never built — only "MCP later" comments remain).
The engine `Exec`/`ExecResult` is the MCP-ready contract; the actual tool (bounded/truncated
output, per-call timeout, `destructiveHint`) wires up when the MCP transport milestone lands.
This plan delivers the reusable method and stops there — do **not** invent an MCP server as a
side effect of M9. (Same gap applies to M8's MCP surface.)

**Phase 7 — integration (`integration` tag, docker). DONE.** `internal/engine/execint_test.go`
drives real `docker compose exec` over the `app` fixture: capture (`echo` ⇒ exit 0), exit-code
passthrough (`false` ⇒ exit 1, not a kazi error), `ResolveContainer` returns a real id,
unknown service ⇒ `ErrServiceNotFound`, `ExecCommand` builds a runnable `compose exec`, and
after `down` the service resolves to a clean not-running error. Verified live (docker 27.4.0,
16s). Interactive PTY handover is exercised by the same `tea.ExecProcess` path as the editor
launch; the CLI passthrough/`--json`/exit-3 flows were smoke-tested against the built binary.

**Bug found + fixed during Phase 7:** `docker compose exec` allocates a pseudo-TTY by default
and errors ("input device is not a TTY") when capturing in a non-terminal, unlike plain
`docker exec`. `execCmd` now adds `-T` on the compose path when `!tty`, so capture works from
scripts/agents. Covered by `TestExecComposeCaptureDisablesTTY`.

## Acceptance mapping

Design ACs 1–6 map to: interactive login shell + per-source resolution (Phases 1–2, 4) →
AC1; non-interactive `-- <cmd>` passthrough + `--json` (Phase 3–4) → AC2; `--shell`/config
override + no-shell error (Phases 2, 4, 7) → AC3; TUI `s` suspend/return (Phase 5) → AC4;
MCP bounded exec (Phase 6, **deferred on the MCP server**) → AC5; no socket, engine-shared
resolution/construction (Phases 1–3) → AC6.

## Open questions to resolve during Phase 0

1. **TTY detection dependency:** use `golang.org/x/term` (`term.IsTerminal`) — confirm it's
   an acceptable new dep, or hand-roll an `isatty` for darwin/linux.
2. **compose `exec --index` vs id path:** confirm returning a "use compose" sentinel from
   `ResolveContainer` (rather than resolving to a container id) is the cleaner seam for M8
   reuse — M8's exec driver likewise wants the compose path to keep service semantics.
3. **`spec.exec.shell` precedence:** lock the order `--shell` (flag) > `-- argv` (explicit
   command, no shell) > `spec.exec.shell` (config) > login-shell probe (default).
