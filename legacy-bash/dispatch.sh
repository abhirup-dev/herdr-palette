#!/usr/bin/env bash
# herdr palette — general-purpose action dispatcher.
# EVERY palette entry executes through here. It:
#   - passes the terminal through (interactive prompts/fzf/attach still work)
#   - tees all output to a capture file
#   - on failure: shows a full error report (command, exit code, parsed herdr
#     JSON error, stderr tail) and waits — errors can never vanish again
#   - on success with >5 lines of output: shows it in a pager (info commands)
#   - on success with little/no output: closes immediately (pure actions)
# Applies to every current and future manifest entry, including custom scripts.
#
# Successful selections are recorded as JSONL at selections.jsonl. The log is
# append-only, portable, and intentionally separate from the static schema
# cache. Argument 2 is the human label; omitting it preserves old callers.

set -u
cmd=${1:-}
label=${2:-$cmd}
[ -z "$cmd" ] && exit 0

CAP=$(mktemp "${TMPDIR:-/tmp}/herdr-palette.XXXXXX")
trap 'rm -f "$CAP"' EXIT

# Run with output tee'd; stdio stays on the tty so read/fzf/attach work.
bash -c "$cmd" 2> >(tee "$CAP" >&2)
rc=$?

if [ $rc -ne 0 ]; then
  printf '\n\033[1;31m── palette: command failed ────────────────────────────\033[0m\n'
  printf '\033[1m$\033[0m %s\n' "$cmd"
  printf '\033[1mexit:\033[0m %s\n' "$rc"
  # herdr CLI errors are JSON on stderr — surface the message field if present.
  msg=$(grep -oE '\{"error".*' "$CAP" 2>/dev/null | head -1 | jq -r '.error.message // .error // empty' 2>/dev/null)
  [ -n "$msg" ] && printf '\033[1mherdr:\033[0m %s\n' "$msg"
  printf '\033[1;31m───────────────────────────────────────────────────────\033[0m\n'
  printf 'press any key to close\n'
  IFS= read -r -n 1 _ < /dev/tty
  exit $rc
fi

# Record only successful runs. History failure must never block an action.
CACHE_ROOT=${HERDR_PALETTE_CACHE_DIR:-"${XDG_CACHE_HOME:-$HOME/.cache}/herdr-palette"}
mkdir -p "$CACHE_ROOT" 2>/dev/null || true
jq -cn --arg ts "$(date -u +%Y-%m-%dT%H:%M:%SZ)" --arg label "$label" --arg command "$cmd" \
  '{ts:$ts,label:$label,command:$command}' >> "$CACHE_ROOT/selections.jsonl" 2>/dev/null || true

# Success: page substantial output, skip trivial one-liners.
lines=$(wc -l < "$CAP" | tr -d ' ')
if [ "$lines" -gt 5 ]; then
  {
    printf '\033[1m$ %s\033[0m — exit 0, %s lines (q to close)\n\n' "$cmd" "$lines"
    cat "$CAP"
  } | less -R
fi
exit 0
