#!/usr/bin/env bash
# Regression checks for typed traversal, live-value separation, and ranking.
# Requires the normal palette dependencies: bash, jq, fzf, and a live Herdr.
set -eu

DIR=$(cd "$(dirname "$0")/.." && pwd)
TMP=$(mktemp -d "${TMPDIR:-/tmp}/herdr-palette-tests.XXXXXX")
trap 'rm -rf "$TMP"' EXIT
export HERDR_PALETTE_CACHE_DIR="$TMP/cache"

# Build a fresh protocol cache once, then --answers must be completely offline.
bash "$DIR/api-browser.sh" --ensure-cache >/dev/null
for spec in pane.move:answers/pane-move-tab.json pane.move:answers/pane-move-new-workspace.json agent.prompt:answers/agent-prompt.json; do
  method=${spec%%:*}; fixture="$DIR/tests/${spec#*:}"
  bash "$DIR/traverse.sh" "$method" --answers "$fixture" --print-answers-only > "$TMP/out.json"
  jq -e --slurpfile output "$TMP/out.json" '. == $output[0]' "$fixture" >/dev/null
done
bash -x "$DIR/traverse.sh" pane.move --answers "$DIR/tests/answers/pane-move-tab.json" --print-answers-only >/dev/null 2>"$TMP/trace"
test "$(grep -Ec '^\+.*herdr ' "$TMP/trace" || true)" = 0

# fzf's non-interactive filter mode proves picker compatibility for CI.
test "$(printf 'alpha\nbeta\n' | fzf --filter=bet --select-1 --exit-0)" = beta

# Source history adds rank rows without disturbing the underlying catalog.
bash "$DIR/source.sh" > "$TMP/base.tsv"
NOW=$(date -u +%Y-%m-%dT%H:%M:%SZ)
mkdir -p "$HERDR_PALETTE_CACHE_DIR"
jq -cn --arg ts "$NOW" '{ts:$ts,label:"split",command:"herdr pane split --current --direction right --no-focus"}' > "$HERDR_PALETTE_CACHE_DIR/selections.jsonl"
bash "$DIR/source.sh" > "$TMP/ranked.tsv"
head -1 "$TMP/ranked.tsv" | grep -q '^\[pane\] split right (keep focus) (\*)\t'
diff -u "$TMP/base.tsv" <(tail -n +2 "$TMP/ranked.tsv") >/dev/null
awk -F'\t' 'NF != 2 { exit 1 }' "$TMP/ranked.tsv"

for file in "$DIR"/*.sh; do bash -n "$file"; done
printf 'phase23 tests: PASS\n'
