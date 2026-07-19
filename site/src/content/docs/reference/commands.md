---
title: Commands
description: The full kazi command surface, grouped by task, with global flags and exit codes.
---

Every command supports `--json` for machine-readable output. Handlers stay thin:
they parse flags, call the engine, and render text or JSON.

## Global flags

| Flag | Effect |
|---|---|
| `--json` | Machine-readable output (stable, versioned schemas). |
| `-h`, `--help` | Help for any command. |

## Stacks & lifecycle

| Command | Behavior |
|---|---|
| `kazi add <name> <path>` | Register a stack. `<path>` is a compose file or directory. Fails if the name is taken. |
| `kazi ls` | List registered + auto-discovered stacks (`NAME KIND STATUS PATH`). |
| `kazi rm <name>` | Deregister a stack — deletes the manifest, never touches containers. Prompts if running; `--yes` skips. |
| `kazi up <stack>` | `compose up -d`. Auto-creates the kazi network + proxy on first routable up. |
| `kazi down <stack>` | `compose down`. Removes this stack's proxy routes. |
| `kazi restart <stack>` | `compose restart`. |
| `kazi status [<stack>]` | Global dashboard, or per-service state/health/ports for one stack. |
| `kazi describe <stack>` | Everything about one stack: status, manifest, services, endpoints. |
| `kazi logs <stack> [service]` | Passthrough to `compose logs`; supports `-f/--follow` and `--tail N`. |
| `kazi ps` | Every container on the runtime (including unmanaged), annotated with stack and kind. |

:::note
Lifecycle verbs work on **auto-discovered** compose stacks exactly as on
registered ones — no import step required.
:::

## Routing & endpoints

| Command | Behavior |
|---|---|
| `kazi urls [<stack>]` | Reachable endpoints: HTTPS URLs for HTTP services, `localhost:PORT` for TCP. |
| `kazi expose [--remove] <stack> <service>` | Allocate (or free) a stable host port for a TCP service. Survives down/up. |
| `kazi trust [--uninstall]` | Install (or remove) kazi's local CA in the system trust store. |

## Templates & ephemeral stacks

| Command | Behavior |
|---|---|
| `kazi try <template> [--keep] [-d] [--set k=v]` | Bring up a catalog template as an ephemeral stack. Foreground tears down on Ctrl-C; `-d` detaches; `--keep` makes it persistent. |
| `kazi keep <stack>` | Flip an ephemeral stack to persistent — manifest only, never recreates containers. |
| `kazi template ls\|new\|import\|reset` | Manage the catalog: list, scaffold from an image (`new --from-image`), import a dir/git URL, or reset an embedded starter. |
| `kazi eject <template> [dir] [--add]` | Copy a template's compose + current values into a real project dir; `--add` registers it. |

## Ad-hoc & adopted stacks

| Command | Behavior |
|---|---|
| `kazi run <image> [--name n] [-p\|-e\|-v ...]` | Create a persistent, routed, image-backed stack and start it — no compose file generated. |
| `kazi adopt <name> <container>...` | Group already-running containers into a stack by membership; never recreates or relabels them. |
| `kazi gc [--dry-run] [--yes]` | Reclaim stopped/expired ephemeral stacks, crash-orphaned containers, and stale port allocations. |

## Shell integration

| Command | Behavior |
|---|---|
| `kazi jump <stack> --print` | Print the stack's project directory (used internally by `kj`). |
| `kazi shell-init` | Emit the `kj` shell function for `eval` in your shell RC file. |

## Exit codes

| Code | Meaning |
|---|---|
| `0` | OK |
| `1` | Engine / runtime failure |
| `2` | Usage error |
| `3` | Stack not found |
| `4` | No container runtime available |
