# kazi M6 — Interactive Create & Edit Design Spec

**Date:** 2026-07-18 · **Scope:** first **post-roadmap** iteration, building on shipped M0–M3 ([M0](2026-07-17-kazi-m0-design.md) · [M1](2026-07-17-kazi-m1-design.md) · [M2](2026-07-17-kazi-m2-design.md) · [M3](2026-07-18-kazi-m3-design.md)) and M5 ([spec](2026-07-18-kazi-m5-design.md), implemented). M4 (server mode) remains independent. · **Status:** approved design, pre-implementation

M5 shipped the TUI as a view/controller over existing verbs. M6 is the promised iteration: make the two creation-and-edit flows first-class *from the keyboard* — register a stack, tweak its config in `$EDITOR`, and launch an ephemeral `try` with real values — while adding almost **no** engine surface. Only `kazi edit` is a genuinely new verb; create and try reuse `Add`/`Try`/`Keep`/`Gc` unchanged.

Consistent with the [command-organization principle](../../CLAUDE.md): each new/changed CLI verb is a thin file in `cmd/kazi/` over an engine method used identically by CLI and TUI. Nothing added here lives only in a skin.

## Decisions locked during design

| Decision | Choice |
|---|---|
| Doc home | New milestone spec (M6), not an M5 amendment — keeps the TUI spec stable, gives these flows their own decisions table. |
| Create | **TUI form over the existing `kazi add`** (name + compose path). No new source-picking engine path; template/image creation stay `try`/`run` as today. |
| Edit | New `kazi edit <stack>` covering **two targets**: the kazi manifest (default) and the underlying **compose file** (`--compose`, compose-backed stacks only). Validate-on-save, re-edit-or-abort on invalid. TUI `e` invokes the same. |
| Try | **Richer TUI try**: values form (`--set`) before launch, then watch the ephemeral stack live with `k:keep` / `g:gc`. Reuses M2 `try -d`/`keep`/`gc`. |
| Editor launch | Skin concern: CLI execs `$EDITOR`; TUI suspends via `tea.ExecProcess`. Path resolution + validation live in the engine (reused by both). `$EDITOR` → `$VISUAL` → `vi`; `--editor` overrides. |

## Create — register a stack from the TUI

The CLI already has the headless path (`kazi add <name> <path>`, M0); M6 adds a keyboard front door and nothing more.

- **TUI `n` (new)** in Stacks mode opens a small form: `name` (DNS-label validated per the M1 amendment) and `path` (compose file or a directory searched for `compose.y(a)ml` / `docker-compose.y(a)ml`, same resolution as `add`).
- Submit → engine `Add` → the new registered stack appears in the sidebar on the next tick, selected. `Esc` aborts, writing nothing.
- Duplicate name / bad path surface inline in the form (engine error code + message), not as a dismissable toast — the user is mid-edit.
- **No new engine surface.** This is `kazi add` with a form in front of it. Template- and image-backed creation remain `try` and `run`; the form deliberately doesn't grow a source picker (kept simple per M5's iterate-later intent).

## Edit — `kazi edit <stack>`

New verb; new file `cmd/kazi/edit.go` (thin: resolve target → launch `$EDITOR` → validate → report). Two targets, both **user-owned files** — kazi never opens a shared template from here.

| Invocation | Opens | Validated with |
|---|---|---|
| `kazi edit <stack>` | the kazi manifest `~/.config/kazi/stacks/<name>.yaml` | YAML parse + `v1alpha1` schema check (known kind/fields) |
| `kazi edit <stack> --compose` | the stack's compose file (`spec.source.compose`) | `<runtime> compose config` |

- **Edit-validate-recover loop** (the M2 `template new` pattern, reused): open `$EDITOR` on the real file → on save, validate → **invalid** ⇒ show the error and offer *re-edit* (reopen the same buffer) or *abort* (restore the original, write nothing) → **valid** ⇒ keep. Comments and formatting survive because the user edits the file directly (no node-API rewrite).
- **`--compose` is compose-backed only.** For `template:` / `image:` / `containers:` stacks it errors clearly: there is no user-owned compose file — edit per-stack values via the manifest (`spec.values`), or the template via `kazi template`. Deterministic, no guessing.
- **Running-stack notice:** if the edited target affects a running stack, print the M3-style "restart to take effect" notice and offer `kazi restart <stack>` (`--restart` / TUI prompt to do it now). kazi never silently recreates on edit.
- **TUI `e`:** select a stack → `e`. Compose-backed ⇒ a two-item picker (`manifest` / `compose`); otherwise open the manifest directly. The TUI suspends to `$EDITOR` via `tea.ExecProcess`, then runs the same validation and reports via toast; invalid ⇒ reopen or discard. Polling is paused while suspended.

**Engine additions (minimal):** `EditTargets(stack) []EditTarget` (each: path + kind + validator) and the two validators (schema for manifest; `compose config` already exists for compose). The `$EDITOR` process launch stays in the skins — that's plumbing, not logic.

## Try — richer ephemeral launch from the TUI

M5's catalog `t:try` launches with template defaults only. M6 puts values in the user's hands and keeps the watch loop on one screen. Still `try -d` under the hood — the engine flow is M2's, unchanged.

- **Catalog mode → select template → `t`** opens a values form pre-populated from the template's `values.yaml` keys (with the M2 scaffolder's secret-looking placeholders, e.g. `*_PASSWORD`, flagged **must-change** and blocking submit until set).
- Submit → `try -d --set k=v …` (the form composes the exact `--set` flags the CLI takes; nothing new). The view switches to Stacks mode focused on the new ephemeral stack.
- **Watch + decide, one key each:** the ephemeral stack streams status/logs live (M5 machinery); `k:keep` promotes it (M2 `keep` → flips `spec.ephemeral`, containers untouched), `g:gc` reclaims it (M2 `gc`). Both guarded per M5's modal rule.
- CLI parity is already there: `kazi try <template> --set k=v -d` (M2). The form is a skin over it — no engine change.

## Schema & surface deltas

- **New command:** `kazi edit <stack> [--compose] [--editor <bin>] [--restart]`. Follows the M0 agent contract for exit codes; no `--json` (it's an interactive editor launch — non-TTY ⇒ exit `2`).
- **New TUI bindings** (slot into M5's contextual keybar): `n:new` (Stacks mode), `e:edit` (a stack selected), a values form on `t:try` (Catalog mode), and `k:keep` on a watched ephemeral stack. All contextual, all shown in the keybar per M5's rule.
- **Engine:** `EditTargets` + two validators only. `Add`, `Try`, `Keep`, `Gc` unchanged. No new wire types.
- **Manifest/Config schema:** **no change.** M6 is pure create/edit ergonomics over `v1alpha1`.

## Error handling

- **Create form:** name collision / DNS-label violation / unresolvable path ⇒ inline field errors (engine codes), form stays open.
- **Edit:** invalid YAML/schema or failed `compose config` ⇒ re-edit-or-abort; abort restores the original byte-for-byte. Missing file (manifest deleted out from under us) ⇒ actionable message, same as M0's dangling-path case. `$EDITOR` unset and no `vi` ⇒ clear error naming `--editor`.
- **`--compose` on a non-compose stack:** explicit rejection with the right alternative (manifest values or `kazi template`).
- **Try form:** unmet must-change placeholders block submit with the offending keys named; launch failures surface as toasts and leave a gc-recoverable stack (M2 invariant holds).
- **TUI suspend:** `$EDITOR` exiting non-zero ⇒ treat as abort (no write); the TUI always restores the alt-screen/cursor on return, even if the editor crashed.

## Testing

- **Unit (fake engine/runtime, golden files):** `EditTargets` resolution per source kind (compose vs template/image/containers rejection); edit-validate-recover state machine (valid → keep, invalid → re-edit, invalid → abort restores original); manifest schema validator and `compose config` validator wiring; create-form validation (name/path) mapping to engine `Add`.
- **TUI (teatest):** `n` form → `Add` invoked with the typed name/path; `e` picker appears only for compose-backed stacks and opens the right target; `t` values form composes the exact `--set` flags and blocks on must-change keys; `k`/`g` on a watched ephemeral dispatch `keep`/`gc` behind the confirm modal.
- **Integration (docker, `integration` tag):** `kazi edit blog --compose`, edit a valid change, save, `--restart` applies it; an invalid edit aborts with the file unchanged; TUI `t` on `postgres` with a set password launches, `k:keep` leaves a persistent stack after quit.

## Acceptance criteria

1. From the TUI, `n` registers a new compose-backed stack (name + path) via the existing `kazi add` engine path; it appears selected on the next tick. Bad name/path show inline, nothing is written on abort.
2. `kazi edit <stack>` opens the manifest in `$EDITOR`; `--compose` opens the compose file (and is rejected with guidance on non-compose stacks); an invalid save offers re-edit or abort, and abort leaves the file byte-for-byte unchanged.
3. Editing a running stack surfaces the "restart to take effect" notice and can restart on request; kazi never silently recreates.
4. TUI `e` opens the same edit flow (picker for compose-backed, direct for others), suspending to `$EDITOR` and validating on return.
5. TUI `t` on a catalog template collects values (must-change placeholders enforced), launches an ephemeral `try -d`, and offers `k:keep` / `g:gc` on the live stack — all reusing M2's engine flow.
6. No manifest/Config schema change and no new engine capability beyond `kazi edit`'s path-resolution + validation; every M6 action is reachable headless via an existing or the one new CLI verb.
