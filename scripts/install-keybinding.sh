#!/usr/bin/env bash
set -euo pipefail

herdr_bin="${HERDR_BIN_PATH:-herdr}"
config="${HERDR_CONFIG_PATH:-${XDG_CONFIG_HOME:-$HOME/.config}/herdr/config.toml}"

mkdir -p "$(dirname "$config")"
touch "$config"

# Migrate the pre-plugin popup binding (palette.sh popup) to the plugin action.
if grep -Fq 'command = "herdr-palette.open"' "$config"; then
  printf 'Herdr Palette keybinding already installed in %s\n' "$config"
else
  tmp="$(mktemp "${config}.tmp.XXXXXX")"
  # Drop the legacy popup block (comment through its height line), if present.
  sed -e '/^# herdr command palette (television)\./,/^height = "80%"$/d' "$config" >"$tmp"
  cat >>"$tmp" <<'EOF'

# herdr-palette: schema-driven command palette (television + Go).
[[keys.command]]
key = "prefix+p"
type = "plugin_action"
command = "herdr-palette.open"
description = "open the herdr command palette"
EOF
  cat "$tmp" >"$config"
  rm -f "$tmp"
  printf 'Installed prefix+P Herdr Palette keybinding in %s\n' "$config"
fi

"$herdr_bin" config check
