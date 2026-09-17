#!/usr/bin/env bash
# Browse the installed Herdr protocol's operations.
#
# Cache lifecycle
# ---------------
# First open for a Herdr version downloads the complete JSON schema once,
# derives a protocol-keyed static index, and atomically records a small pointer.
# Later opens use only that pointer and cache; they do not invoke or parse
# `herdr api schema`. A changed `herdr --version` invalidates the pointer and
# causes one fresh schema build. Runtime session values are never cached.
set -eu

DIR=$(cd "$(dirname "$0")" && pwd)
JQ_DIR="$DIR/jq"
# Phase A coexistence seam: the Go indexer writes the exact same graph/pointer
# contract. Traversal, execution, and ranking remain bash+jq until their own
# migration phases. Removing this binary cleanly restores the jq indexer.
GO_INDEXER="$HOME/Codes/Personal/herdr-palette/bin/herdr-palette"
CACHE_ROOT=${HERDR_PALETTE_CACHE_DIR:-"${XDG_CACHE_HOME:-$HOME/.cache}/herdr-palette"}
CURRENT="$CACHE_ROOT/current.json"
HERDR_VERSION=$(herdr --version)
mkdir -p "$CACHE_ROOT"

cache_for_current_version() {
  if [ -x "$GO_INDEXER" ]; then
    "$GO_INDEXER" index --check 2>/dev/null
    return $?
  fi

  [ -f "$CURRENT" ] || return 1
  jq -L "$JQ_DIR" -e --arg herdr_version "$HERDR_VERSION" \
    -f "$JQ_DIR/cache-current-matches.jq" "$CURRENT" >/dev/null 2>&1 || return 1

  local protocol cache
  protocol=$(jq -r -f "$JQ_DIR/cache-current-protocol.jq" "$CURRENT")
  cache="$CACHE_ROOT/protocol-$protocol.json"
  [ -f "$cache" ] || return 1
  printf '%s\n' "$cache"
}

build_cache() {
  if [ -x "$GO_INDEXER" ]; then
    "$GO_INDEXER" index
    return $?
  fi

  local schema raw protocol schema_version digest indexed_at cache temp_index temp_current
  schema=$(mktemp "${TMPDIR:-/tmp}/herdr-schema.XXXXXX")
  temp_index=$(mktemp "$CACHE_ROOT/.protocol-index.XXXXXX")
  temp_current=$(mktemp "$CACHE_ROOT/.current.XXXXXX")
  trap 'rm -f "$schema" "$temp_index" "$temp_current"' RETURN

  # This is the only full schema read. It is deliberately performed only on a
  # cache miss/version change; all later traversal reads the generated index.
  herdr api schema --json > "$schema"
  raw=$(jq -r -f "$JQ_DIR/raw-metadata.jq" "$schema")
  IFS=$'\t' read -r protocol schema_version <<EOF
$raw
EOF
  case "$protocol:$schema_version" in
    *[!0-9:]*|:) echo "palette: schema has invalid protocol metadata" >&2; return 1 ;;
  esac

  # POSIX cksum is used instead of a platform-specific sha tool. The checksum
  # records which exact bytes generated the index without adding a dependency.
  digest="cksum:$(cksum < "$schema" | tr -s ' ' | cut -d ' ' -f 1-2 | tr ' ' ':')"
  indexed_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)
  cache="$CACHE_ROOT/protocol-$protocol.json"

  jq -L "$JQ_DIR" \
    --arg digest "$digest" \
    --arg generated_at "$indexed_at" \
    -f "$JQ_DIR/build-index.jq" "$schema" > "$temp_index"
  mv "$temp_index" "$cache"

  jq -L "$JQ_DIR" -n \
    --arg herdr_version "$HERDR_VERSION" \
    --argjson protocol "$protocol" \
    --argjson schema_version "$schema_version" \
    --arg cache "$cache" \
    --arg indexed_at "$indexed_at" \
    -f "$JQ_DIR/build-current.jq" > "$temp_current"
  mv "$temp_current" "$CURRENT"
  printf '%s\n' "$cache"
}

# --reindex intentionally deletes only the current-version pointer. The old
# protocol file is safely replaced atomically by build_cache; protocol caches
# for other installed versions remain available for inspection.
MODE=${1:-}
case "$MODE" in
  ""|--ensure-cache|--reindex) ;;
  *) echo "usage: $0 [--ensure-cache|--reindex]" >&2; exit 2 ;;
esac
[ "$MODE" = --reindex ] && rm -f "$CURRENT"
CACHE=$(cache_for_current_version || build_cache)

# Other palette components use this mode to share the same lazy cache lifecycle
# without opening an fzf UI. It is deliberately an internal path-only API.
if [ "$MODE" = --ensure-cache ] || [ "$MODE" = --reindex ]; then
  printf '%s\n' "$CACHE"
  exit 0
fi

# Reindex is a first fzf row, rather than a hidden maintenance command. It is
# intentionally separate from operation rows and rebuilds only after Enter.
selection=$({
    printf '__reindex__\t[reindex] rebuild cache for installed Herdr protocol\n'
    jq -L "$JQ_DIR" -r -f "$JQ_DIR/operation-lines.jq" "$CACHE"
  } | fzf --scheme=default --delimiter=$'\t' --with-nth=2 \
      --prompt='api> ' \
      --header='Herdr protocol operations — Enter [reindex] to refresh schema cache' \
      --preview="bash '$DIR/api-preview.sh' '$CACHE' {1}" \
      --preview-window='right:60%:wrap' \
  || true)
[ -z "$selection" ] && exit 0

method=$(printf '%s' "$selection" | cut -f 1)
if [ "$method" = __reindex__ ]; then
  bash "$0" --reindex >/dev/null
  exec bash "$0"
fi
# Browse retains its non-mutating typed traversal; execute.sh is the reviewed
# transport path exposed separately in the main palette.
exec bash "$DIR/traverse.sh" "$method"
