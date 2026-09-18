#!/bin/sh
# Install the Television channel(s) this plugin's popups depend on.
#
# scripts/palette.sh runs `tv herdr`; without ~/.config/television/cable/herdr.toml
# that call fails instantly, the popup pane closes, and the keybinding looks
# like a no-op. Run from `make install` and from the plugin [[build]] step so a
# plain `herdr plugin install` is sufficient.
set -eu

root="${HERDR_PLUGIN_ROOT:-$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)}"
dest="${TELEVISION_CABLE_DIR:-$HOME/.config/television/cable}"

if ! command -v tv >/dev/null 2>&1; then
  printf 'herdr-palette: television (tv) not found on PATH.\n' >&2
  printf '  install it first: brew install television\n' >&2
  exit 1
fi

mkdir -p "$dest"
for src in "$root"/cable/*.toml; do
  [ -e "$src" ] || continue
  name=$(basename "$src")
  cp "$src" "$dest/$name"
  printf 'herdr-palette: installed television channel %s -> %s\n' "$name" "$dest/$name"
done
