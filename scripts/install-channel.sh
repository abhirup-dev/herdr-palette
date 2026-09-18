#!/bin/sh
# Install the Television channel(s) this plugin's popups depend on.
#
# scripts/palette.sh runs `tv herdr` and scripts/switch-palette.sh runs
# `tv herdr-switch`; without the channels in ~/.config/television/cable/ those
# calls fail instantly, the popup pane closes, and the keybinding looks like a
# no-op. Run from `make install` and from the plugin [[build]] step so a plain
# `herdr plugin install` is sufficient.
#
# Channels ship with an @BIN@ placeholder rather than a hardcoded checkout path:
# under `herdr plugin install` the plugin lives in a content-hashed directory,
# so the path is only knowable at install time.
set -eu

root="${HERDR_PLUGIN_ROOT:-$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)}"
dest="${TELEVISION_CABLE_DIR:-$HOME/.config/television/cable}"
bin="$root/bin/herdr-palette"

if ! command -v tv >/dev/null 2>&1; then
  printf 'herdr-palette: television (tv) not found on PATH.\n' >&2
  printf '  install it first: brew install television\n' >&2
  exit 1
fi

if [ ! -x "$bin" ]; then
  printf 'herdr-palette: binary missing at %s — run: make build\n' "$bin" >&2
  exit 1
fi

mkdir -p "$dest"
for src in "$root"/cable/*.toml; do
  [ -e "$src" ] || continue
  name=$(basename "$src")
  # @BIN@ -> absolute binary path. sed's delimiter is | since the path has /.
  sed "s|@BIN@|$bin|g" "$src" > "$dest/$name"
  printf 'herdr-palette: installed television channel %s -> %s\n' "$name" "$dest/$name"
done
