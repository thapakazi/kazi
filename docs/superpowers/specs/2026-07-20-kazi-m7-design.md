# kazi M7 — Stats Design Spec

**Date:** 2026-07-20 · **Scope:** post-roadmap iteration, building on shipped M0–M3 ([M0](2026-07-17-kazi-m0-design.md) · [M1](2026-07-17-kazi-m1-design.md) · [M2](2026-07-17-kazi-m2-design.md) · [M3](2026-07-18-kazi-m3-design.md)), M5 ([TUI](2026-07-18-kazi-m5-design.md)), and M6 ([create & edit](2026-07-18-kazi-m6-design.md)) · **Status:** approved design, pre-implementation

M7 adds live resource stats at two altitudes: a per-service **Stats** tab in the detail pane (lazydocker-style CPU/mem sparklines, net/block I/O, PIDs) and host **CPU/Memory/Disk** graphs on the `ALL` overview (ONCE-style). Unlike M6, this genuinely grows the engine — but correctly: a new `Stats`/`HostStats` capability reused by a `kazi stats` CLI verb, MCP, and the TUI. Nothing lives only in a skin.

**Constraint that shapes everything:** kazi never touches the Docker API socket ([architecture rule](../../CLAUDE.md)). Container stats come only from `<runtime> stats`; host stats from a portable OS read. Proxy-derived metrics (Visits / req-rate / %errors) are **deferred** to a later spec — they need a Caddy metrics subsystem.

## Decisions locked during design

| Decision | Choice |
|---|---|
| Doc home | New milestone spec (M7), post-roadmap. |
| Altitudes | **Both**: per-service Stats detail tab + host CPU/Mem/Disk on the `ALL` overview. |
| Metric depth | **Resource stats now** — CPU% · mem (abs + %) · net I/O · block I/O · PIDs · host cpu/mem/disk. Proxy Visits/Traffic/%errors deferred. |
| Container source | `<runtime> stats --format json`. **Streamed while a live view is visible** (the second logs-style stream exception, per M5), scoped to the stack's container IDs. `--no-stream` one-shot for the CLI. Never the API socket. |
| Host source | Portable OS read via a cross-platform lib (gopsutil — darwin/linux, no cgo). The one new dependency. |
| History | TUI-side ring buffer, default 60 samples (`spec.tui.statsHistory`). Sparklines are rendered from it; the engine stays stateless. |
| CLI parity | New `kazi stats [stack] [--host] [--json]` verb + engine methods; TUI is a renderer over them. |

## Engine surface (new, shared)

Three methods on the facade, all reused across skins; the `Runtime` interface grows one command constructor.

```
Stats(ctx, stack) ([]ContainerStats, error)         one-shot: <runtime> stats --no-stream --format json <ids>
StatsStream(ctx, ids) (<-chan StatSample, error)     live:     <runtime> stats --format json <ids>   (parsed per line)
HostStats(ctx) (HostStats, error)                    host cpu/mem/disk via gopsutil
```

- **Types** (the `--json` wire shapes, same as every other engine struct): `ContainerStats{Service, Name, CPUPercent, MemUsage, MemLimit, MemPercent, NetRx, NetTx, BlockRead, BlockWrite, PIDs}`; `StatSample` is one streamed `ContainerStats` + a sequence index; `HostStats{CPUPercent, CPUCores, MemUsed, MemTotal, DiskUsed, DiskTotal}`.
- **Container-ID scoping:** the engine already lists a stack's containers (M0 discovery); `Stats`/`StatsStream` pass those IDs to `<runtime> stats`. No compose-level stats verb exists, so this is the container-level runtime command — still shelled, still runtime-agnostic (docker/podman/nerdctl all accept `stats --format json`; best-effort where they diverge).
- **Runtime interface:** add `StatsCmd(ids []string, stream bool) *exec.Cmd` alongside the existing ps/compose constructors. Additive.
- **Engine stays stateless & daemonless:** it emits samples; history/aggregation lives in the caller (TUI ring buffer). No background collector.

## `kazi stats` (CLI)

`kazi stats [stack] [--host] [--json]` — a scoped `docker stats` that respects kazi's grouping.

- No stack ⇒ every kazi-visible container grouped by stack; a stack name ⇒ just its services. `--host` ⇒ the `HostStats` line.
- **One-shot snapshot by default** (`--no-stream` under the hood) so it's pipe- and agent-safe; `--json` emits the versioned envelope (`kind: StatsList`). The continuous, animated view is the TUI's job — the CLI doesn't stream.
- Exit codes per the M0 contract; runtime lacking JSON stats ⇒ structured error, not a crash.

## TUI — per-service Stats tab

The detail tabs become **Services · Logs · URLs · Config · Stats**. Selecting Stats opens a `StatsStream` scoped to the selected stack's containers (per-service when a service row is focused, else all services).

```
─ blog ▸ Services │ Logs │ URLs │ Config │ Stats ──────────────────────────
  web    CPU  ▁▂▃▅▇▅▃▂  2.4%      Mem  ▂▂▃▃▃▃▃▃  128MiB / 512MiB (25%)
         net  ↓1.2MB ↑340KB      blk  r 45MB w 12MB      PIDs 14
  db     CPU  ▁▁▂▁▁▁▁▁  0.6%      Mem  ▅▅▅▅▅▅▅▅  201MiB / 512MiB (39%)
         net  ↓88KB  ↑12KB       blk  r 6.7GB w 0B       PIDs 20
```

- Sparklines drawn from the ring buffer (default 60 samples ≈ last ~2min at the runtime's ~1s cadence). CPU% and Mem% each get a sparkline; net/block I/O and PIDs are current values.
- **Stream lifecycle mirrors logs (M5):** the stream starts on entering the tab, tears down on leave/`Esc`; polling of the rest of the UI is unaffected. Only running containers stream; stopped/exited show `—`.
- `Enter`/`f` zooms the tab to fullscreen (bigger graphs), same motion contract as the logs pane.
- Rendering via a lightweight sparkline (ntcharts, or a small braille/block renderer in `internal/tui`) — kept internal, no heavy chart dep.

## TUI — host overview on `ALL`

Selecting the synthetic `ALL` item renders host graphs above the cross-kind dashboard, from `HostStats` polled on the normal tick (cheap, non-blocking — no stats stream at the host level):

```
┌ CPU (14 cores) ──────┐┌ Memory (36.0G) ─────┐┌ Disk ───────────────┐
│ ▁▂▅█▅▂▁ 312%          ││ ▃▃▄▄▄▄▄ 12.4G/36.0G ││ 89% used            │
│                       ││                      ││ 884G used, 110G free│
└───────────────────────┘└──────────────────────┘└─────────────────────┘
 aggregate containers: CPU 4.7%  Mem 1.8G across 11 stacks
```

- Host CPU/Mem/Disk from gopsutil; an aggregate line sums container CPU/mem across kazi-visible stacks (from the same snapshot the dashboard already pulls). Disk is the root filesystem by default.
- The host block is `ALL`-only; per-stack selection shows the Stats tab instead.

## Schema & surface deltas

- **New command:** `kazi stats [stack] [--host] [--json]`. New file `cmd/kazi/stats.go`, thin over `Stats`/`HostStats` (per the command-organization principle).
- **New TUI:** `Stats` detail tab (slots into M5's tab bar); host graphs on `ALL`. New binding surfaced contextually; no new global keys.
- **Engine:** `Stats`, `StatsStream`, `HostStats` + the `StatsCmd` runtime constructor + the three result types. First real engine growth since M3 — additive, shared by all skins.
- **Config:** optional `spec.tui.statsHistory` (default `60`). Reuses M5's `refreshInterval` for host-poll cadence.
- **Dependency:** gopsutil (host stats) + an internal sparkline renderer. **No manifest schema change.**
- **MCP:** `stats` becomes an additional read-only tool (annotated `readOnlyHint`), reusing the CLI's `StatsList` schema — free, since it's an engine method.

## Error handling

- **Runtime without JSON stats** (older/podman/nerdctl quirks): the Stats tab shows "stats unavailable on `<runtime>`" and the CLI returns a structured error; the rest of the UI is unaffected.
- **Stream death mid-view:** keep the last frame, flag staleness, retry on re-enter — same as the M5 tick-failure rule.
- **`--no-stream` latency:** a snapshot call blocks ~1s computing the CPU delta; it always runs as a `tea.Cmd` off the UI goroutine (TUI) or is simply the CLI's foreground cost. Never blocks the event loop.
- **Host read failure** on an exotic platform: degrade per-metric (e.g. hide Disk) rather than failing the whole overview; `doctor` (M3) can note it.
- **Stopped/exited containers:** excluded from the stream; shown as `—` in the tab, never as zero (zero is a real running value).

## Testing

- **Unit (fixtures, golden files):** parse `docker stats --format json` lines → `ContainerStats` (incl. the `MemUsage / MemLimit` and `NetRx / NetTx` split-string forms docker emits); ring-buffer retention/downsampling; sparkline renderer golden output; `HostStats` mapping from a fake host provider; `kazi stats --json` envelope shape.
- **TUI (teatest):** entering the Stats tab opens a stream scoped to the selected stack's container IDs and leaving tears it down; per-service vs whole-stack scoping; `ALL` renders host graphs from a fake `HostStats`; runtime-unavailable degradation.
- **Integration (docker, `integration` tag):** a CPU-busy fixture container shows non-zero CPU% in `kazi stats <stack> --json` and a rising sparkline in the TUI within a couple of samples; podman path best-effort (skipped if `stats --format json` unsupported).

## Acceptance criteria

1. The detail pane gains a **Stats** tab showing per-service CPU/mem sparklines plus net I/O, block I/O, and PIDs, streamed from `<runtime> stats` while visible and torn down on leave.
2. The `ALL` overview shows host CPU/Memory/Disk graphs plus an aggregate container-usage line.
3. `kazi stats [stack] [--host] [--json]` returns a kazi-grouped snapshot; the `--json` form is a versioned, agent-safe envelope (non-streaming).
4. No Docker API socket is used — container stats come only from `<runtime> stats`, host stats from a portable OS read; podman/nerdctl are best-effort and degrade cleanly when JSON stats are unsupported.
5. Stats are engine methods (`Stats`/`StatsStream`/`HostStats`) reused by CLI, MCP, and TUI — nothing lives only in the TUI; proxy Visits/Traffic/%errors are explicitly deferred to a later spec.
