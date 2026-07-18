# kazi M2 — Catalog & Lifecycle Design Spec

**Date:** 2026-07-17 · **Scope:** milestone M2 of [the roadmap](../../roadmap.md), building on the implemented M0 ([spec](2026-07-17-kazi-m0-design.md)) and M1 ([spec](2026-07-17-kazi-m1-design.md)) · **Status:** approved design, pre-implementation

M2 delivers the catalog and ephemeral lifecycle: `kazi try postgres` with zero residue after exit, ad-hoc containers (`run`), adoption of hand-run containers (`adopt`), garbage collection (`gc`), template graduation (`eject`), and a template scaffolder that turns any image ref into an editable template.

## Decisions locked during design

| Decision | Choice |
|---|---|
| Catalog | Embedded starters (`go:embed`) materialized to `~/.config/kazi/templates/` on first use + `kazi template import <git-url\|dir>`. Offline-first; awesome-compose works as an import source. |
| Scaffolder | `kazi template new <name> --from-image <ref>` — works with any registry (Docker Hub, ghcr.io, private; auth inherited via the runtime), derives the template from the OCI image config, opens `$EDITOR`. |
| try lifecycle | Foreground session by default (Ctrl-C tears down); `-d` detaches for agents, reclaimed by `gc`. |
| Ephemerality | Lives in the **manifest** (`spec.ephemeral`), because container labels are immutable — `--keep`/`kazi keep` edits the manifest and never recreates containers. The `kazi.ephemeral` container label is written at creation only as a crash hint for `gc`. |
| Adoption | Pure manifest-membership (`spec.source.containers`); adopted containers are never recreated or relabeled. |

## Templates & catalog

### Template model

A template is a directory — no templating language (roadmap-locked):

```
~/.config/kazi/templates/postgres/
  compose.yml     # plain compose; ${VAR:-default} interpolation only
  values.yaml     # defaults + a description: key for catalog listings
```

Effective values = template `values.yaml` ← `--set key=val` (M3 inserts Stack `spec.values` and `-f overrides.yaml` into the middle of this chain). Values are flattened to env vars for compose interpolation.

### Sources

- **Embedded starters:** postgres, redis, mysql, mongo, mailpit, minio. Materialized on first use; user-editable afterward. kazi never overwrites an existing template dir; `kazi template reset <name>` restores pristine.
- **Import:** `kazi template import <git-url|dir> [name]` — shallow clone (or copy), take the subdirectory, validate a compose file exists, land in the catalog. Name collisions are an error suggesting an explicit name — never overwrite.
- `kazi template ls` lists the catalog with descriptions.

### Scaffold from an image

`kazi template new pg19 --from-image postgres:19` (any registry ref):

1. `<runtime> pull` then `<runtime> image inspect` — registry auth inherited from the user's existing docker/podman credentials; kazi stores no secrets.
2. Derive from the OCI image config: `ExposedPorts` → `ports` + HTTP/TCP classification; `Volumes` → named volumes; `Env` → `values.yaml` defaults, with secret-looking vars (`*_PASSWORD`, `*_KEY`, …) given placeholder-must-change values.
3. Write the template: required fields filled; optional knobs present but commented — `deploy.resources.limits` (cpus, memory), extra storage mounts, healthcheck stub, `restart:` policy.
4. Open `$EDITOR` on `compose.yml`; on save, validate with `compose config`. Invalid → offer re-edit or abort. Aborting/empty save writes nothing. Valid → immediately `kazi try`-able.

## Lifecycle verbs

### `kazi try <template> [--keep] [-d] [--set k=v]`

Foreground: resolve template → up as ephemeral stack → print `kazi urls` output → stream logs. On Ctrl-C/exit: `down -v --rmi local`, free port allocations, delete manifest, proxy reload. Zero residue. Second Ctrl-C abandons immediately (gc reclaims). `-d`: detach, leaving the ephemeral stack to `gc`/`rm`. `--keep` at launch — or `kazi keep <stack>` later — flips `spec.ephemeral` to false in the manifest; containers untouched.

### `kazi run <image> [--name n] [-p|-e|-v ...]`

Creates a registered, persistent, image-backed stack (`spec.source.image`) and starts the container with `kazi.*` labels via the runtime — no compose file generated. Verbs map per-source: `up`=create/start, `down`=stop, `restart`=restart. Routing is identical to compose stacks: classification reads the image's exposed ports; the container joins the `kazi` network with its `<service>.<stack>` alias → `https://<stack>.localhost`.

### `kazi adopt <name> <container>...`

Manifest-membership only: writes `spec.source.containers: [<names>]`; running containers are never recreated or relabeled (labels are immutable — hard constraint, not a choice). start/stop/logs/status work by container name. `network connect --alias` works on running containers, so adopted HTTP services still get hostnames. `kazi rm` on an adopted stack removes only the manifest. Adopting a compose-labeled container is rejected with a pointer to its discovered stack.

### `kazi gc [--dry-run] [--yes]`

Sweeps in order: (1) stopped or TTL-expired ephemeral stacks — full teardown including volumes and locally-pulled images; (2) orphaned containers wearing the `kazi.ephemeral` crash-hint label with no manifest; (3) port allocations pointing at nonexistent stacks. TTL from `config.yaml` `spec.cleanup.ephemeralTTL` (default `24h`). Manual-only (no daemon); prints the reclaim list and confirms unless `--yes`; `--dry-run` only prints. `kazi status` counts reclaimable debris as a nudge.

**Invariant: nothing kazi-ephemeral is ever unreachable by gc.** Every ephemeral artifact carries either a manifest or the crash-hint label; partial teardown failures always leave one of the two.

### `kazi eject <template> [dir] [--add]`

Copies `compose.yml` (interpolation intact) to `dir` (default `./<template>/`), writes current values as `.env`, prints the `kazi add` command (`--add` runs it). The ejected stack has no remaining link to the template.

## Schema & surface deltas

- **Stack manifest:** `spec.source` arms `image:` and `containers:` become real; try-stacks use `template:`. New: `spec.ephemeral`, `spec.values` (reproduces `--set` overrides on later `up`s). All additive to `v1alpha1`.
- **Config:** `spec.cleanup.ephemeralTTL` (default `24h`).
- **Engine:** lifecycle verbs dispatch through a per-source strategy (`compose` | `image` | `containers`) — the M0 forward-compat promise cashed in. `ls/status/urls/ps` are unchanged.
- **New commands:** `try`, `run`, `adopt`, `gc`, `keep`, `eject`, `template ls|new|import|reset`. All follow the M0 agent contract (`--json`, exit codes, `--yes`).

## Error handling

- **try teardown:** runs on SIGINT/SIGTERM; second signal abandons (gc-recoverable). Partial teardown failure leaves gc-recoverable debris — see invariant.
- **template new:** pull errors stream from the runtime; editor abort writes nothing; post-edit validation failure → re-edit or abort.
- **template import:** no compose file → clear error; collision → suggest a name.
- **run/adopt:** stack-name collisions rejected (M0 DNS-label rule applies); adopt-of-compose-container redirected to the discovered stack.
- **gc:** confirm-by-default; `--yes` for automation; `--json` reclaim report.

## Testing

- **Unit (fake runtime, golden files):** per-source strategy dispatch; try session state machine (signal → teardown → abandon); gc selection table tests over the manifest × label × running × TTL matrix; scaffolder output from fixture OCI image-config JSON (incl. secret-var placeholder rule); values flattening; eject golden output.
- **Integration (docker, `integration` tag):**
  - `kazi try postgres` → Ctrl-C → assert zero residue: no containers, volumes, locally-pulled images, allocations, manifest, or Caddyfile routes.
  - `try -d` then `gc` reclaim; simulated crash (delete manifest, leave containers) then `gc` reclaim via crash-hint label.
  - `run` nginx → `https://…localhost` responds; adopt round-trip on a hand-run container.

## Acceptance criteria

1. `kazi try postgres` works offline out of the box, prints urls, and leaves zero residue after Ctrl-C (roadmap headline).
2. `kazi try postgres -d` plus a crashed parent process is fully reclaimed by `kazi gc`.
3. `kazi template new pg19 --from-image postgres:19` — and a ghcr.io ref — produce editable, valid, immediately try-able templates with resource knobs commented.
4. `kazi run <image>` yields a routed persistent stack with no compose file; `kazi adopt` groups a hand-run container without recreating it.
5. `kazi template import` of an awesome-compose subdirectory becomes try-able.
6. `--keep`/`kazi keep` promotion edits only the manifest; containers are never recreated.
