# `kaji` — Compose-preferred (not compose-required), runtime-agnostic local stack manager: starting plan

**Decisions locked:** Go engine · laptop-first, server-mode later · CLI first, TUI later · **OCI runtime-agnostic** (Docker is one backend, not the foundation).

## Stack model & discovery (the lazydocker property, improved)

kaji's view = **union of what it launched and what's already running on the runtime**. Three stack kinds, one interface, capabilities degrade gracefully:

| Kind | How kaji knows it | Lifecycle | Views |
|---|---|---|---|
| **Compose stack** | registered path, or *discovered* via `com.docker.compose.project` labels on running containers | full: up/down/restart/eject/config | per-stack service view, logs, config diff |
| **Adopted (non-compose)** | `kaji adopt <container\|group>` or `kaji run <image>` (plain run + kaji labels, no compose file generated) | start/stop/rm/logs; no `up` semantics | grouped container view |
| **Unmanaged** | runtime scan (`ps` all) minus the above | view-only + one-key `adopt` | flat list, like lazydocker's containers panel |

- **Discovery is passive and free**: compose already labels every container with its project + working dir. `kaji ls` shows compose stacks it never launched; `kaji up/down` on them just works via the label's project directory. No import step.
- **`kaji jump <stack>` / `kj <stack>`**: jump to a stack's compose directory. Blunt constraint: a child process can't change the parent shell's cwd — so like zoxide/direnv, `kaji shell-init` emits a tiny shell function that wraps `kaji jump --print`. One line in `.zshrc`.
- **Per-stack views**: `kaji status <stack>` scopes everything (services, ports, logs, urls) to that stack; `kaji status` alone = global dashboard across all three kinds.
- Non-compose stacks still get proxy hostnames: adopted containers with a declared/detected HTTP port are included in the generated Caddyfile the same way.

## Runtime abstraction (OCI, not Docker)

Everything kaji runs is an OCI image; everything kaji orchestrates is a compose file. Both are standards, so the engine talks to a small `Runtime` interface, never to Docker directly:

- Backends: **docker**, **podman** (rootless-friendly, drop-in CLI), **nerdctl** (containerd). Config: `runtime = "auto" | docker | podman | nerdctl`; auto-detect probes in that order.
- All orchestration goes through `<runtime> compose` — the compose *spec* is the contract, the binary is pluggable. Labels, healthchecks, volumes, `down -v --rmi` semantics are spec-level and portable.
- Consequence for the proxy: **no caddy-docker-proxy plugin** (it watches the Docker API socket — dead on containerd, awkward on podman). Instead kaji renders a Caddyfile from its own stack-store state on every up/down and hot-reloads Caddy (`caddy reload`). Simpler, runtime-agnostic, and kaji already knows every stack it launched — no socket-watching needed.
- `kaji doctor` reports which runtimes are present and which one is active.

## Core design (the 4 fixes to ONCE's narrowness)

1. **App model = stack, compose-preferred but not compose-required.** A stack is a named group of containers. Backing can be: a compose file (full lifecycle), plain containers adopted into kaji, or a template. ONCE inverted: their unit is one HTTP container; ours is a stack of N anything-containers however they were started.
2. **Health = Docker-native, not `/up`.** Priority order: compose `healthcheck` if defined → container state + restart count → optional TCP port probe → *optional* HTTP check only if a stack's config declares `health_url`. Nothing is assumed about the workload.
3. **Declarative YAML config, k8s-style API.** Files are YAML on disk; the engine API speaks JSON payloads of the same resources (k8s convention). Every resource: `apiVersion: kaji.dev/v1alpha1`, `kind`, `metadata`, `spec` (desired) + `status` (observed, engine-owned, never persisted to the manifest). Layout `~/.config/kaji/`:
   - `config.yaml` — `kind: Config`: cleanup policy, template sources, port range, runtime preference, proxy settings
   - `stacks/<name>.yaml` — `kind: Stack`, one manifest per registered stack (replaces the badly-named `registry.toml`; "registry" is reserved for image registries). `spec.source` is one of `compose: <path>`, `template: <name>`, `image: <ref>` (adopted/plain), plus `spec.values`, `spec.proxy`
   - `templates/<name>/` — `compose.yml` (plain, spec-standard `${VAR:-default}` interpolation — NOT a templating language) + `values.yaml` (defaults)
   Per-project override lives next to the code as `kaji.yaml` (a `kind: Stack` overlay). Verbs are kubectl-shaped: `kaji apply -f`, `kaji get stacks -o yaml|json`, `kaji describe <stack>`, `kaji delete`. `kaji config get/set` edits manifests in place.
   **Values, helm-style without helm complexity:** effective env = template `values.yaml` ← Stack `spec.values` ← `-f overrides.yaml` ← `--set key=val` (last wins), flattened into compose interpolation vars. No gotpl/sprig to maintain; a template is just a compose file with sane `${...:-defaults}`.
   **Image auth: inherited, not reimplemented.** The runtime does all pulls, so `~/.docker/config.json` (credsStore/credHelpers) and podman's `auth.json` work as-is. If kaji ever reads registries directly (catalog metadata), it resolves creds via the standard docker-config keychain. kaji stores no secrets.
4. **Ephemeral by default for `try`.** Everything launched gets labels `kaji.managed=true`, `kaji.ephemeral=true|false`. `kaji try X` = up → on exit `down -v --rmi local` unless `--keep`. `kaji gc` sweeps orphans by label. Registered stacks are persistent by default.

## What we steal vs. defer from ONCE (MIT)

- **Steal now:** proxy + SSL (reimagined for localhost: Caddy internal CA + `.localhost`, see Routing model), catalog UX (pick by name, one command), TUI layout ideas, install script pattern.
- **Defer to server mode (M4):** ACME/real-hostname SSL, backup/restore with pre/post hooks, background service.

## Agent-friendly contract (day-one, cheap if done from M0)

One engine, three skins: CLI (humans + agents), TUI (humans), MCP server (agents). All three call the same Go engine package — no logic in any skin.

- Every command supports `--json` (stable schemas, versioned), meaningful exit codes, `--yes`/non-interactive mode, `NO_COLOR`/plain output. Errors as structured JSON on stderr.
- `kaji mcp` (M3): stdio MCP server exposing engine ops as tools — `list_stacks`, `up`, `down`, `try`, `logs`, `urls`, `gc` — so Claude Code orchestrates stacks natively instead of parsing CLI output.
- TUI is read/write over the same engine but never required: anything the TUI can do, the CLI/MCP can do headless.
- Idempotent verbs (`up` on running stack = no-op success) so agent retries are safe.

## Routing model (day-one)

- System stack `kaji-proxy`: plain Caddy, auto-started by `kaji`; kaji generates the Caddyfile from stack-store state and hot-reloads it on every up/down (runtime-agnostic — no Docker-socket watching).
- HTTP services reachable at `https://<stack>.localhost` (`*.localhost` → 127.0.0.1 in browsers by RFC 6761, zero DNS setup). Which service/port gets routed: declared `http_port` in template/`kaji.yaml`, else single-exposed-port auto-detect.
- SSL: Caddy internal CA; `kaji trust` installs it once into the system store → green-lock HTTPS everywhere locally.
- Raw TCP (postgres/redis): hostname routing impossible without Host/SNI — instead store-assigned **stable ports** + `kaji urls` listing every endpoint.
- Server mode later = same architecture, Caddy switches internal CA → ACME/Let's Encrypt for real hostnames.

## Milestones

- **M0 — skeleton (weekend):** Go module, cobra CLI, `Runtime` interface + auto-detect (docker/podman/nerdctl), shell-out to `<runtime> compose` (not compose-go lib — spec compat for free). Commands: `kaji add/ls/rm`, `kaji up|down|restart|status <name>`, `kaji logs <name>`, `kaji ps` (all containers incl. unmanaged), label-based compose discovery, `kaji jump` + `kaji shell-init`. stack manifest store (apply/get/delete). *Done when: kaji sees every running container grouped by stack, and manages 3 existing projects without cd-ing.*
- **M1 — proxy & SSL (day-one scope):** `kaji-proxy` Caddy system stack, generated-Caddyfile routing, `<stack>.localhost` hostnames, `kaji trust`, `kaji urls`, stable port assignment for TCP services. *Done when: 10 stacks up, every HTTP one reachable by name over HTTPS, no ports memorized.*
- **M2 — catalog & lifecycle:** `kaji try <template> [--keep]`, `kaji run <image>` (plain, no compose), `kaji adopt`, `kaji gc`, `kaji eject <template> [dir]`, template seeding from awesome-compose. *Done when: `kaji try postgres` leaves zero residue after exit.*
- **M3 — config, MCP & integration:** `kaji config get/set`, per-stack `kaji.yaml` overrides (env, ports, profiles), `kaji doctor`, `kaji mcp` stdio server for Claude Code, generated lazydocker `customCommands` snippet. *Done when: excalidraw + its MCP run as one customized catalog stack, and Claude Code can up/down/inspect stacks via kaji's MCP tools.*
- **M4 — server mode (opt-in):** same binary on a home server/VPS — Caddy flips to ACME certs on real hostnames, backup hooks (ONCE-style pre/post), background service.
- **M5 — TUI:** bubbletea dashboard over the engine (stacks panel, catalog panel, logs). Only after CLI proves the model.

## First concrete steps (M0, ~in order)

1. `git init kaji && go mod init` — scaffold with cobra; `internal/store` (Stack manifests), `internal/runtime` (interface + docker/podman/nerdctl detect), `internal/compose` (exec wrapper), `internal/labels`
2. Implement stack manifests load/save + `add/ls/rm`
3. Implement compose exec wrapper: `<runtime> compose -p kaji-<name> --project-directory <path> <verb>` + label injection via `--project-name` and override file
4. `status` = `<runtime> compose ls --format json` filtered to kaji-managed + per-stack `ps`
5. Ship to your own machine, live with it a week before M1

## Open items (decide during M0, not now)

Name; config schema versioning policy (v1alpha1 → v1); whether `try` templates support variants/profiles; minimum Go version.
