# kazi docs site

Astro [Starlight](https://starlight.astro.build) docs, themed with
[lucode-starlight](https://github.com/lucas-labs/lucode-starlight-theme).
Powered by [bun](https://bun.sh).

## Develop

```sh
just install   # bun install
just dev       # http://localhost:4321
just build     # static output → dist/
```

(Or from the repo root: `just docs-install`, `just docs-dev`, `just docs-build`.)

## Structure

```
src/content/docs/
  index.mdx                     # intro / splash
  getting-started/
    installation.md
    usage.md
  reference/
    commands.md
src/assets/                     # logo + images
```

## Placeholders to replace

- **`src/assets/tui-demo.svg`** — stand-in for the TUI demo. The bubbletea
  dashboard ships in milestone **M5**; record a real clip, drop it at
  `src/assets/tui-demo.gif`, and update the import in `index.mdx`.
- **`site` / `base` in `astro.config.mjs`** — set for GitHub Pages
  (`https://thapakazi.github.io/kazi`); adjust when deployment is finalized.
