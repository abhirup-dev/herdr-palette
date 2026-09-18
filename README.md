# herdr-palette

A schema-driven command palette for [Herdr](https://github.com/) — a fuzzy
Television popup over every Herdr operation: current-pane/tab/workspace
actions, live agent/pane/tab/workspace targets, and the full protocol API
with reviewed CLI bindings. 100% Go (bytedance/sonic; no bash, no jq).

Installed as a Herdr plugin (`herdr plugin link`) — **this repo root is the
single home for all code and data**. The historical bash+jq implementation
is archived, unwired, under `legacy-bash/`.

## Requirements

- [Television](https://github.com/alexpasmantier/television) (`tv`) — the popup
  renderer. `brew install television`. Both popups shell out to `tv`; if it is
  missing the pane exits immediately and the keybinding looks like a no-op.

## Install

```sh
make install          # build + link plugin + install CLI + channel + prefix+P keybinding
```

- CLI: `~/.local/bin/herdr-palette`
- Plugin: `herdr plugin link` (manifest `herdr-plugin.toml`; popup pane +
  actions)
- Keybinding: `prefix+p` → plugin action `herdr-palette.open`
- Television channel: `cable/herdr.toml` -> `~/.config/television/cable/herdr.toml`
  (source, preview, and run actions all invoke this binary). Installed by
  `make install-channel`, and by the plugin `[[build]]` step so a plain
  `herdr plugin install` is self-sufficient.

## Subcommands

| Command | Purpose |
|---|---|
| `source` | Full palette manifest: ranked `(*)` recents head + catalog (LABEL⇥COMMAND) |
| `select 'LABEL⇥COMMAND'` / `select COMMAND LABEL` | Popup/TV router → built-in dispatcher (execute + selection log + error report) |
| `act NAME [ARGS…]` | Interactive/compound actions (port of act.sh): rename, move, close, prompts, send-keys |
| `api [--reindex]` | Operation browser over the cached protocol graph; non-mutating typed traversal |
| `execute [METHOD] [--answers FILE] [--yes] [--pick]` | Reviewed execution: traverse → render argv → review → dispatch |
| `traverse METHOD [--answers FILE] [--print-answers-only]` | Typed parameter traversal with live datasources |
| `preview 'COMMAND'` | TV preview: live pane/agent output, else subcommand help |
| `index [--check] [--graph METHOD] [--schema PATH]` | Build/verify the protocol graph cache |

## Runtime layout

- **Plugin root** (this repo): code, `bindings.json`, `herdr-plugin.toml`,
  `scripts/`. Resolved at runtime via `$HERDR_PLUGIN_ROOT`, else the
  directory containing `bin/herdr-palette` (`internal/pluginroot`).
- **Cache** (`~/.cache/herdr-palette/`): `protocol-<N>.json` graph (lazy,
  keyed by herdr version), `current.json` pointer, `selections.jsonl`
  ranking log. Runtime session values are never cached.

## Architecture

```
cmd/herdr-palette      CLI wiring
internal/schema        herdr api schema parsing
internal/graph         protocol affordance graph (91+ operations)
internal/traverse      typed param walker (fzf TUI on stderr)
internal/datasource    live pane/tab/workspace/agent pickers
internal/bindings      hand-maintained CLI binding table (safety boundary)
internal/render        answers → argv (validation-first, no partial argv)
internal/execute       reviewed pipeline → dispatcher
internal/dispatch      single executor: passthrough, error report, JSONL log
internal/actions       composite interactive actions (act)
internal/ranking       recency-weighted frequency (top 8, " (*)" suffix)
internal/manifest      catalog + ranked head
internal/pluginroot    single-home resolution
```

API-only operations (no CLI binding) are browse-only and refuse execution
before any prompting. Failed commands render the red error report (command,
exit code, parsed herdr JSON error) and wait — errors never vanish.

## Development

```sh
make check     # gofmt + go test ./... + go vet
make test      # go test ./...
make build     # bin/herdr-palette
```

`legacy-bash/` is reference-only (emergency rollback instructions in its
README).
