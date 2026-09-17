#!/usr/bin/env bash
# herdr palette — plugin popup entry point.
# Runs the television channel, then routes the selection (LABEL<TAB>COMMAND)
# into the Go binary's select router, which executes it through the built-in
# dispatcher (single executor + selections.jsonl writer). No bash runtime.
set -u

bin="${HERDR_PLUGIN_ROOT:-$HOME/Codes/Personal/herdr-palette}/bin/herdr-palette"
if [ ! -x "$bin" ]; then
  printf 'herdr-palette: binary missing at %s — run: make build\n' "$bin" >&2
  exit 1
fi

sel=$(tv herdr)
[ -z "$sel" ] && exit 0

label=$(printf '%s' "$sel" | cut -f 1)
command=$(printf '%s' "$sel" | cut -f 2)
[ -n "$command" ] || exit 0
exec "$bin" select "$command" "$label"
