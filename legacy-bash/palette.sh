#!/usr/bin/env bash
# herdr palette — popup entry point. Runs the television channel, then
# executes the COMMAND field (tab-split field 2) of whatever was selected.
set -u

sel=$(tv herdr)
[ -z "$sel" ] && exit 0

label=$(printf '%s' "$sel" | cut -f 1)
command=$(printf '%s' "$sel" | cut -f 2)
[ -n "$command" ] || exit 0
exec bash ~/.config/herdr/palette/dispatch.sh "$command" "$label"
