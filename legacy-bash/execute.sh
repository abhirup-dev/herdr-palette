#!/usr/bin/env bash
# Execute a cached Herdr API operation through a reviewed CLI binding.
#
# Flow: select operation (if omitted) → typed traversal → render argv → review
# → existing dispatch.sh. The protocol graph stays static in the cache; typed
# IDs are collected by traverse.sh from a live snapshot. bindings.json is the
# explicit safety boundary: missing and api-only bindings never execute.
#
# Usage: execute.sh [METHOD] [--answers FILE] [--yes]
# --answers is intended for deterministic scripts/tests. --yes accepts the
# review without a tty; interactive calls always display the review first.
set -eu

DIR=$(cd "$(dirname "$0")" && pwd)
JQ_DIR="$DIR/jq"
GO_BIN="$HOME/Codes/Personal/herdr-palette/bin/herdr-palette"
METHOD=""
ANSWERS_FILE=""
ASSUME_YES=false

# Phase C seam: when the Go binary exists it owns the whole pipeline; this
# bash script remains the full fallback so the palette works without a build.
if [ -x "$GO_BIN" ]; then
  exec "$GO_BIN" execute "$@"
fi

while [ $# -gt 0 ]; do
  case "$1" in
    --answers) ANSWERS_FILE=${2:?--answers needs a JSON file}; shift 2 ;;
    --yes) ASSUME_YES=true; shift ;;
    -h|--help)
      echo "usage: $0 [METHOD] [--answers FILE] [--yes]"; exit 0 ;;
    -*) echo "unknown option: $1" >&2; exit 2 ;;
    *)
      [ -z "$METHOD" ] || { echo "only one METHOD may be given" >&2; exit 2; }
      METHOD=$1; shift ;;
  esac
done

CACHE=$(bash "$DIR/api-browser.sh" --ensure-cache)
BINDINGS=$(cat "$DIR/bindings.json")

pick_method() {
  jq -L "$JQ_DIR" -r --argjson bindings "$BINDINGS" \
    -f "$JQ_DIR/execution-operation-lines.jq" "$CACHE" \
    | fzf --delimiter=$'\t' --with-nth=2 --prompt='execute> ' \
          --header='CLI bindings are executable; API-only operations are browse-only' \
    | cut -f 1
}
[ -n "$METHOD" ] || METHOD=$(pick_method || true)
[ -n "$METHOD" ] || exit 0

TREE=$(jq -L "$JQ_DIR" --arg method "$METHOD" -f "$JQ_DIR/operation-tree.jq" "$CACHE" 2>/dev/null) \
  || { echo "palette: unknown cached operation: $METHOD" >&2; exit 2; }

availability=$(jq -r --arg method "$METHOD" \
  '.[$method].availability // "api-only"' "$DIR/bindings.json")
if [ "$availability" != cli ]; then
  printf 'palette: %s is API-only (no CLI transport registered); refusing execution.\n' "$METHOD" >&2
  exit 2
fi

collect_answers() {
  # Phase B seam: prefer the Go traversal when the binary exists; the bash
  # walker remains the fallback so the palette keeps working without a build.
  local go_bin="$HOME/Codes/Personal/herdr-palette/bin/herdr-palette"
  local runner=()
  if [ -x "$go_bin" ]; then
    runner=("$go_bin" traverse)
  else
    runner=(bash "$DIR/traverse.sh")
  fi
  if [ -n "$ANSWERS_FILE" ]; then
    "${runner[@]}" "$METHOD" --answers "$ANSWERS_FILE" --print-answers-only
  else
    "${runner[@]}" "$METHOD" --print-answers-only
  fi
}

while :; do
  ANSWERS=$(collect_answers)
  RENDERED=$(printf '%s\n' "$ANSWERS" | jq -L "$JQ_DIR" \
    --arg method "$METHOD" --argjson bindings "$BINDINGS" --argjson tree "$TREE" \
    -f "$JQ_DIR/render-argv.jq")
  ok=$(printf '%s\n' "$RENDERED" | jq -r -L "$JQ_DIR" --arg action ok -f "$JQ_DIR/execution-result.jq")
  if [ "$ok" != true ]; then
    printf '%s\n' "$RENDERED" | jq . >&2
    exit 2
  fi

  # The review contains the unmodified answer tree and the exact argv. It is
  # deliberately rendered from JSON, never reconstructed from shell words.
  printf '%s\n' "$RENDERED" | jq -L "$JQ_DIR" --arg action review --arg method "$METHOD" --argjson answers "$ANSWERS" \
    -f "$JQ_DIR/execution-result.jq"

  if "$ASSUME_YES"; then choice=y
  else
    printf 'Execute this operation? [y]es / [n]o / [e]dit answers / [q]uit: '
    IFS= read -r choice
  fi
  case "$choice" in
    y|Y|yes)
      command=""
      while IFS= read -r b64; do
        arg=$(printf '%s' "$b64" | base64 -d)
        printf -v quoted '%q' "$arg"
        command+="${command:+ }$quoted"
      done < <(printf '%s\n' "$RENDERED" | jq -r -L "$JQ_DIR" --arg action argv-b64 -f "$JQ_DIR/execution-result.jq")
      exec bash "$DIR/dispatch.sh" "$command" "[api] $METHOD" ;;
    e|E|edit)
      [ -z "$ANSWERS_FILE" ] || { echo "palette: --answers input cannot be edited interactively" >&2; exit 2; }
      continue ;;
    n|N|no|q|Q|quit|"") exit 0 ;;
    *) echo "please choose y, n, e, or q" >&2 ;;
  esac
done
