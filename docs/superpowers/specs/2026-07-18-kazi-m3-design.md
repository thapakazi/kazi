# kazi M3 — Config, MCP & Integration Design Spec

**Date:** 2026-07-18 · **Scope:** milestone M3 of [the roadmap](../../roadmap.md), building on implemented M0/M1 and the [M2 spec](2026-07-17-kazi-m2-design.md) · **Status:** approved design (sections 2–3 intentionally high-level), pre-implementation

M3 completes the declarative story (`kazi.yaml` overlays, kubectl verbs, `config get/set`), ships the MCP server for agents, and adds `doctor` plus the lazydocker integration.

## Decisions locked during design

| Decision | Choice |
|---|---|
| MCP surface | Full lifecycle; destructive ops (`rm`, `gc`, `expose`) exposed but annotated `destructiveHint` so hosts prompt. Read-only tools annotated `readOnlyHint`. |
| kubectl verbs | Full set now: `apply -f`, `get -o yaml\|json`, `describe` (already implemented), `delete` (alias of `rm`). |
| Overlay precedence | `kazi.yaml` (repo-committed) is base; the machine manifest merges over it — local intent wins. |
| MCP SDK | Official `modelcontextprotocol/go-sdk`, stdio transport. |

## Declarative surface

### `kazi.yaml` per-project overlay

`kind: Stack` overlay next to the code. Supported fields: `metadata.name` (default: dir name), `spec.proxy`, `spec.profiles` (compose profiles — new field), `spec.values`, `spec.expose`.

- **Precedence:** manifest-over-overlay; full values chain: template `values.yaml` ← `kazi.yaml` `spec.values` ← manifest `spec.values` ← `-f overrides.yaml` ← `--set` (last wins).
- **Bare `kazi up`:** in a dir with `kazi.yaml` or a compose file, resolves the stack by cwd and auto-registers on first use. No import step, extended to the current project.

### kubectl verbs

- `kazi apply -f <file|->` — create-or-update any kind (Stack, Config); validates; reports `created|configured|unchanged`; idempotent (agent-safe write path).
- `kazi get stacks|stack <name>|config [-o yaml|json]` — store reads with live `status`.
- `kazi delete stack <name>` — same engine call and confirms as `rm`.

### `kazi config get/set`

Dotted-path read/write on Config or a named Stack manifest (`kazi config set blog spec.proxy.service web`). Edits via the yaml.v3 node API so user comments survive; schema-checked before save; "restart to take effect" notice when a running stack's runtime state is affected.

## `kazi mcp`

Stdio MCP server, fourth thin skin over the engine (no logic of its own). Register: `claude mcp add kazi -- kazi mcp`.

- **Tools:** `list_stacks`, `status`, `up`, `down`, `restart`, `logs` (bounded tail, no follow), `urls`, `try` (always detached + ephemeral; reclaim via `gc`), `doctor`; gated: `rm`, `gc`, `expose`.
- Tool results reuse the CLI's `--json` schemas verbatim — one contract, versioned once.

## Doctor & integrations

- **`kazi doctor [--json]`:** runtimes present/active + compose version; proxy & `kazi` network health; trust installed; `*.localhost` resolution check; port-range headroom; manifest validation across the store.
- **`kazi integrations lazydocker`:** prints the generated `customCommands` snippet.

## Deltas

- **Schema:** `spec.profiles` on Stack (additive). Nothing else.
- **New commands:** `apply`, `get`, `delete`, `config get|set`, `doctor`, `mcp`, `integrations lazydocker`. All follow the M0 agent contract.

## Error handling

- `apply`: validation failures name the field path; unknown `kind`/`apiVersion` rejected explicitly.
- Overlay conflicts never error — precedence resolves them; `describe` shows the effective merged spec with per-field origin (template/overlay/manifest/flag).
- MCP: engine errors map to MCP tool errors with the CLI's structured error codes; destructive tools without host approval simply aren't invoked (host-side gate).
- `doctor` exits non-zero if any check fails (scriptable), each finding with a fix hint.

## Testing

- **Unit:** merge-chain table tests (template/overlay/manifest/-f/--set); dotted-path edits preserving comments (golden files); apply idempotency (`unchanged` on re-apply); doctor check matrix against the fake runtime.
- **Integration:** MCP round-trip — spawn `kazi mcp`, drive `list_stacks → up → status → logs → down` against a fixture stack via an MCP client; bare-`kazi up` auto-registration in a fixture project dir.

## Acceptance criteria

1. excalidraw + its MCP server run as one customized catalog stack, configured via `kazi.yaml` (roadmap headline).
2. Claude Code, via `kazi mcp`, lists, ups, inspects (status/logs/urls), and downs a stack; `rm`/`gc` prompt at the host before executing.
3. `kazi get stack X -o yaml | kazi apply -f -` round-trips as `unchanged`.
4. `kazi config set` edits preserve hand-written comments in the manifest.
5. `kazi doctor` passes clean on a healthy setup and pinpoints a stopped proxy, missing trust, and absent runtime with fix hints.
