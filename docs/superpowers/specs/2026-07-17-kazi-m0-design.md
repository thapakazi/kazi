# kazi M0 — Design Spec

**Date:** 2026-07-17 · **Scope:** milestone M0 (skeleton) of [the roadmap](../../roadmap.md) · **Status:** approved design, pre-implementation

kazi is a compose-preferred, runtime-agnostic local stack manager. M0 delivers a Go binary that sees every running container on the machine grouped by stack, and manages existing compose projects without `cd`-ing into them.

## Decisions locked during design

| Decision | Choice |
|---|---|
| Name | **kazi** (binary `kazi`, module `github.com/thapakazi/kazi`, labels `kazi.*`, config `~/.config/kazi/`). The roadmap's "kaji" spelling is superseded. |
| Spec scope | M0 only; M1+ appear as forward-compatibility constraints, not features. |
| Runtimes | Docker is first-class and tested. podman and nerdctl are auto-detected and best-effort through the same compose-CLI contract; not M0 blockers. |
| CLI surface | Friendly verbs (`add/ls/rm/up/down/…`) with `--json` everywhere. kubectl-shaped verbs (`apply -f`, `get`, `describe`) deferred to M3. |
| Code structure | Engine facade from day one: thin cobra handlers over `internal/engine`. |
| Go version | 1.25 (latest stable at kickoff). |
| Schema versioning | `kazi.dev/v1alpha1`; alpha may change without migration. Freeze to `v1` when the schema survives M3. |

## Goals & non-goals

**Goal (M0 done-when):** see [Acceptance criteria](#acceptance-criteria). In short: full visibility of all containers grouped by stack; lifecycle control of ≥3 real compose projects; discovery of compose stacks kazi never launched; `kj` directory jumping.

**Non-goals for M0** — deferred but designed-for:

- **Proxy/SSL** (M1): no Caddy, no `*.localhost`, no `kazi trust`.
- **Ad-hoc containers & catalog** (M2): `kazi run <image>`, `kazi adopt`, `kazi try`, `kazi gc`, templates. *Explicit forward-compatibility requirement — see next section.*
- **kubectl verbs, config editing, MCP** (M3).
- **Server mode** (M4), **TUI** (M5).

## Forward compatibility: ad-hoc (non-compose) containers

M2 introduces stacks that are plain containers, not compose projects. Three M0 design points keep that door open and MUST NOT be violated during implementation:

1. **`spec.source` is a union.** `compose: <path>` is the only implemented arm in M0; `image: <ref>` (ad-hoc/adopted) and `template: <name>` are reserved arm names. Adding them later is additive, not a schema break.
2. **Stack kind is data, not code paths.** A stack is "a named group of containers however started." `ls`, `status`, `ps`, and `logs` operate on the grouped-container model and render image-backed stacks unchanged. Only lifecycle verbs differ per source (`compose up/down` vs `start/stop/rm`), isolated behind a per-source strategy inside the engine.
3. **kazi labels are kazi-applied, not compose-applied.** `kazi.managed`/`kazi.stack` identify membership independent of compose, so a future plain `run` container groups and discovers identically.

## Architecture

One engine, many skins (CLI now; MCP M3, TUI M5). All logic lives in the engine; skins parse input, call the engine, render output.

```
cmd/kazi/           cobra commands: flag parsing → engine call → render (text or --json). No logic.
internal/engine/    the single facade: Up, Down, Restart, Status, List, Logs, Ps, Add, Remove, Jump.
internal/store/     Stack/Config manifests: load/save/delete YAML under the config root.
internal/runtime/   Runtime interface + docker/podman/nerdctl detection ("auto" probes in that order).
internal/compose/   exec wrapper: <runtime> compose -p <project> --project-directory <dir> <verb>.
internal/labels/    label names + parse/inject helpers (kazi.*, com.docker.compose.*).
```

- Engine methods take `context.Context` and return typed results (`StackInfo`, `ContainerInfo`, `ServiceStatus`) — the same structs `--json` marshals and the M3 MCP server will expose.
- All orchestration shells out to `<runtime> compose`; the compose *spec* is the contract, the binary is pluggable. kazi never talks to the Docker API socket.
- The `Runtime` interface stays small: availability probe, `Ps(ctx) ([]Container, error)` (via `<runtime> ps -a --format json`), and construction of compose/container commands as `*exec.Cmd` for the compose wrapper to run.
- Config root defaults to `~/.config/kazi/`, overridable via `KAZI_CONFIG_DIR` (used by tests).

## Data model

### Stack manifest — `~/.config/kazi/stacks/<name>.yaml`

```yaml
apiVersion: kazi.dev/v1alpha1
kind: Stack
metadata:
  name: blog
spec:
  source:
    compose: /Users/thapakazi/repos/blog/docker-compose.yml   # absolute path; only arm implemented in M0
```

- Written by `kazi add`, deleted by `kazi rm`. One file per registered stack.
- `status` is engine-computed at read time and **never persisted** to the manifest.

### Config — `~/.config/kazi/config.yaml`

```yaml
apiVersion: kazi.dev/v1alpha1
kind: Config
spec:
  runtime: auto        # auto | docker | podman | nerdctl
```

M0 reads exactly this one field; absent file ⇒ defaults.

### Labels

| Label | Value | Purpose |
|---|---|---|
| `kazi.managed` | `true` | container is kazi-launched |
| `kazi.stack` | `<name>` | stack membership |
| `kazi.ephemeral` | *(reserved, M2)* | named now so the vocabulary is stable |

Injected via a generated compose override file passed as an additional `-f` (pure compose spec ⇒ portable across runtimes).

### Stack kinds (runtime view)

kazi's view = union of three sources, deduped in this priority:

1. **Registered** — manifest exists. Full lifecycle.
2. **Discovered** — running containers carry `com.docker.compose.project` labels but no manifest. Full up/down works via the `…project.working_dir` label. No import step.
3. **Unmanaged** — everything else on the runtime. View-only in M0 (one-key adopt arrives in M2).

## Command surface

| Command | Behavior |
|---|---|
| `kazi add <name> <path>` | Register a stack. `<path>` is a compose file or a directory searched for `compose.y(a)ml` / `docker-compose.y(a)ml`. Fails if name taken (exit 1). |
| `kazi ls` | All registered + discovered stacks. Columns `NAME KIND STATUS PATH`; STATUS like `running 3/3`, `stopped`. |
| `kazi rm <name>` | Deregister only — deletes the manifest, never touches containers. Confirms if the stack is running; `--yes` skips. |
| `kazi up <stack>` | Resolve → project dir → stream `compose up -d`. Idempotent: already-running ⇒ exit 0. |
| `kazi down <stack>` | `compose down` — never `-v` in M0. |
| `kazi restart <stack>` | `compose restart`. |
| `kazi status [<stack>]` | No arg: global dashboard grouped by kind. With arg: per-service state, health, published ports for that stack. |
| `kazi logs <stack> [service]` | Passthrough to `compose logs`; supports `-f/--follow`, `--tail N`. |
| `kazi ps` | Every container on the runtime, including unmanaged, annotated with stack + kind. |
| `kazi jump <stack> --print` | Print the stack's project directory. |
| `kazi shell-init` | Emit a shell function `kj` wrapping `kazi jump --print` with `cd` (zoxide pattern; one line in `.zshrc`). |

Lifecycle verbs work on **discovered** stacks exactly as on registered ones.

### Health (M0 depth)

Per roadmap priority order, M0 implements the first two rungs: compose `healthcheck` result if defined, else container state + restart count. TCP/HTTP probes come later.

### Agent contract (day one)

- `ls`, `status`, `ps` accept `--json`: versioned envelope `{"apiVersion":"kazi.dev/v1alpha1","kind":"<ListKind>","items":[…]}` marshaled directly from engine structs.
- `up/down/restart/add/rm` with `--json` print a one-line result object.
- With `--json`, errors are structured on stderr: `{"error":{"code":"<symbol>","message":"…"}}`.
- Exit codes: `0` ok · `1` engine/runtime failure · `2` usage · `3` stack not found · `4` no container runtime available.
- Idempotent verbs (safe agent retries), `NO_COLOR` respected, `--yes` for the one M0 prompt (`rm`).

## Discovery & project-name resolution

Discovery is passive and on-demand: each command that needs it runs `<runtime> ps -a --format json` once, parses `com.docker.compose.project`, `…project.working_dir`, `…service`, and `kazi.*` labels, and groups containers into the three kinds. No sockets, no daemon, no polling.

**Project naming rule:** kazi launches registered stacks as `-p kazi-<name>` (collision-proof namespace). Exception: if the stack's working directory already has containers under a different compose project name (user ran `docker compose up` by hand before registering), kazi **reuses that existing project name** rather than starting a duplicate copy. Existing project for the same working dir wins; otherwise `kazi-<name>`.

## Error handling

- Engine returns typed errors — `ErrStackNotFound`, `ErrNoRuntime`, `ErrComposeFailed{ExitCode, Stderr}` — mapped by the CLI to the exit codes above.
- Compose subprocess output streams through untouched on `up/down/logs`; kazi frames failures with one trailing line (what failed, which stack, which runtime) rather than swallowing them.
- Registered path missing on disk ⇒ actionable message ("manifest points at X, which no longer exists; fix the path or `kazi rm <name>`"), not a raw exec error.

## Testing

- **Unit:** fake `Runtime` (scripted `ps` JSON, recorded compose invocations). Engine logic — grouping, kind resolution, project-name rule, label injection, manifest CRUD — tested entirely against the fake. Store tests use a temp `KAZI_CONFIG_DIR`.
- **Integration:** `//go:build integration` suite requiring real Docker: register a fixture compose project, drive up → status → logs → down, assert labels and discovery. Manual/CI-optional in M0.

## Acceptance criteria

1. `kazi ps` shows every running container on the machine, grouped registered / discovered / unmanaged.
2. Three real pre-existing compose projects are registered and driven (up/down/restart/logs/status) without ever `cd`-ing.
3. A compose stack started by hand and never registered appears in `kazi ls`, and `kazi down <it>` works.
4. `kj <stack>` changes directory after one `.zshrc` line (`eval "$(kazi shell-init)"`).
5. `ls/status/ps --json` output matches the documented envelope; failure paths produce the documented exit codes.

## Out-of-scope reminders recorded for later milestones

- M1 proxy regenerates a Caddyfile from engine state on every up/down — the engine's typed stack view is the input, so no M0 rework is expected.
- M2 ad-hoc containers: see [Forward compatibility](#forward-compatibility-ad-hoc-non-compose-containers).
- M3 MCP: engine structs are already the wire types; `kazi mcp` becomes a fourth thin skin.
