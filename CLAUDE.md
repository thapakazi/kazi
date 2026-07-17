# kazi

Compose-preferred, runtime-agnostic (docker/podman/nerdctl) local stack manager. Go 1.25, module `github.com/thapakazi/kazi`, cobra CLI.

Note: docs/roadmap.md says "kaji" — the decided name is **kazi**. Use kazi everywhere.

## Status

Pre-code: only `docs/` exists. Spec for milestone M0 is being written under `docs/superpowers/specs/`. See `docs/roadmap.md` for the full plan.

## Architecture rules

- **One engine, three skins** (CLI, TUI, MCP): all logic lives in `internal/engine` (facade), backed by `internal/store`, `internal/runtime`, `internal/compose`, `internal/labels`.
- `cmd/kazi` cobra handlers stay thin: parse flags → call engine → render text or `--json`. Never put logic in cobra handlers.
- Orchestration shells out to `<runtime> compose` — the compose spec is the contract. Never talk to the Docker API socket directly.
- Docker is the first-class tested runtime; podman/nerdctl are best-effort via auto-detect.

## Config & manifests

- YAML at `~/.config/kazi/`, k8s-style: `apiVersion: kazi.dev/v1alpha1`, `kind`, `metadata`, `spec`.
- `status` is computed live from the runtime, never persisted.
- Container labels: `kazi.managed`, `kazi.stack` (`kazi.ephemeral` reserved).

## Commands

No Makefile yet. Standard Go:

```
go build ./...
go test ./...
go vet ./...
```
