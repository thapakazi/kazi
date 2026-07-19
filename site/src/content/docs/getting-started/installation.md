---
title: Installation
description: Build kazi from source, verify your container runtime, and wire up shell integration.
---

## Requirements

- **Go 1.25+**
- A container runtime with compose support — `docker compose` is first-class;
  `podman` and `nerdctl` are auto-detected and best-effort.
- [`just`](https://github.com/casey/just) *(optional)* for the convenience recipes.

## Build from source

kazi is pre-release, so install by building the binary:

```sh
git clone https://github.com/thapakazi/kazi
cd kazi

# with just
just build

# or plain Go
go build -o kazi ./cmd/kazi
```

Move the resulting `kazi` binary somewhere on your `PATH`, or run it in place
with `./kazi`.

```sh
just install   # go install ./cmd/kazi  → $GOBIN/kazi
```

## Verify the runtime

kazi shells out to `<runtime> compose`. Confirm a runtime is available:

```sh
docker compose version   # or: podman compose / nerdctl compose
```

By default kazi auto-detects the runtime (probing `docker`, then `podman`, then
`nerdctl`). Pin it explicitly in config if you want — see
[Configuration](#configuration).

## Trust the local CA (for HTTPS)

kazi routes HTTP services through a local Caddy proxy with its own internal CA.
Run this once so `https://<stack>.localhost` shows a green lock:

```sh
kazi trust        # installs kazi's CA into the system trust store (sudo)
```

:::note[Firefox]
Firefox uses its own certificate store. To have it honor kazi's CA, open
`about:config`, search for `security.enterprise_roots.enabled`, and set it to
`true`.
:::

## Shell integration

Add one line to your `~/.zshrc` (or `~/.bashrc`) to get the `kj` function, which
jumps into a stack's project directory:

```sh
eval "$(kazi shell-init)"
```

Then:

```sh
kj blog    # cd into blog's project directory
```

## Configuration

Config lives under `~/.config/kazi/` (override with `KAZI_CONFIG_DIR`). The file
is optional — every field has a sensible default.

```yaml
# ~/.config/kazi/config.yaml
apiVersion: kazi.dev/v1alpha1
kind: Config
spec:
  runtime: auto   # auto | docker | podman | nerdctl
  proxy:
    httpPorts: [80, 3000, 3001, 5000, 5173, 8000, 8080, 8888]
    tcpPorts:  [1521, 3306, 5432, 5672, 6379, 9092, 27017]
  ports:
    range: "42000-42999"   # host-port range for `kazi expose`
  cleanup:
    ephemeralTTL: "24h"    # `kazi gc` reclaims ephemeral stacks older than this
```

Ready to drive some stacks? Head to [Usage →](/getting-started/usage/).
