# kazi M8 — Backup & Restore Design Spec

**Date:** 2026-07-20 · **Scope:** post-roadmap iteration (backup was originally deferred to M4 server-mode; pulled out as a local-first feature). Builds on shipped M0–M3 ([M0](2026-07-17-kazi-m0-design.md) · [M1](2026-07-17-kazi-m1-design.md) · [M2](2026-07-17-kazi-m2-design.md) · [M3](2026-07-18-kazi-m3-design.md)) · **Status:** approved design, pre-implementation

M8 backs up and restores a stack's real data so it survives a `down -v`, a bad migration, or a laptop swap. The design answers the core question — *volume vs. real data* — with **both**: a universal volume snapshot as the floor, plus opt-in per-service logical dumps for databases. Consistent by default (cold), local-first, reproducible via a per-backup manifest. M4 server-mode can later layer scheduling and remote destinations on the same engine methods.

## Decisions locked during design

| Decision | Choice |
|---|---|
| Approach | **Hybrid.** Volume snapshot is the universal default (works for any stack, no config); per-service **logical** backup/restore commands are opt-in in the manifest for databases. |
| Consistency | **Cold by default** — quiesce (`compose stop`) the stack for a consistent volume snapshot; `--hot` skips the pause. Logical dumps run hot (the dump tool provides consistency). |
| Doc home | New post-roadmap spec (M8), local-first. |
| No socket, no secrets | Volume tars via an ephemeral helper container; logical dumps via `<runtime> exec`. Creds come from the container's own env — kazi stores none, and never touches the API socket. |
| Restore default | **In-place**, preceded by an automatic safety backup; `--as <new>` clones into a fresh stack. |
| Artifact integrity | Write to temp → fsync → atomic rename; the `Backup` manifest is written **last** as the commit point. A half-written backup is never listed as valid. |

## Backup model — artifacts

A backup is a set of **artifacts**, each independently one of two kinds:

- **`volume:<name>`** — a named volume, tarred by a throwaway helper: `<runtime> run --rm -v <vol>:/data:ro -v <dest>:/backup alpine tar czf /backup/volume-<name>.tar.gz -C /data .`. Universal, workload-agnostic — this is what captures excalidraw's drawings, minio buckets, anything.
- **`logical:<service>`** — the service's own dump tool, run inside the running container and streamed to a file: `<runtime> exec <container> sh -c '<command>' > logical-<service>.dump`. Consistent and version-portable — the right tool for Postgres/MySQL/Mongo/Redis.

**Declaration (additive to Stack `v1alpha1`):**

```yaml
spec:
  backup:
    volumes: [drawings, uploads]      # default: every named volume the stack declares
    exclude: [cache]                  # optional
    services:
      db:
        command: pg_dumpall -U $POSTGRES_USER      # logical dump (hot, consistent)
        restore: psql -U $POSTGRES_USER            # fed the artifact on stdin
```

- **Zero-config default:** no `spec.backup` ⇒ back up **all named volumes**, cold. Excalidraw works immediately.
- Artifacts are the user's declared set — kazi doesn't guess that a `logical:db` makes the `pg_data` volume redundant; list what you want. (Typical DB stack: declare the logical dump, drop the data volume from `volumes`.)
- The logical `command`/`restore` are plain compose-interpolated strings; env (incl. passwords) resolves inside the container.

## Consistency & ordering (cold by default)

One `kazi backup` run:

1. **Pre-hook** (optional `spec.backup.hooks.pre`) — ONCE-style, runs on the host.
2. **Logical dumps first, while running** — each `logical:<service>` via `exec`, streamed to its artifact file.
3. **Quiesce for volumes** — unless `--hot`, `compose stop` the stack so volume tars are consistent (SIGTERM ⇒ graceful flush/exit; `pause` is insufficient for databases). `--hot` snapshots live with a recorded `method: hot` and a warning.
4. **Snapshot volumes** — helper container tars each `volume:<name>` to the destination.
5. **Resume** — `compose start` back to the prior state (only if we stopped). A restart failure is surfaced loudly; kazi never leaves a stack stopped silently.
6. **Post-hook** (optional) → **write `backup.yaml`** last (commit point).

## Storage & the Backup manifest

Backups land in `~/.local/share/kazi/backups/<stack>/<timestamp>/` (root overridable via `config.yaml` `spec.backup.dir`):

```
2026-07-20T10-00-00Z/
  backup.yaml            # kind: Backup — the record
  volume-drawings.tar.gz
  logical-db.dump
```

```yaml
apiVersion: kazi.dev/v1alpha1
kind: Backup
metadata: { stack: blog, timestamp: 2026-07-20T10:00:00Z }
spec:
  method: cold                       # cold | hot
  artifacts:
    - { kind: volume,  name: drawings, file: volume-drawings.tar.gz, bytes: …, sha256: … }
    - { kind: logical, service: db,    file: logical-db.dump,        bytes: …, sha256: …, command: "pg_dumpall …" }
  images:  { web: "caddy@sha256:…", db: "postgres@sha256:…" }   # for reproducible / version-checked restore
  engine:  "docker 27.1"
```

The manifest makes restore reproducible and lets kazi **version-check** before overwriting (image-digest drift ⇒ the physical-backup format may not load). Retention via `spec.backup.keep` (default `0` = keep all); `kazi backup rm` / gc honor it.

## Commands

| Command | Behavior |
|---|---|
| `kazi backup <stack> [--hot] [--to <dir>] [--json]` | Create a backup per the model above. Prints artifacts + location; `--json` returns the `Backup` record. |
| `kazi restore <stack> [<backup>] [--as <new>] [--force] [--json]` | Restore (default `<backup>` = latest). In-place unless `--as`. Auto safety-backup first (`--no-safety` opts out). |
| `kazi backups [stack] [--json]` | List backups with method/size/age (a `ls` for backups). |
| `kazi backup rm <stack> <backup> [--yes]` | Delete a backup; retention gc uses the same path. |

**Restore ordering (in-place):** safety-backup → **version check** (compare recorded image digests to current; mismatch ⇒ warn, require `--force`) → `compose down` (volumes emptied for `volume:` artifacts, wiped+untarred via helper) → `compose up` → run each `logical:<service>` `restore` command feeding its `.dump` on stdin → report. `--as <new>` writes a new Stack manifest pointing at fresh volumes and restores into it (clone; the original is untouched).

## Schema & surface deltas

- **Stack manifest:** `spec.backup` (`volumes`, `exclude`, `services.<n>.command|restore`, `hooks.pre|post`). Additive to `v1alpha1`.
- **Config:** `spec.backup.dir` (default `~/.local/share/kazi/backups`), `spec.backup.keep` (retention). `spec.backup.encrypt` **reserved** (artifacts are unencrypted local files in M8 — see error handling).
- **New resource:** `kind: Backup` (a persisted record, not stack status).
- **Engine:** `Backup`, `Restore`, `ListBackups` + a per-artifact planner. Runtime interface gains ephemeral-run and `exec` command constructors (additive alongside the M0 compose/ps constructors, reused by M2's `run`/`adopt`).
- **New commands:** `backup`, `restore`, `backups`, `backup rm` — thin files in `cmd/kazi/` per the command-organization principle; full M0 agent contract (`--json`, exit codes, `--yes`).
- **MCP:** `backup`/`restore` (destructive-hinted; restore also `--force`-gated), `list_backups` (read-only) — reuse the CLI schemas.
- **Ephemeral stacks (M2):** `try` stacks are disposable; `kazi backup` on one warns and requires `--force` (backing up throwaway data is usually a mistake).

## Error handling

- **Version-incompatible restore:** recorded vs. current image digest mismatch ⇒ warn (physical/on-disk format may not load, e.g. a Postgres major bump) and require `--force`. Logical restores are format-portable and only info-warn.
- **Logical restore failure:** abort, leave the safety backup intact; the stack is left in a named, reported state (not half-restored silently).
- **Quiesce/restart failure:** if `compose start` fails after a cold snapshot, surface loudly with the exact restart command; the backup artifact itself is still valid.
- **Nothing to back up:** no named volumes and no logical services ⇒ clear message, exit non-zero, no empty backup written.
- **Disk full / partial write:** temp-write + fsync + atomic rename; manifest is the last write, so an interrupted run leaves no listable backup.
- **Data sensitivity:** dump/tar artifacts hold real (possibly secret-bearing) data and are stored **unencrypted** locally in M8 — documented explicitly; at-rest encryption is deferred behind the reserved `spec.backup.encrypt`.

## Testing

- **Unit (fake runtime, golden files):** artifact-plan computation (defaults = all volumes; declared volumes/exclude/logical mix); `Backup` manifest golden; restore ordering state machine (safety → version-check → volumes → logical); version-mismatch gating; retention selection; atomic-commit (manifest-last) behavior on simulated interruption.
- **Integration (docker, `integration` tag):**
  - Volume path: an excalidraw-like files stack → `kazi backup` (cold) → wipe the volume → `kazi restore` → drawings present; assert the stack was stopped and restarted.
  - Logical path: Postgres with a declared `pg_dumpall`/`psql` pair → `--hot` backup → drop a table → restore → rows back; assert no downtime and no creds stored by kazi.
  - `restore --as clone` produces an independent stack with the data; original untouched.

## Acceptance criteria

1. `kazi backup <stack>` with zero config backs up every named volume cold (consistent); excalidraw drawings survive a volume wipe + restore.
2. Declaring a per-service `command`/`restore` backs up Postgres via `pg_dump` **hot** and restores into a fresh instance; kazi stores no credentials and never uses the Docker socket.
3. Restore is in-place by default and auto-takes a safety backup first; `--as <new>` clones the data into a new stack, leaving the source intact.
4. Every backup carries a reproducible `Backup` manifest (artifacts + sha256, image digests, engine, cold/hot); restoring across an incompatible image version warns and requires `--force`.
5. `backup`/`restore`/`backups` are engine methods reused by CLI and MCP; artifacts are atomic (manifest written last) so an interrupted backup is never listed as valid.
