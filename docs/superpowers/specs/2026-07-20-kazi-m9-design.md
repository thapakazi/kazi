# kazi M9 — Exec / Shell-into-Container Design Spec

**Date:** 2026-07-20 · **Scope:** post-roadmap iteration. Builds on shipped M0–M3 ([M0](2026-07-17-kazi-m0-design.md) · [M1](2026-07-17-kazi-m1-design.md) · [M2](2026-07-17-kazi-m2-design.md) · [M3](2026-07-18-kazi-m3-design.md)), M5 ([TUI](2026-07-18-kazi-m5-design.md)); reuses the runtime `exec` constructor introduced in [M8](2026-07-20-kazi-m8-design.md) · **Status:** approved design, pre-implementation

M9 lets you get inside a running container to do work: an interactive shell for humans (`kazi exec blog db`), a captured one-off command for scripts (`kazi exec blog db -- pg_isready`), and an MCP `exec` tool for agents. Shell resolution follows lazydocker's login-shell probe; process launch is a skin concern (CLI passes stdio through, TUI suspends), while container resolution and command construction live in the engine — so nothing exec-related lives only in a skin.

**attach** (connecting to PID 1 stdio) is intentionally **out of scope** — `exec` a new shell/process is what "get inside to work" means, and attach is easy to hang or kill the app with. It can be a later addition if a real need appears.

## Decisions locked during design

| Decision | Choice |
|---|---|
| Operation | `exec` only (new shell/process). No `attach`. |
| Shell resolution | **lazydocker's login-shell probe**: `/bin/sh -c 'eval $(grep ^$(id -un): /etc/passwd \| cut -d : -f 7-)'` — boots the container user's configured shell. Overridable via `--shell` / `-- <cmd>` / config `spec.exec.shell`. |
| Agent surface | Interactive shell (humans/TUI) **plus** captured `-- <cmd>` (scripts) **plus** an MCP `exec` tool (bounded, `destructiveHint`). |
| Skin split | Engine resolves service→container and builds the command; **skins run it** — CLI inherits stdio, TUI suspends via `tea.ExecProcess`. Non-interactive capture (`Exec`) lives in the engine for MCP/scripts. |
| Return pause | After the TUI shell exits, "Press enter to return" — skippable via config `spec.tui.returnImmediately` (kazi's `returnImmediately`). |
| No socket / no secrets | Everything goes through `<runtime> [compose] exec`; creds come from the container env; kazi stores none. |

## Command surface

```
kazi exec <stack> <service> [--shell <path>] [--user u] [--workdir d] [-e K=V]... [--index n] [-- <cmd>...]
```

- **Interactive shell** (no `--`): allocate a TTY and run the login-shell probe (or `--shell`). Requires a terminal — piped/no-TTY without `-- <cmd>` ⇒ exit `2` with a hint to pass a command.
- **One-off command** (`-- <cmd>`): run the exact argv (no shell wrapper), stdout/stderr/stdin passed through, **no TTY** unless `--tty`. Returns the **command's own exit code** (docker/ssh convention).
- **`--json`** (non-interactive only): wrap the result as `{exitCode, stdout, stderr}` so agents/scripts disambiguate kazi failure from the command's exit code (below).
- Flags mirror the small, familiar `docker exec` set: `--user`, `--workdir`, `-e/--env`, `--index` (which replica), `--shell`, `--tty`.

## Service → container resolution (per-source)

Reuses M2's per-source strategy so exec works on every stack kind:

- **compose-backed:** `<runtime> compose -p kazi-<name> --project-directory <dir> exec [--index n] <service> <cmd>` — compose resolves the service to its container.
- **image-backed (M2 `run`):** the stack's single container ⇒ `<runtime> exec <container> <cmd>`.
- **adopted (M2 `containers`):** `<service>` names an adopted container ⇒ `<runtime> exec <container> <cmd>`.

A service with multiple replicas defaults to index 1 (`--index` selects). A stopped/absent service is a clean error (below), never a raw runtime failure.

## Engine methods (shared)

```
ResolveContainer(ctx, stack, service, index) (containerID, error)   // per-source
ExecCommand(stack, service, opts) (*exec.Cmd, error)                // interactive builder — skins run it with inherited stdio
Exec(ctx, stack, service, argv, opts) (ExecResult, error)          // non-interactive capture — MCP/scripts
```

- `ExecResult{ExitCode, Stdout, Stderr}` is the `--json` and MCP wire shape. Reuses the runtime `ExecCmd` constructor added in M8.
- Interactive launch is deliberately *not* an engine method returning data — a TTY-attached process belongs to the skin. The engine hands back a ready `*exec.Cmd`; the CLI runs it inheriting the terminal, the TUI runs it via `tea.ExecProcess`.

## TUI integration

- **`s` (shell)** on a running service row: engine builds the interactive `*exec.Cmd`; the TUI suspends via `tea.ExecProcess`, drops you into the shell, and on exit shows the return pause (unless `spec.tui.returnImmediately`), then resumes — polling paused while suspended, restored on return. Same suspend/restore contract as M6's editor launch.
- Contextual per M5: `s:shell` appears only for **running** service rows; never on stopped containers or non-service selections. Shown live in the keybar.

## MCP

- **`exec` tool:** inputs `stack`, `service`, `command` (argv), optional `user`/`workdir`; returns `ExecResult` with **bounded** stdout/stderr (truncated past a cap with a marker) and a run **timeout** so a hung command can't wedge the agent. Non-interactive only — no TTY over MCP.
- Annotated **`destructiveHint`** (arbitrary in-container commands can mutate state), so hosts prompt. Reuses the CLI's `ExecResult` schema.

## Schema & surface deltas

- **New command:** `kazi exec` — thin file `cmd/kazi/exec.go` over the engine methods (command-organization principle). Full M0 agent contract for *pre-exec* failures; command exit code passed through otherwise.
- **Config:** `spec.exec.shell` (default: empty ⇒ login-shell probe); `spec.tui.returnImmediately` (default `false`). Additive.
- **Engine:** `ResolveContainer`, `ExecCommand`, `Exec`. No manifest schema change.
- **MCP:** `exec` tool (`destructiveHint`).
- **TUI:** `s:shell` contextual binding + suspend/return-pause.

## Error handling & exit codes

- **Pre-exec failures use kazi codes:** stack/service not found ⇒ `3` (list available services); no runtime ⇒ `4`; interactive requested without a TTY ⇒ `2`. Service present but **not running** ⇒ actionable error suggesting `kazi up <stack>`.
- **Once the command runs, kazi returns its exit code verbatim** (like `ssh`/`docker exec`). This overlaps kazi's own codes (a command exiting `3` vs not-found `3`) — the documented `--json` envelope carries kazi-success separately so scripts/agents disambiguate; the raw CLI follows the standard passthrough convention.
- **No shell in image:** probe fails and `/bin/sh` is absent (distroless/scratch) ⇒ clean error naming the image, suggesting `-- <binary>` with an explicit executable.
- **MCP:** output truncated past the cap with a marker; a per-call timeout bounds runaway commands.
- **Security:** exec runs arbitrary commands in containers by design; the MCP tool is destructive-hinted so the host gates it; kazi never persists creds or command history.

## Testing

- **Unit (fake runtime, golden files):** command construction per source (compose `exec` vs `<runtime> exec`; probe wrapper vs `--shell` vs `-- argv`; `--user`/`--workdir`/`-e`/`--index`); TTY-detection → interactive-vs-capture branch; `ResolveContainer` incl. not-running and multi-replica; exit-code passthrough; `--json`/`ExecResult` envelope; MCP output truncation + timeout.
- **TUI (teatest):** `s` on a running service suspends with the correct exec command; not offered on stopped services; return-pause honored and skipped per config.
- **Integration (docker, `integration` tag):** `kazi exec <stack> <svc> -- sh -c 'echo hi'` ⇒ `hi`, exit `0`; `-- false` ⇒ exit `1`; not-running ⇒ exit `3`; MCP `exec` round-trip returns captured output; interactive path smoke-tested via a scripted PTY.

## Acceptance criteria

1. `kazi exec <stack> <service>` opens an interactive login shell (lazydocker-style probe) with a TTY, resolving the container per stack kind; not-running/absent give clear, actionable errors.
2. `kazi exec <stack> <service> -- <cmd>` runs non-interactively, passes through stdin/stdout/stderr, and exits with the command's own code; `--json` wraps `{exitCode, stdout, stderr}`.
3. `--shell` and `spec.exec.shell` override the probe; images with no shell fail cleanly with guidance to pass an explicit binary.
4. TUI `s` on a running service suspends into the shell and returns with a pause (skippable via `spec.tui.returnImmediately`); it's never offered on stopped containers.
5. An MCP `exec` tool lets an agent run a bounded, timed command in a container, destructive-hinted so the host prompts.
6. No Docker API socket is used — exec goes through `<runtime> [compose] exec`; resolution/construction are engine methods reused by CLI, TUI, and MCP, so nothing exec-related lives only in a skin.
