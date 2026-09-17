# legacy-bash — archived, not wired

This directory is the complete pre-Phase-E bash + jq implementation of the
herdr palette (act.sh, dispatch.sh, source.sh, traverse.sh, execute.sh,
api-browser.sh, preview.sh, api-preview.sh, palette.sh, jq/, tests/),
archived from `~/.config/herdr/palette` when the Go binary took full
ownership.

**Nothing here is executed.** The live palette is the Go binary at
`../bin/herdr-palette` (repo: `~/Codes/Personal/herdr-palette`), wired via
`herdr plugin link`, the Television channel `~/.config/television/cable/herdr.toml`,
and the `prefix+p` keybinding. All schema parsing, traversal, rendering,
ranking, dispatching, previews, and interactive actions are native Go
(sonic; no jq, no bash runtime).

Kept for reference and as an emergency rollback: restore by copying these
files back to `~/.config/herdr/palette` and pointing the Television channel
source at `source.sh`. The bash suites under `tests/` require bash + jq +
fzf and a live herdr session.
