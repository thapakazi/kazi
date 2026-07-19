# kazi M5 — TUI Design Spec

**Date:** 2026-07-18 · **Scope:** milestone M5 of [the roadmap](../../roadmap.md), building on the implemented M0 ([spec](2026-07-17-kazi-m0-design.md)), M1 ([spec](2026-07-17-kazi-m1-design.md)), M2 ([spec](2026-07-17-kazi-m2-design.md)), and M3 ([spec](2026-07-18-kazi-m3-design.md)). M4 (server mode) is **not** a prerequisite — the TUI sits on the engine as of M3 · **Status:** approved design, pre-implementation

M5 delivers a bubbletea dashboard over the same engine the CLI and MCP already use: see every stack grouped by kind, drive its full lifecycle, tail logs, browse the catalog — vim-navigable, keyboard-only. It is the primary human interface the earlier specs kept pointing at, and it adds **zero** engine capability: anything the TUI does, the CLI/MCP already can headless.

Design intent locked with the user: **keep the first cut simple and iterate.** The surface below is the starting set, not a ceiling.

## Decisions locked during design

| Decision     | Choice                                                                                                                                                 |
|--------------|--------------------------------------------------------------------------------------------------------------------------------------------------------|
| Launch       | Explicit `kazi tui`. Bare `kazi` stays help/usage (no TTY-magic).                                                                                      |
| Framework    | bubbletea + bubbles + lipgloss. Custom panels; no lazydocker-as-lib dependency.                                                                        |
| Liveness     | **TUI-side polling ticker** over the existing pull-only engine — no daemon, no engine watch API, no new wire types. Logs stream via `compose logs -f`. |
| Action scope | **Full parity, guarded.** Every engine op reachable; destructive/creational ops behind a confirm or input modal. Nothing lives only in this skin.      |
| Layout       | Sidebar + tabbed detail; two top-level modes (Stacks / Catalog); synthetic `ALL` overview; always-on status bar.                                       |
| Navigation   | **vim motions from day one** (`h/j/k/l`, `g/G`, `Ctrl-d/u`), arrows as aliases.                                                                        |
| Keybindings  | **Contextual** — the keybar and active bindings reflect the focused pane and the selected item's kind/state.                                           |

## Architecture

A fourth thin skin. `cmd/kazi/tui.go` wires the cobra command; all screen logic lives in a new `internal/tui/` package that calls **only** the `internal/engine` facade — same typed structs (`StackInfo`, `ServiceStatus`, `ContainerInfo`) the `--json` marshaller and MCP server return.

```
cmd/kazi/tui.go     cobra: `kazi tui` → construct model → tea.NewProgram(...).Run()
internal/tui/       bubbletea model/update/view; panels, modals, keymaps. No orchestration logic.
internal/engine/    unchanged. TUI reads via List/Status/Ps/Urls; writes via Up/Down/Restart/Try/Gc/Expose/...
```

- **No new engine surface.** The engine stays daemonless and pull-only; the TUI owns the clock (§Live updates). If a screen needs data the engine can't already return, that's an engine bug fixed for all skins — not a TUI-private path.
- Engine calls run off the UI goroutine as `tea.Cmd`s returning messages; the model never blocks on a subprocess.

## Layout

```
┌ kazi ─ runtime:docker ─ proxy:● ─ trust:✓ ─ gc:2 reclaimable ────────────── ┐  status bar (doctor-lite)
├─Stacks────────┬─ blog ▸ Services │ Logs │ URLs │ Config ─────────────────── ┤
│ ▸ ALL          │  web   running  healthy   →  https://blog.localhost        │
│ ▸ REGISTERED   │  db    running  healthy   →  localhost:42017               │
│   blog   ●     │                                                            │
│   api    ○     │                                                            │
│ ▸ DISCOVERED   │                                                            │
│   redis  ●     │                                                            │
│ ▸ UNMANAGED    │                                                            │
│   n8n    ●     │                                                            │
│ ▸ SYSTEM       │                                                            │
│   kazi-proxy ● │                                                            │
├────────────────┴────────────────────────────────────────────────────────────┤
│ u:up d:down r:restart l:logs t:try x:expose g:gc D:rm  Tab:catalog  ?:help  │  contextual keybar
└─────────────────────────────────────────────────────────────────────────────┘
```

- **Two top-level modes**, toggled by `Tab` (or `1`/`2`): **Stacks** (above) and **Catalog** (sidebar → template list; detail → values + description, actions `t:try  e:eject`).
- **Sidebar (left):** stacks grouped by kind — `REGISTERED / DISCOVERED / UNMANAGED / SYSTEM` — each with a health glyph (`●` up, `○` stopped, `◐` partial/unhealthy). A synthetic **`ALL`** item at the top is the default selection and renders the cross-kind global dashboard (the `kazi status` no-arg feel).
- **Detail (right):** tabs **Services · Logs · URLs · Config**. Config shows M3's effective-merged spec with per-field origin. `ALL` selected → the detail area is the global overview instead of tabs.
- **Status bar (top):** always-on `doctor`-lite glance — active runtime, proxy health, trust installed, gc-reclaimable count, port-range headroom. Computed from the same engine reads, refreshed on the tick.
- **Keybar (bottom):** contextual (§Navigation).

## Navigation & keybindings

Two rules govern the whole app, and they are the point of this milestone:

1. **vim motions everywhere, from day one.** Movement never depends on arrow keys (which are aliases, not the contract).
2. **Bindings are contextual.** The keybar shows *only* what's valid for the focused pane and the selected item; an action absent from the keybar is not bound in that context.

**Global (any context):**

| Key | Action |
|---|---|
| `q` / `Ctrl-c` | quit |
| `?` | help overlay (full keymap for the current context) |
| `Tab` / `1` `2` | switch top-level mode (Stacks ↔ Catalog) |
| `/` | filter the focused list |
| `Esc` | close modal / clear filter / defocus |

**Motion (focused list or logs pane):**

| Key | Action |
|---|---|
| `j` / `k` | down / up |
| `g` / `G` | top / bottom |
| `Ctrl-d` / `Ctrl-u` | half-page down / up |
| `h` / `l` | move focus sidebar ↔ detail |
| `[` / `]` | previous / next detail tab (when detail focused) |
| `Enter` | descend: focus detail on a stack; zoom the logs pane |

**Contextual action keys** — resolved per selection; shown live in the keybar, gated behind modals where noted:

| Context | Keys offered |
|---|---|
| Registered/discovered stack, running | `d:down` `r:restart` `l:logs` `x:expose` `D:rm`* |
| Registered/discovered stack, stopped | `u:up` `l:logs` `D:rm`* |
| Unmanaged container | `a:adopt`* (view only otherwise) |
| System stack (`kazi-proxy`) | `l:logs` `T:trust`* — never `rm` (protected) |
| Catalog template | `t:try`* `e:eject`* |
| Global (`ALL`) | `g:gc`* `T:trust`* |

\* Destructive or creational → confirm/input modal (§Actions). The same physical key means different things in different contexts by design; the keybar removes the ambiguity.

## Actions (full parity, guarded)

Every action dispatches the identical engine call the CLI verb does — same idempotency, same exit semantics surfaced as a toast.

- **Confirm modal** for irreversible ops: `rm`, `gc`, `down` of a system-adjacent stack → `rm blog? [y/N]`. `y`/`Enter` confirms, `n`/`Esc` cancels.
- **Input modal** for ops that need arguments: `try <template> [--set k=v]`, `expose <stack> <service> [--port]`, `adopt <name> <container…>`. Small form; `Esc` aborts writing nothing.
- **Async + toast:** the call runs as a `tea.Cmd`; a transient toast reports `ok`/error (structured error code + message from the engine). The next tick reflects the new state — no optimistic UI.
- `try` from the TUI is always detached (`-d`); the resulting ephemeral stack is reclaimable via `g:gc`, matching the M2/M3 agent contract (no foreground session inside the UI).

## Live updates

The TUI owns a refresh ticker (`tea.Tick`); the engine is untouched.

- Default interval **2s**, from Config `spec.tui.refreshInterval` (additive, optional). Each tick fires the engine reads the current view needs (`List` always; `Status`/`Urls` for the selected stack; status-bar signals) as `tea.Cmd`s; results arrive as messages and re-render.
- **Logs are the exception:** the Logs tab spawns a streaming `compose logs -f` (per-service when a service row is selected, else whole stack) whose lines arrive as messages; it is not polled. Leaving the tab / `Esc` tears the stream down.
- Manual `R` forces an immediate refresh. Polling pauses while a modal is open (no state yank mid-decision).

## Schema & surface deltas

- **New command:** `kazi tui` (no `--json`; it *is* the human skin). `--refresh <dur>` overrides the interval for one run.
- **Config:** optional `spec.tui.refreshInterval` (default `2s`). Nothing else; absent ⇒ default.
- **Engine:** no changes. New dependencies: bubbletea, bubbles, lipgloss.
- **No manifest schema change.** The TUI is pure read/act over existing `v1alpha1` resources.

## Error handling

- Engine/runtime errors surface as toasts carrying the structured error code + message; the UI stays usable (a failed `up` doesn't crash the dashboard).
- Proxy/trust degradation shows in the status bar (`proxy:✕`, `trust:—`) exactly as `doctor` would report it — the bar is the always-visible health surface.
- A tick whose engine read fails leaves the last good frame and flags staleness in the status bar; it never blanks the screen.
- **No-TTY / no runtime:** `kazi tui` on a non-terminal exits `2` with a message; no runtime available exits `4` (M0 codes), same as any other command.
- `q`/`Ctrl-c` always restores the terminal (alt-screen off, cursor shown) even mid-modal or mid-stream.

## Testing

- **Unit (teatest + fake engine):** the `internal/tui` model is driven with scripted messages against a fake engine; assert rendered frames (golden) for each mode/tab, the three stack kinds, and the `ALL` overview. Contextual-keymap table tests: for each `(pane, selection kind, state)` the offered bindings match the spec table — including that `rm` is absent on the system stack and action keys are absent on unmanaged rows.
- **Motion tests:** `h/j/k/l`, `g/G`, `Ctrl-d/u`, `[`/`]`, `Tab` produce the expected focus/selection transitions; arrows alias identically.
- **Action tests:** each guarded action opens the right modal, and confirm/cancel invokes/does-not-invoke the recorded engine call; `try` dispatches detached; toasts render engine errors.
- **Integration (docker, `integration` tag):** launch `kazi tui` against a fixture stack; drive `up → logs stream → down` and assert the ticker reflects state within one interval.

## Acceptance criteria

1. `kazi tui` opens a bubbletea dashboard showing every stack grouped registered/discovered/unmanaged/system, refreshing live without any engine daemon.
2. The full keyboard is vim-navigable from day one (`h/j/k/l`, `g/G`, `Ctrl-d/u`, `[`/`]`) with arrows as aliases; `?` shows the current context's keymap.
3. Keybindings are contextual: the keybar and active bindings change with the focused pane and the selected item's kind/state — `rm` never appears on `kazi-proxy`, action keys never appear on an unmanaged row.
4. Every engine op is reachable from the TUI; destructive/creational ones prompt via modal; each dispatches the same engine call as its CLI verb and reports the result.
5. The Logs tab streams `compose logs -f` (per-service when a service is selected) and tears the stream down on leave; the rest of the UI stays responsive during streaming.
6. Nothing in the TUI is capability the CLI/MCP lacks — the skin is a view/controller, not a new surface.
