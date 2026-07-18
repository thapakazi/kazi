# kazi

**The control plane for your local containers.**

kazi is a compose-preferred, runtime-agnostic local stack manager: it lets you see every container on your machine grouped by stack and drive existing compose projects without `cd`-ing into them. Docker is first-class and fully tested; podman and nerdctl are auto-detected and best-effort via the same compose-CLI contract.

> **Status: M1 (routing & expose) under active development.**
> See [docs/roadmap.md](docs/roadmap.md) for the full plan.

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

# Start it (auto-creates kazi network + proxy on first up)
./kazi up blog

# List all stacks
./kazi ls

# Check status
./kazi status blog

# See all reachable URLs
./kazi urls

# Follow logs
./kazi logs blog -f

# Stop the stack
./kazi down blog
```

---

## HTTPS for every stack

kazi automatically routes every HTTP service through a local Caddy reverse proxy
with TLS, so `https://blog.localhost` just works — zero port memorization.

### First time setup

```sh
# 1. Register and start your stack — the proxy is created automatically.
kazi add blog ~/repos/blog
kazi up blog

# 2. Trust the local CA once (requires sudo).
kazi trust

# 3. Open https://blog.localhost in your browser.
#    Service name becomes the subdomain automatically:
#    - single HTTP service  → https://blog.localhost
#    - multiple HTTP services → https://api.blog.localhost, etc.
```

### See all endpoints

```sh
kazi urls           # all stacks
kazi urls blog      # one stack
```

Output shows HTTP URLs (https://...) and TCP ports (localhost:NNNNN) for
database-style services that were exposed with `kazi expose`.

### Expose a TCP port

Services that are not HTTP (databases, message brokers) are not proxied
automatically. Use `kazi expose` to get a stable host-port mapping:

```sh
# Allocate a host port in the 42000-42999 range; persists across down/up.
kazi expose blog db

# See the assigned port.
kazi urls blog

# Remove the binding.
kazi expose --remove blog db
```

The port assignment is saved in `~/.config/kazi/state/ports.yaml` and
survives stack restarts.

### CLI tools (curl, psql, redis-cli)

The `kazi trust` CA is not automatically trusted by CLI tools.
Use `--resolve` to route by name without a system trust:

```sh
curl -sk --resolve blog.localhost:443:127.0.0.1 https://blog.localhost
```

Or use `kazi urls` to get the plain `localhost:PORT` for TCP services.

### Firefox note

Firefox uses its own certificate store and does not read the macOS system
keychain by default. To trust kazi's CA in Firefox:

1. Open `about:config` → search for `security.enterprise_roots.enabled`
2. Set it to `true` → Firefox will now use the system keychain.

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
| `kazi up <stack>` | Resolve project dir → stream `compose up -d`. Auto-creates kazi network + proxy on first routable up. |
| `kazi down <stack>` | Run `compose down`. Routes for this stack are removed from the proxy. |
| `kazi restart <stack>` | Run `compose restart`. |
| `kazi status [<stack>]` | No arg: global dashboard grouped by kind. With arg: per-service state, health, and published ports. Supports `--json`. |
| `kazi describe <stack>` (or `-s <stack>`) | Everything about one stack: status, manifest declarations, services, endpoints. Supports `--json`. |
| `kazi logs <stack> [service]` | Passthrough to `compose logs`; supports `-f/--follow` and `--tail N`. |
| `kazi ps` | Every container on the runtime (including unmanaged), annotated with stack and kind. Supports `--json`. |
| `kazi urls [<stack>]` | List all reachable endpoints: HTTPS URLs for HTTP services, `localhost:PORT` for TCP. Supports `--json`. |
| `kazi expose [--remove] <stack> <service>` | Allocate (or free) a stable host port for a TCP service. Port survives down/up. |
| `kazi trust [--uninstall]` | Install (or remove) kazi's local CA into the system trust store. Run once after first `kazi up`. |
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
  proxy:
    httpPorts: [80, 3000, 3001, 5000, 5173, 8000, 8080, 8888]  # ports classified as HTTP
    tcpPorts:  [1521, 3306, 5432, 5672, 6379, 9092, 27017]      # ports classified as TCP/db
  ports:
    range: "42000-42999"   # host-port range for kazi expose allocations
```

All fields have sensible defaults and the file is optional.

**Stack manifests** are stored at `~/.config/kazi/stacks/<name>.yaml`.
Manifests support proxy and expose configuration:

```yaml
apiVersion: kazi.dev/v1alpha1
kind: Stack
metadata:
  name: blog
spec:
  source:
    compose: /home/user/repos/blog/docker-compose.yml
  proxy:
    service: web     # declare the primary HTTP service (needed when >1 HTTP service)
  expose:
    - service: db
      port: auto     # "auto" picks a free port in spec.ports.range; or give a number
```

**Managed paths under `~/.config/kazi/`:**

| Path | Contents |
|---|---|
| `config.yaml` | Global config (runtime, proxy port lists, range) |
| `stacks/<name>.yaml` | One manifest per registered stack |
| `proxy/compose.yml` | Generated Caddy compose file (do not edit) |
| `proxy/Caddyfile` | Generated routing config (do not edit) |
| `state/ports.yaml` | Port allocation ledger for `kazi expose` |

Stack status is always computed live from the runtime and never persisted.

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
