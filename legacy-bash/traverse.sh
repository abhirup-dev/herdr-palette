#!/usr/bin/env bash
# Traverse a cached Herdr operation's typed parameter tree.
#
# Usage: traverse.sh METHOD [--answers FILE] [--print-answers-only]
#
# The cache is static protocol metadata. Interactive identifier pickers obtain
# a fresh `herdr api snapshot`; --answers mode deliberately makes no Herdr
# calls at all, so it is suitable for tests and scripts. This phase only prints
# a validated answer tree — it never executes an operation.
set -eu

DIR=$(cd "$(dirname "$0")" && pwd)
JQ_DIR="$DIR/jq"
METHOD=${1:-}
shift || true
ANSWERS_FILE=""
PRINT_ONLY=false
while [ $# -gt 0 ]; do
  case "$1" in
    --answers) ANSWERS_FILE=${2:?--answers needs a JSON file}; shift 2 ;;
    --print-answers-only) PRINT_ONLY=true; shift ;;
    *) echo "usage: $0 METHOD [--answers FILE] [--print-answers-only]" >&2; exit 2 ;;
  esac
done
[ -n "$METHOD" ] || { echo "usage: $0 METHOD [--answers FILE] [--print-answers-only]" >&2; exit 2; }

CACHE_ROOT=${HERDR_PALETTE_CACHE_DIR:-"${XDG_CACHE_HOME:-$HOME/.cache}/herdr-palette"}
if [ -n "$ANSWERS_FILE" ]; then
  # No version/schema/snapshot calls in scripted mode. The caller intentionally
  # tests a specific cached protocol; first open api-browser to create it.
  CACHE=$(jq -r -f "$JQ_DIR/cache-path.jq" "$CACHE_ROOT/current.json" 2>/dev/null || true)
  [ -n "$CACHE" ] && [ -f "$CACHE" ] || { echo "palette: cache missing; run api-browser once" >&2; exit 1; }
else
  CACHE=$(bash "$DIR/api-browser.sh" --ensure-cache)
fi

# The operation tree was resolved at cache-build time; this is only a lookup.
TREE=$(jq -L "$JQ_DIR" --arg action tree --arg method "$METHOD" -c \
  -f "$JQ_DIR/traverse-runtime.jq" "$CACHE")

if [ -n "$ANSWERS_FILE" ]; then
  [ -f "$ANSWERS_FILE" ] || { echo "palette: answers file not found: $ANSWERS_FILE" >&2; exit 1; }
  ANSWERS=$(jq -L "$JQ_DIR" --arg action compact -c -f "$JQ_DIR/traverse-runtime.jq" "$ANSWERS_FILE")
  jq -n --argjson tree "$TREE" --argjson answers "$ANSWERS" \
    -L "$JQ_DIR" -f "$JQ_DIR/validate-run.jq"
  exit 0
fi

ANSWERS='{}'
set_value() {
  local path=$1 value=$2
  ANSWERS=$(jq -L "$JQ_DIR" --arg action set-path --arg path "$path" --argjson value "$value" -c \
    -f "$JQ_DIR/traverse-runtime.jq" <<<"$ANSWERS")
}
node_field() { jq -L "$JQ_DIR" --arg action property --arg property "${1#.}" -r -f "$JQ_DIR/traverse-runtime.jq" <<<"$2"; }
json_string() { jq -L "$JQ_DIR" --arg action string --arg value "$1" -cn -f "$JQ_DIR/traverse-runtime.jq"; }

# Resolve only identifiers against current session state. The registry is
# intentionally in bash so a maintainer can add a semantic field without
# touching the static protocol graph.
datasource_for() {
  local name=$1
  case "$name" in
    pane_id|source_pane_id|target_pane_id) printf panes ;;
    tab_id) printf tabs ;;
    workspace_id) printf workspaces ;;
    target) case "$METHOD" in agent.*) printf agents ;; *) printf panes ;; esac ;;
    *) return 1 ;;
  esac
}
pick_live() {
  local source=$1 prompt=$2 snap row
  snap=$(herdr api snapshot)
  row=$(printf '%s' "$snap" | jq -r --arg source "$source" -f "$JQ_DIR/snapshot-values.jq" \
    | fzf --delimiter=$'\t' --with-nth=2 --prompt="$prompt> " || true)
  printf '%s' "$row" | cut -f 1
}
pick_enum() {
  local prompt=$1 values=$2
  printf '%s' "$values" | jq -L "$JQ_DIR" --arg action variant-values -r -f "$JQ_DIR/traverse-runtime.jq" \
    | fzf --prompt="$prompt> " || true
}
read_scalar() {
  local prompt=$1 type=$2 default=$3 value
  printf '%s' "$prompt"
  [ "$default" != null ] && printf ' [%s]' "$default"
  printf ': '
  IFS= read -r value
  [ -z "$value" ] && [ "$default" != null ] && value=$default
  case "$type" in
    boolean) case "$value" in y|Y|yes|true|1) printf true ;; n|N|no|false|0) printf false ;; *) return 1 ;; esac ;;
    integer) [[ $value =~ ^-?[0-9]+$ ]] && printf '%s' "$value" || return 1 ;;
    number) [[ $value =~ ^-?([0-9]+([.][0-9]*)?|[.][0-9]+)$ ]] && printf '%s' "$value" || return 1 ;;
    *) json_string "$value" ;;
  esac
}

walk_node() {
  local node=$1 path=$2 name=$3 kind value source selected type default prompt fields count i field fname required child
  kind=$(node_field '.kind' "$node")
  case "$kind" in
    enum)
      selected=$(pick_enum "$name" "$(node_field '.values' "$node")")
      [ -n "$selected" ] || return 1
      set_value "$path" "$(json_string "$selected")" ;;
    union)
      selected=$(jq -L "$JQ_DIR" --arg action variant-tags -r -f "$JQ_DIR/traverse-runtime.jq" <<<"$node" | fzf --prompt="$name type> " || true)
      [ -n "$selected" ] || return 1
      set_value "$path.type" "$(json_string "$selected")"
      count=$(jq -L "$JQ_DIR" --arg action variant-count --arg tag "$selected" -r -f "$JQ_DIR/traverse-runtime.jq" <<<"$node")
      for ((i=0; i<count; i++)); do
        field=$(jq -L "$JQ_DIR" --arg action variant-field --arg tag "$selected" --argjson index "$i" -c -f "$JQ_DIR/traverse-runtime.jq" <<<"$node")
        walk_field "$field" "$path" || return 1
      done ;;
    object)
      count=$(jq -L "$JQ_DIR" --arg action field-count -r -f "$JQ_DIR/traverse-runtime.jq" <<<"$node")
      for ((i=0; i<count; i++)); do
        field=$(jq -L "$JQ_DIR" --arg action field --argjson index "$i" -c -f "$JQ_DIR/traverse-runtime.jq" <<<"$node")
        walk_field "$field" "$path" || return 1
      done ;;
    array)
      value='[]'
      while :; do
        printf '%s item (empty finishes): ' "$name"
        IFS= read -r selected
        [ -z "$selected" ] && break
        value=$(jq -L "$JQ_DIR" --arg action append-string --arg value "$selected" -c -f "$JQ_DIR/traverse-runtime.jq" <<<"$value")
      done
      set_value "$path" "$value" ;;
    scalar)
      source=$(datasource_for "$name" || true)
      if [ -n "$source" ]; then
        selected=$(pick_live "$source" "$name")
        [ -n "$selected" ] || return 1
        set_value "$path" "$(json_string "$selected")"
      else
        type=$(node_field '.type' "$node")
        default=$(node_field '.default' "$node")
        prompt=$(node_field '.description' "$node")
        [ "$prompt" = null ] && prompt=""
        selected=$(read_scalar "$(printf '%s' "$prompt" | sed "s/^/$name /")" "$type" "$default") || { echo "invalid $type" >&2; return 1; }
        set_value "$path" "$selected"
      fi ;;
    *) echo "unsupported schema node for $path: $kind" >&2; return 1 ;;
  esac
}
walk_field() {
  local field=$1 parent=$2 name path required choice node
  name=$(node_field '.name' "$field")
  path=${parent:+$parent.}$name
  required=$(node_field '.required' "$field")
  node=$(node_field '.node' "$field")
  if [ "$required" != true ]; then
    choice=$(printf 'skip\nconfigure\n' | fzf --prompt="$name> " || true)
    [ "$choice" = configure ] || return 0
  fi
  walk_node "$node" "$path" "$name"
}

walk_node "$TREE" "" "$METHOD"
$PRINT_ONLY || printf 'Answers for %s:\n' "$METHOD"
jq -f "$JQ_DIR/print-json.jq" <<<"$ANSWERS"
