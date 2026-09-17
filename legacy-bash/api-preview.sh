#!/usr/bin/env bash
# Render an indexed operation in the fzf preview pane.
# Input is the static cache only; live session state is not read in phase 1.
set -eu

CACHE=${1:?cache path required}
METHOD=${2:?operation method required}
DIR=$(cd "$(dirname "$0")" && pwd)

if [ "$METHOD" = __reindex__ ]; then
  cat <<'EOF'
Rebuild the cached operation graph for the installed Herdr version.

This performs one `herdr api schema --json` call, rewrites the current
protocol cache atomically, then reopens the operation browser. Live panes,
tabs, workspaces, and agents are never written to this cache.
EOF
  exit 0
fi

jq -L "$DIR/jq" --arg method "$METHOD" -f "$DIR/jq/operation-detail.jq" "$CACHE"
