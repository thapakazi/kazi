---
title: Usage
description: The everyday kazi workflow — register and run compose stacks, reach them over HTTPS, spin up throwaway databases, and clean up.
---

This page walks the most common workflows. For the full verb list, see the
[Commands reference](/reference/commands/).

## Run an existing compose project

Point kazi at any compose file or directory and drive it by name — no `cd`, no
import ceremony.

```sh
kazi add blog ~/repos/blog     # register a stack
kazi up blog                   # compose up -d (creates the proxy on first run)
kazi ls                        # list every stack, registered + discovered
kazi status blog               # per-service state, health, ports
kazi logs blog -f              # stream logs
kazi down blog                 # stop and remove containers
```

:::tip[No import needed]
Lifecycle verbs (`up`, `down`, `restart`, `logs`, `status`) also work on
compose projects kazi never launched — it discovers them via the
`com.docker.compose.project` label. `kazi ls` lists them alongside registered
stacks.
:::

## Reach services over HTTPS

kazi routes every HTTP service through a local Caddy proxy. The service name
becomes the subdomain automatically:

```sh
kazi urls blog     # e.g. https://blog.localhost
kazi urls          # all reachable endpoints, across all stacks
```

- **Single HTTP service** → `https://blog.localhost`
- **Multiple HTTP services** → `https://api.blog.localhost`, etc.

Declare the primary service in the stack manifest when there's more than one:

```yaml
# ~/.config/kazi/stacks/blog.yaml
spec:
  proxy:
    service: web
```

### Expose a TCP port (databases, brokers)

Non-HTTP services aren't proxied by name. Allocate a **stable host port** that
survives `down`/`up`:

```sh
kazi expose blog db          # assign a port in the 42000–42999 range
kazi urls blog               # shows localhost:NNNNN for db
kazi expose --remove blog db # release it
```

### CLI tools (curl, psql, redis-cli)

The `kazi trust` CA covers browsers, not every CLI tool. Route by name with
`--resolve`, or use the plain `localhost:PORT` from `kazi urls`:

```sh
curl -sk --resolve blog.localhost:443:127.0.0.1 https://blog.localhost
```

## Instant databases & scratch containers

Spin up a throwaway service from the built-in catalog — no compose file, no
manifest to write:

```sh
kazi try postgres                          # foreground; Ctrl-C tears it down
kazi try postgres --set postgres_password=secret
kazi try redis -d                          # detached (for scripts/agents)
kazi keep redis                            # decide to keep it (manifest-only flip)
```

On exit, a foreground `try` runs a full `down -v --rmi local`, frees ports,
deletes the manifest, and reloads the proxy — **zero residue**. The catalog
ships offline with `postgres`, `redis`, `mysql`, `mongo`, `mailpit`, and
`minio`.

```sh
kazi template ls                                   # list the catalog
kazi template new pg19 --from-image postgres:19    # scaffold from any image ($EDITOR)
kazi template import ./awesome-compose/postgresql  # import a dir or git URL
kazi eject postgres ./pg --add                     # graduate a template to a real project
```

## Ad-hoc & adopted containers

```sh
# Run any image as a persistent, routed stack — no compose file generated.
kazi run traefik/whoami --name hello -p 8080:80 -e KEY=val

# Group already-running containers into a stack without recreating them.
kazi adopt mydb pg-container redis-container
```

## Clean up

```sh
kazi gc --dry-run   # show what would be reclaimed
kazi gc             # confirm, then reclaim
kazi gc --yes       # no prompt (for automation)
```

`gc` reclaims stopped or TTL-expired ephemeral stacks (full teardown including
volumes), containers orphaned by a crashed `try -d`, and stale port allocations.

## Driving kazi from a script or agent

Every command supports `--json` with stable, versioned schemas, plus `--yes` for
non-interactive runs and documented [exit codes](/reference/commands/#exit-codes).
That's all an agent needs — no MCP server required.

```sh
kazi ls --json | jq -r '.[] | select(.status=="running") | .name'
kazi try redis -d --json
kazi gc --yes --json
```
