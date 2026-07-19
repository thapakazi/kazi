---
title: For LLMs & agents
description: How a coding agent or LLM should drive kazi — the JSON contract, conventions, and copy-paste recipes.
---

kazi is agent-friendly from day one. An LLM or script drives it the same way a
person does — by shelling out — but reads structured JSON instead of formatted
text. No MCP server or SDK is required.

## The contract

- **`--json` on every command** — stable, versioned schemas. Parse this, never
  the human table output.
- **Meaningful exit codes** — branch on these, don't scrape stderr:

  | Code | Meaning |
  |---|---|
  | `0` | OK |
  | `1` | Engine / runtime failure |
  | `2` | Usage error |
  | `3` | Stack not found |
  | `4` | No container runtime available |

- **`--yes` skips prompts** — always pass it for destructive verbs (`rm`, `gc`)
  in non-interactive runs.
- **Verbs are idempotent** — `up` on a running stack is a success no-op, so
  retries are safe.
- **Errors are structured JSON on stderr**, not free text.

## Rules of thumb for an agent

1. Always append `--json`; decode stdout as JSON.
2. Check the exit code first; on non-zero, read the JSON error on stderr.
3. Use `--yes` for anything that removes state.
4. Prefer `kazi try <template> -d` for scratch dependencies, then reclaim with
   `kazi gc --yes` — never leave residue.
5. Discover state with `kazi ls --json` / `kazi status --json` before acting.

## Command map

Read/inspect (safe, read-only):

```
kazi ls --json                 # every stack: name, kind, status, path
kazi status --json             # global dashboard
kazi status <stack> --json     # per-service state, health, ports
kazi describe <stack> --json   # full detail: manifest, services, endpoints
kazi ps --json                 # every container on the runtime
kazi urls [<stack>] --json     # reachable endpoints (HTTPS + localhost:PORT)
```

Lifecycle (mutating, idempotent):

```
kazi add <name> <path>         # register a compose file/dir
kazi up <stack>                # compose up -d (+ proxy on first run)
kazi down <stack>              # compose down
kazi restart <stack>
kazi rm <name> --yes           # deregister manifest (never touches containers)
```

Ephemeral & ad-hoc:

```
kazi try <template> -d --json          # scratch stack, detached
kazi try <template> --set k=v -d       # with value overrides
kazi keep <stack>                      # promote ephemeral → persistent
kazi run <image> --name <n> -p 8080:80 --json
kazi adopt <name> <container>... --json
kazi gc --yes --json                   # reclaim ephemeral stacks + orphans
```

Routing:

```
kazi expose <stack> <service> --json          # stable host port for TCP
kazi expose --remove <stack> <service> --json
```

## Recipes

Bring up a scratch Postgres, get its endpoint, then tear it down:

```sh
kazi try postgres -d --json
kazi urls postgres --json | jq -r '.[] | .url // .address'
kazi gc --yes --json
```

Find every running stack by name:

```sh
kazi ls --json | jq -r '.[] | select(.status == "running") | .name'
```

Start a stack only if it isn't already up (idempotent, so just call it):

```sh
kazi up blog --json
```

:::note[Coming in M3]
`kazi mcp` will expose these same engine operations as MCP tools for editors
that prefer it. It's a convenience layer over this exact CLI contract — the JSON
interface above is the source of truth.
:::
