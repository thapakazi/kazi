# kazi M5-Log — Logs Tab Design Spec

**Date:** 2026-07-18 · **Scope:** an extension of milestone [M5 (TUI)](2026-07-18-kazi-m5-design.md), deepening the **Logs** tab only · **Status:** approved design, pre-implementation · **Depends on:** the M5 Logs base (live `compose logs -f` streaming into a per-stack ring buffer via `engine.LogStream`) which is **already implemented**.

The M5 base gives the Logs tab a live tail: select a stack, open **Logs**, and lines from `<runtime> compose logs -f` stream into a 500-line ring buffer rendered clipped-to-pane. M5-Log turns that read-only tail into a usable log viewer: **tail sizing, in-pane search, time-window jumps, pattern grouping, and copy** — all keyboard-driven, all client-side over the engine's existing log stream. It adds **one** engine capability (a `--since`/tail-parameterised stream) and otherwise stays a pure skin.

Design intent (locked with the user): keep each feature a small, independent increment on the working tail; nothing here blocks the rest of M5 wave 2 (action modals).

## Decisions locked during design

| Decision | Choice |
|---|---|
| Where logic lives | Search / grouping / copy are **client-side** over the in-memory ring buffer — no new engine reads. Only the **time-window jump** needs the engine (a `--since` arg on the stream). |
| Follow model | The stream still follows by default; **`f`** toggles follow (pause freezes the view and keeps buffering; resume snaps to latest). No optimistic UI. |
| Time windows | Fixed ladder `1m · 5m · 10m · 30m · 1h · 2h · 5h · all`, cycled with `s` (or picked from a small menu). Implemented via `compose logs --since <dur>`; restarts the stream. |
| Search | Incremental, case-insensitive substring over the buffer; `n`/`N` walk matches; matches highlighted. `/` enters log-search when the Logs tab is focused (distinct from the sidebar filter). |
| Grouping | `p` toggles a **grouped** view: lines normalised (digits/UUIDs/timestamps templated to `#`) then bucketed to `count × pattern`, most-frequent first. Toggles back to raw. |
| Copy | `y` yanks the visible lines, `Y` the whole buffer, to the **system clipboard via OSC 52** (works over SSH; falls back to `pbcopy`/`wl-copy`/`xclip` when OSC 52 is unavailable). A toast confirms `copied N lines`. |
| Scrollback | The Logs pane becomes a scrollable viewport: `j/k`, `Ctrl-d/u`, `g/G` scroll within it when the detail pane is focused and the tab is Logs. |

## Layout

The Logs tab gains a one-line **log status/control strip** under the tab header, showing the active follow/tail/since/search/group state; the rest of the pane is the (scrollable) log viewport.

```
┌ … tabs … ─ Services │ Logs │ URLs │ Config ─────────────────────────────────┐
│ follow:● tail:500 since:5m  /err (3/12)  group:off                          │  log control strip
│ 12:01:04 web  | GET /health 200                                             │
│ 12:01:05 web  | GET / 200                                                   │  viewport (scrollable)
│ 12:01:06 db   | ERROR could not connect  ◀ match                            │
│ …                                                                            │
├──────────────────────────────────────────────────────────────────────────── ┤
│ f:follow t:tail s:since /:search n:next p:group y:copy Y:copy-all  Esc:back  │  contextual keybar (Logs)
└──────────────────────────────────────────────────────────────────────────── ┘
```

- The **control strip** is derived state, always visible while on Logs; fields collapse when at defaults (e.g. no `/err (…)` when not searching).
- **Match highlight:** searched substrings are reverse-video; the current match row is marked and auto-scrolled into view.

## Keybindings (Logs tab, when detail is focused)

These are contextual — they appear in the keybar only while the Logs tab is active, and they shadow the generic motion keys inside the viewport.

| Key | Action |
|---|---|
| `f` | toggle follow (pause / resume-to-latest) |
| `t` | cycle tail size: `100 · 500 · 1000 · all` (restarts stream) |
| `s` | cycle since-window: `1m · 5m · 10m · 30m · 1h · 2h · 5h · all` (restarts stream) |
| `/` | enter log search; type to filter incrementally, `Enter` locks it |
| `n` / `N` | jump to next / previous match |
| `p` | toggle pattern-grouping view |
| `y` / `Y` | copy visible / entire buffer to clipboard |
| `j`/`k` `Ctrl-d`/`u` `g`/`G` | scroll the viewport (pauses follow while scrolled up) |
| `Esc` | clear search → else exit grouping → else defocus back to sidebar |

`s`/`t` restart the underlying stream (they change the `compose logs` invocation); everything else operates on the buffer already held.

## Architecture & engine delta

- **New engine surface (one method, additive):** extend the stream to accept options —
  `LogStream(ctx, name, service string, opts LogStreamOpts) (io.ReadCloser, CancelFunc, error)`
  where `LogStreamOpts{ Tail string; Since string }` maps to `compose logs --tail <n> [--since <dur>]`. The M5 base call becomes `opts{Tail:"200"}`. This stays orchestration-in-the-engine; the TUI never builds a compose command.
- **Everything else is TUI-side.** The model gains: `logFollow bool`, `logTail`, `logSince`, `logSearch string`, `logMatches []int`, `logMatchCur int`, `logGrouped bool`, and a `viewport`-style scroll offset. Search/group/copy are pure functions over `logLines`; changing `tail`/`since` calls `stopLogStream` + `startLogStreamCmd` with new opts (the existing teardown/restart path).
- **Clipboard:** a small `internal/tui/clipboard.go` helper emits OSC 52 (base64) to the tea output, with a shell-tool fallback. No new module dependency for the OSC 52 path.
- Grouping normaliser lives in `internal/tui/loggroup.go` (pure, table-tested): regex-template digits, hex/UUID runs, ISO timestamps → `#`, then count identical templates.

## Testing

- **Reducer tests (fake engine):** `f` toggles follow and freezes appends into the view while the buffer still grows; `t`/`s` re-issue `startLogStreamCmd` with the expected `LogStreamOpts`; `Esc` unwinds search → group → defocus in order.
- **Search tests:** buffer with known lines → `/substr` sets the match set and count; `n`/`N` cycle and wrap; highlight offsets are correct; case-insensitive.
- **Grouping tests:** the normaliser collapses `GET /x/12 200` and `GET /x/97 200` into one bucket with count 2; timestamps/UUIDs template out; ordering is by descending count.
- **Copy tests:** `y` produces the OSC 52 sequence for the visible slice; `Y` for the whole buffer; the toast reports the right line count. (Assert the emitted sequence, not the actual system clipboard.)
- **Scroll tests:** scrolling up pauses follow; `G` resumes and snaps to latest.
- **Integration (docker, `integration` tag):** against a fixture that emits predictable lines, assert `since:1m` yields fewer lines than `all`, and search finds a known marker.

## Acceptance criteria

1. On the Logs tab, `f` pauses/resumes follow; while paused the view is stable and resuming snaps to the latest buffered line.
2. `t` and `s` change the tail size and time window by restarting the engine stream with `--tail`/`--since`, and the control strip reflects the active values.
3. `/` searches the buffer incrementally, case-insensitive; `n`/`N` walk highlighted matches with wrap; the count shows in the control strip.
4. `p` shows a `count × pattern` grouped view with digits/UUIDs/timestamps templated out, ordered by frequency; toggling returns to the raw tail.
5. `y`/`Y` copy the visible / whole buffer to the system clipboard (OSC 52, with a shell fallback) and confirm with a toast.
6. Nothing in the Logs tab is capability the CLI lacks: tail/since map to `compose logs` flags; search/group/copy are views over the same streamed bytes.
```
