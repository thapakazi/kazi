# kazi

**The control plane for your local containers.**

kazi is a compose-preferred, runtime-agnostic local stack manager: it lets you see every container on your machine grouped by stack and drive existing compose projects without `cd`-ing into them. Docker is first-class and fully tested; podman and nerdctl are auto-detected and best-effort via the same compose-CLI contract.

> **Status: M0 (skeleton) under active development.**
> See [docs/roadmap.md](docs/roadmap.md) for the full plan and [docs/superpowers/specs/2026-07-17-kazi-m0-design.md](docs/superpowers/specs/2026-07-17-kazi-m0-design.md) for the M0 design spec.

---

## Requirements

- Go 1.25+
- A container runtime with compose support (`docker compose`)
- [`just`](https://github.com/casey/just) (optional, but recommended for the recipes below)

---

## Quick start

```sh
git clone https://github.com/thapakazi/kazi
cd kazi

# Build (with just)
just build

# Build (plain Go)
go build -o kazi ./cmd/kazi

# Register a stack
./kazi add blog ~/repos/blog

# Start it
./kazi up blog

# List all stacks
./kazi ls

# Check status
./kazi status blog

# Follow logs
./kazi logs blog -f

# Stop the stack
./kazi down blog
```

---

## Shell integration

Add one line to `~/.zshrc` (or `~/.bashrc`) to get the `kj` function that jumps to a stack's project directory:

```sh
eval "$(kazi shell-init)"
```

Then:

```sh
kj blog    # cd into blog's project directory
```

---

## Commands

| Command | Behavior |
|---|---|
| `kazi add <name> <path>` | Register a stack. `<path>` is a compose file or directory. Fails if the name is already taken (exit 1). |
| `kazi ls` | List all registered + auto-discovered stacks. Columns: `NAME KIND STATUS PATH`. Supports `--json`. |
| `kazi rm <name>` | Deregister a stack — deletes the manifest, never touches containers. Prompts if running; `--yes` skips. |
| `kazi up <stack>` | Resolve project dir → stream `compose up -d`. Idempotent: already-running → exit 0. |
| `kazi down <stack>` | Run `compose down` (never `-v` in M0). |
| `kazi restart <stack>` | Run `compose restart`. |
| `kazi status [<stack>]` | No arg: global dashboard grouped by kind. With arg: per-service state, health, and published ports. Supports `--json`. |
| `kazi logs <stack> [service]` | Passthrough to `compose logs`; supports `-f/--follow` and `--tail N`. |
| `kazi ps` | Every container on the runtime (including unmanaged), annotated with stack and kind. Supports `--json`. |
| `kazi jump <stack> --print` | Print the stack's project directory (used internally by `kj`). |
| `kazi shell-init` | Emit the `kj` shell function for eval in your shell RC file. |

Lifecycle verbs (`up/down/restart/logs/status`) work on **auto-discovered** compose stacks (containers with `com.docker.compose.project` labels) exactly as on registered ones — no import step required.

### Exit codes

| Code | Meaning |
|---|---|
| `0` | OK |
| `1` | Engine / runtime failure |
| `2` | Usage error |
| `3` | Stack not found |
| `4` | No container runtime available |

---

## Configuration

Config lives in `~/.config/kazi/` (override with `KAZI_CONFIG_DIR`).

**`~/.config/kazi/config.yaml`**

```yaml
apiVersion: kazi.dev/v1alpha1
kind: Config
spec:
  runtime: auto   # auto | docker | podman | nerdctl
```

Stack manifests are stored at `~/.config/kazi/stacks/<name>.yaml` and written/deleted by `kazi add` / `kazi rm`. Stack status is always computed live from the runtime and never persisted.

---

## Development

```sh
just          # list all recipes
just build    # go build -o kazi ./cmd/kazi
just test     # go test ./...
just vet      # go vet ./...
just fmt      # gofmt -l -w .
just check    # fmt + vet + test
just install  # go install ./cmd/kazi
just run ls   # go run ./cmd/kazi ls
just clean    # remove the kazi binary
```

Plain Go equivalents (no `just` required):

```sh
go build ./...
go test ./...
go vet ./...
```

**Integration tests** require a running Docker daemon:

```sh
just test-integration
# or: go test -tags integration ./internal/engine/ -v
```
