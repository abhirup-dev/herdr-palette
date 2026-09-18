#!/bin/sh
# Install the Television channel(s) this plugin's popups depend on.
#
# scripts/palette.sh runs `tv herdr` and scripts/switch-palette.sh runs
# `tv herdr-switch`; without the channels in ~/.config/television/cable/ those
# calls fail instantly, the popup pane closes, and the keybinding looks like a
# no-op. Run from `make install` and from the plugin [[build]] step so a plain
# `herdr plugin install` is sufficient.
#
# Channels ship with an @BIN@ placeholder rather than a hardcoded path, because
# the binary's location is only knowable at install time.
set -eu

root="${HERDR_PLUGIN_ROOT:-$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)}"
dest="${TELEVISION_CABLE_DIR:-$HOME/.config/television/cable}"
built="$root/bin/herdr-palette"

# The channel must name a STABLE path. `herdr plugin install` runs [[build]]
# inside a temporary .tmp-install-*/checkout/ directory and moves it away
# immediately afterwards, so baking $root into the channel yields a dead path
# on the very first popup. Copy the binary somewhere durable and point there.
bindir="${HERDR_PALETTE_BINDIR:-$HOME/.local/bin}"
bin="$bindir/herdr-palette"

if ! command -v tv >/dev/null 2>&1; then
  printf 'herdr-palette: television (tv) not found on PATH.\n' >&2
  printf '  install it first: brew install television\n' >&2
  exit 1
fi

if [ ! -x "$built" ]; then
  printf 'herdr-palette: binary missing at %s - run: make build\n' "$built" >&2
  exit 1
fi

# Replace in place via a temp file + mv so a running popup never reads a
# half-written binary.
mkdir -p "$bindir"
if ! cmp -s "$built" "$bin" 2>/dev/null; then
  cp "$built" "$bin.tmp.$$"
  chmod 0755 "$bin.tmp.$$"
  mv "$bin.tmp.$$" "$bin"
  printf 'herdr-palette: installed CLI -> %s\n' "$bin"
fi

mkdir -p "$dest"
for src in "$root"/cable/*.toml; do
  [ -e "$src" ] || continue
  name=$(basename "$src")
  # @BIN@ -> stable binary path. sed delimiter is | since the path contains /.
  sed "s|@BIN@|$bin|g" "$src" > "$dest/$name"
  printf 'herdr-palette: installed television channel %s -> %s\n' "$name" "$dest/$name"
done
