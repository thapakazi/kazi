# kazi M8 — Backup & Restore Implementation Plan

**Date:** 2026-07-20 · **Tracks:** [M8 design spec](2026-07-20-kazi-m8-design.md) · **Status:** ready to implement (TDD)

This plan turns the approved M8 design into ordered, test-first work. It follows the
repo's rules: all logic in the engine facade + a dedicated `internal/backup` package;
cobra handlers stay thin; orchestration shells out to the runtime CLI, never the socket.

## Extension to the approved spec: `logical` gains a `driver`

The design locks two artifact kinds — `volume:<name>` and `logical:<service>`. We keep
that vocabulary and add a **driver** to logical artifacts so a service can be dumped
either by an in-container tool *or* by an HTTP endpoint it exposes:

- **`exec` driver** (spec's original case): `command` runs via `<runtime> exec`; `restore`
  is fed the artifact on stdin. Consistency comes from the dump tool (`pg_dumpall`).
- **`http` driver** (new): `GET /backup` pulls the artifact body; `POST /restore` pushes
  it back. Reached **in-container first** (`exec <c> curl …`); if curl/wget is absent, a
  fallback ephemeral helper shares the container's netns
  (`run --rm --network container:<id> curlimages/curl …`) so `localhost:<port>` still resolves.

Manifest (additive to Stack `v1alpha1`):

```yaml
spec:
  backup:
    volumes: [drawings, uploads]     # default: every named volume the stack declares
    exclude: [cache]
    hooks: { pre: "...", post: "..." }
    services:
      db:                            # exec driver
        command: pg_dumpall -U $POSTGRES_USER
        restore: psql -U $POSTGRES_USER
      search:                        # http driver
        http:
          port: 8080                 # container port
          backup:  GET /backup       # method + path → artifact body
          restore: POST /restore     # method + path ← artifact body (optional)
```

A service sets **exactly one** of `command`/`restore` or `http`. In the `Backup` record each
logical artifact carries `driver: exec|http`; restore dispatches on it.

## Runtime interface: reuse, don't grow

The existing `Runtime.Cmd(ctx, args...)` already builds arbitrary `<bin> <args…>`, so
volume-tar helpers (`run --rm -v …`), exec dumps (`exec …`), and the http helper all
compose from it. **No new interface methods** — `internal/backup` builds these argv slices
and runs them through `compose.Output`/`compose.Run`. This keeps `fake.go` untouched for
the command-construction path; the fake gains only recorded-output support for exec/volume
listing (below).

Two read-only runtime queries the drivers need (built from `Cmd`, no interface change):
- **Named volumes for a project:** `<rt> volume ls -q --filter label=com.docker.compose.project=<project>`
  → real volume names (avoids `<project>_<vol>` mangling guesswork).
- **Container id/image per service:** already available via `Ps` + compose labels
  (`com.docker.compose.service`); image digest via `<rt> inspect --format '{{index .RepoDigests 0}}'`.

## Package layout

```
internal/backup/
  record.go     # Backup manifest type (kind: Backup), YAML (un)marshal
  store.go      # storage layout, atomic write (temp→fsync→rename), list, rm, retention gc
  plan.go       # artifact planner: manifest+config → []Artifact (volume/exec/http)
  driver.go     # VolumeTar / ExecDump / HTTPDump backup+restore argv builders
internal/engine/
  backup.go     # Backup(), ListBackups(), BackupRm() orchestration
  restore.go    # Restore() ordering state machine
cmd/kazi/
  backup.go     # `backup`, `backups`, `backup rm`
  restore.go    # `restore`
```

`internal/backup` holds no engine deps (pure data + argv builders + fs); the engine wires
runtime + config into it. Users uninterested in backup don't pull engine weight into it.

## Types (Phase 0)

`store` additions (additive `v1alpha1`, `KnownFields(true)` still passes):

```go
type Spec struct {
    ...
    Backup *BackupSpec `yaml:"backup,omitempty"`
}
type BackupSpec struct {
    Volumes  []string                  `yaml:"volumes,omitempty"`
    Exclude  []string                  `yaml:"exclude,omitempty"`
    Hooks    *BackupHooks              `yaml:"hooks,omitempty"`
    Services map[string]BackupService  `yaml:"services,omitempty"`
}
type BackupService struct {
    Command string           `yaml:"command,omitempty"` // exec driver
    Restore string           `yaml:"restore,omitempty"`
    HTTP    *BackupHTTP      `yaml:"http,omitempty"`    // http driver (mutually exclusive)
}
type BackupHTTP struct {
    Port    int    `yaml:"port"`
    Backup  string `yaml:"backup"`            // "GET /backup"
    Restore string `yaml:"restore,omitempty"` // "POST /restore"
}
type BackupHooks struct{ Pre, Post string `yaml:",omitempty"` }
```

`ConfigSpec.Backup` (`dir` default `~/.local/share/kazi/backups`, `keep` retention int,
`encrypt` reserved). Validation: a `BackupService` with both `Command` and `HTTP` set is a
manifest error.

`backup.Backup` record (`kind: Backup`) exactly per the design's `backup.yaml`, with each
artifact gaining `Driver string` when `Kind == "logical"`.

## Sequencing: backup first, restore second

We ship **backup end-to-end** (Phases 0–4 + the backup slice of CLI/MCP/integration)
before starting restore. Restore (Phase 5 + its CLI/MCP/integration) builds on the exact
same `internal/backup` record, store, and drivers, so deferring it costs nothing — the
backup work is the foundation restore replays. Phases below are grouped accordingly.

### Batch A — backup (do now)

Each phase = failing unit test (fake runtime + golden files) → implement → green.

**Phase 0 — schema & record types.** `store` BackupSpec + config; `backup.Backup`
(un)marshal golden. Mutual-exclusion validation test.

**Phase 1 — planner (`backup/plan.go`).** manifest+config → `[]Artifact`. Tests:
zero-config default = all named volumes; declared `volumes`/`exclude`; exec-logical only
(volume dropped); http-logical; mixed. Golden artifact lists. Pure function, no runtime.

**Phase 2 — backup drivers (`backup/driver.go`).** ⚠️ Depends on
[M9](2026-07-20-kazi-m9-implementation-plan.md): the exec + http-fallback drivers reuse
M9's `ResolveContainer` + exec `*exec.Cmd` builder, wiring `cmd.Stdout` to the artifact file
(stream, don't capture in memory). argv builders for the capture path (restore counterparts
land in Batch B, same file):
- `VolumeTar`: `run --rm -v <vol>:/data:ro -v <dst>:/backup alpine tar czf …`.
- `ExecDump`: `exec <c> sh -c '<command>'` → file.
- `HTTPDump`: exec-curl primary, `run --network container:<id> curlimages/curl` fallback;
  a `curlProbe` helper decides primary vs fallback (tested with a fake that reports curl
  present/absent). Unit-test argv strings against goldens.

**Phase 3 — atomic store (`backup/store.go`).** temp-write + fsync + atomic rename;
`backup.yaml` written **last** (commit point). Tests: interrupted run (manifest absent) is
not listed; list ordering by timestamp; `rm`; retention `keep` selection.

**Phase 4 — `engine.Backup`.** Ordering state machine per design §Consistency: pre-hook →
logical dumps (running) → quiesce (`compose stop` unless `--hot`) → volume snapshots →
resume (`compose start`, loud on failure) → post-hook → write manifest. Record image
digests + engine version. Ephemeral-stack guard (require `--force`). "Nothing to back up"
⇒ non-zero, no empty dir. Fake-runtime state-machine tests assert call order and that a
resume failure surfaces the exact restart command.

**Phase 4b — backup CLI + MCP.** Thin `cmd/kazi/backup.go`: `backup`/`backups`/`backup rm`,
full M0 agent contract (`--json`, exit codes, `--yes`). MCP `backup` (destructive-hinted)
and `list_backups` (read-only), reusing CLI schemas.

**Phase 4c — backup integration (`integration` tag, docker).**
- Volume: excalidraw-like stack → cold `backup` → assert stack stopped+restarted, artifact
  + `backup.yaml` present with sha256.
- exec logical: Postgres `pg_dumpall` → `--hot` backup → assert no downtime, dump non-empty.
- http logical: a service exposing `GET /backup` → backup captures body; assert netns-fallback
  path taken when the image lacks curl.

At the end of Batch A, `kazi backup` is fully shippable on its own: backups are created,
listed, removed, and retained, with reproducible manifests — restore just isn't wired yet.

### Batch B — restore (do after Batch A)

**Phase 5 — restore drivers + `engine.Restore`.** Add restore argv counterparts to
`backup/driver.go` (`VolumeTar` wipe+untar; `ExecDump` `exec -i <c> sh -c '<restore>'` < file;
`HTTPDump` POST `--data-binary @-`). In-place ordering: safety-backup (`--no-safety` opts out)
→ version check (digest drift ⇒ warn + require `--force`; logical is format-portable, info
only) → `compose down` → volume wipe+untar via helper → `compose up` → per-service logical
`restore` (exec stdin or http POST) → report. `--as <new>` writes a fresh Stack manifest at
new volumes and restores into it; source untouched. Tests: state machine, version-mismatch
gating, logical-restore-failure abort leaves safety intact + named state.

**Phase 6 — restore CLI + MCP.** `cmd/kazi/restore.go` (`--as`, `--force`, `--no-safety`,
`--json`). MCP `restore` (destructive-hinted, `--force`-gated).

**Phase 7 — restore integration (`integration` tag, docker).**
- Volume round-trip: backup → wipe volume → `restore` → drawings back.
- exec logical: drop a table → restore → rows back.
- http logical: `POST /restore` round-trip.
- `restore --as clone` → independent stack; original untouched.

## Acceptance mapping

Batch A satisfies the backup halves of the design's acceptance criteria — zero-config cold
volume backup (AC1), exec+http logical hot dumps (AC2), reproducible `Backup` manifest with
sha256/digests/method (AC4/5 capture side), atomic manifest-last (AC5). The restore halves —
in-place default + safety + `--as` (AC3), version-gated `--force` restore (AC4) — are Batch B.
The http driver is the plan's addition beyond AC2's exec case (Phase 4c capture; Phase 7 round-trip).

## Open questions to resolve during Phase 0

1. **Helper image pin:** `alpine` for tar, `curlimages/curl` for the http fallback — pin by
   digest in config (`spec.backup.helperImages`) or hardcode? Lean: hardcode with a config override.
2. **Restore `--as` volume naming:** new project `kazi-<new>` ⇒ volumes `kazi-<new>_<vol>`;
   confirm no clash with an existing stack of that name (reject if taken).
