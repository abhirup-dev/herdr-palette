#!/usr/bin/env bash
# herdr switch surface — plugin popup entry point.
# Runs the herdr-switch television channel (workspaces/tabs/agents picker,
# Tab cycles filter presets), then routes the selection into the Go
# dispatcher like the actions palette.
set -u

sel=$(tv herdr-switch)
[ -z "$sel" ] && exit 0

label=$(printf '%s' "$sel" | cut -f 1)
command=$(printf '%s' "$sel" | cut -f 2)
[ -n "$command" ] || exit 0

bin="${HERDR_PLUGIN_ROOT:-$HOME/Codes/Personal/herdr-palette}/bin/herdr-palette"
if [ ! -x "$bin" ]; then
  printf 'herdr-palette: binary missing at %s — run: make build\n' "$bin" >&2
  exit 1
fi
exec "$bin" select "$command" "$label"
