#!/usr/bin/env bash
# herdr palette — plugin popup entry point.
# Runs the television channel, then routes the selection (LABEL<TAB>COMMAND)
# into the Go binary's select router, which executes it through the built-in
# dispatcher (single executor + selections.jsonl writer). No bash runtime.
set -u

if ! command -v tv >/dev/null 2>&1; then
  printf 'herdr-palette: television (tv) not found — brew install television\n' >&2
  read -r _ || true
  exit 1
fi
if ! tv list-channels 2>/dev/null | grep -qx 'herdr'; then
  printf 'herdr-palette: television channel "herdr" missing — run: make install-channel\n' >&2
  read -r _ || true
  exit 1
fi

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
