#!/usr/bin/env bash
# Regression checks for Phase 4 bindings/execution and Phase 5 cache controls.
# This intentionally performs one real pane move in a disposable test tab, then
# closes both resources. It requires a live Herdr session, bash, jq, and fzf.
set -euo pipefail

DIR=$(cd "$(dirname "$0")/.." && pwd)
JQ_DIR="$DIR/jq"
TMP=$(mktemp -d "${TMPDIR:-/tmp}/herdr-palette-phase45.XXXXXX")
TEST_TAB=""
MOVED_PANE=""
cleanup() {
  [ -n "$MOVED_PANE" ] && herdr pane close "$MOVED_PANE" >/dev/null 2>&1 || true
  [ -n "$TEST_TAB" ] && herdr tab close "$TEST_TAB" >/dev/null 2>&1 || true
  rm -rf "$TMP"
}
trap cleanup EXIT

CACHE=$(bash "$DIR/api-browser.sh" --ensure-cache)
BINDINGS=$(cat "$DIR/bindings.json")
render() {
  local method=$1 answers=$2 tree
  tree=$(jq -L "$JQ_DIR" --arg method "$method" -f "$JQ_DIR/operation-tree.jq" "$CACHE")
  jq -L "$JQ_DIR" --arg method "$method" --argjson bindings "$BINDINGS" --argjson tree "$tree" \
    -f "$JQ_DIR/render-argv.jq" "$answers"
}

# Every CLI binding's command words and documented flags must be present in
# actual installed `herdr <group> <command> --help` output. The binding list
# contains 20 reviewed operations, one of which is explicitly API-only.
COUNT=0
jq -r -f "$JQ_DIR/binding-check-lines.jq" "$DIR/bindings.json" > "$TMP/bindings.tsv"
while IFS=$'\t' read -r method availability command flags; do
  COUNT=$((COUNT + 1))
  if [ "$availability" = api-only ]; then
    [ "$method" = pane.focus ]
    continue
  fi
  set -- $command
  HELP=$(herdr "$@" --help 2>&1)
  if [ -n "$flags" ]; then
    while IFS= read -r flag; do
      [ -z "$flag" ] || printf '%s\n' "$HELP" | grep -F -- "$flag" >/dev/null
    done < <(printf '%s' "$flags" | tr '\037' '\n')
  fi
done < "$TMP/bindings.tsv"
[ "$COUNT" -eq 20 ]

# Unbound optional values are an error object, never a partial argv.
render pane.split "$DIR/tests/answers/pane-split-unbound.json" > "$TMP/unbound.json"
jq -e '.ok == false and .error.code == "unbound_parameter" and .error.paths == ["workspace_id"]' "$TMP/unbound.json" >/dev/null

# API-only request methods refuse execution before prompting for a terminal.
if bash "$DIR/execute.sh" pane.focus --yes >"$TMP/api-only.out" 2>&1; then
  echo "api-only operation unexpectedly executed" >&2
  exit 1
fi
grep -F 'API-only (no CLI transport registered)' "$TMP/api-only.out" >/dev/null

# Full reviewed loop: create a scratch pane, move it through execute.sh using a
# generated answer tree, then close the moved pane and its disposable tab.
SNAP=$(herdr api snapshot)
WORKSPACE=$(printf '%s' "$SNAP" | jq -r '.result.snapshot.focused_workspace_id // (.result.snapshot.workspaces[] | select(.focused == true) | .workspace_id)')
TEST_TAB=$(herdr tab create --workspace "$WORKSPACE" --label "palette-exec-test" --no-focus | jq -r '.result.tab.tab_id')
SCRATCH=$(herdr pane split --current --direction right --cwd "$PWD" --no-focus | jq -r '.result.pane.pane_id')
jq -n --arg pane "$SCRATCH" --arg tab "$TEST_TAB" \
  '{pane_id:$pane,destination:{type:"tab",tab_id:$tab,split:"right"},focus:false}' > "$TMP/move.json"
bash "$DIR/execute.sh" pane.move --answers "$TMP/move.json" --yes > "$TMP/move.out"
MOVED_PANE=$(tail -1 "$TMP/move.out" | jq -r '.result.move_result.pane.pane_id')
[ -n "$MOVED_PANE" ] && [ "$MOVED_PANE" != null ]
herdr pane close "$MOVED_PANE" >/dev/null
MOVED_PANE=""
herdr tab close "$TEST_TAB" >/dev/null
TEST_TAB=""

# Regression: an empty-object answer value (e.g. "workspace_id":{}) must NOT
# bypass the unbound-leaf check and render a partial argv.
jq -n '{direction:"right",workspace_id:{}}' > "$TMP/empty-obj.json"
render pane.split "$TMP/empty-obj.json" > "$TMP/empty-obj.out"
jq -e '.ok == false and ((.error.code == "unbound_parameter" and (.error.paths | index("workspace_id") != null)) or (.error.code == "invalid_answers" and (.error.message | contains("workspace_id"))))' "$TMP/empty-obj.out" >/dev/null

# Regression: argv elements containing newlines survive the argv handoff intact
# (base64 channel between jq and the shell, one element per line).
printf 'line one\nline two\n' > "$TMP/nl-text.txt"
jq -n --arg t "$(cat "$TMP/nl-text.txt")" '{target:"wTEST:p1",text:$t,wait:null}' > "$TMP/nl.json"
OUT=$(jq -s '{argv:["herdr","agent","prompt","--text",.[0].text]}' "$TMP/nl.json" | jq -r '.argv[] | @base64')
LAST=""
while IFS= read -r b64; do arg=$(printf '%s' "$b64" | base64 -d); LAST="$arg"; done <<< "$OUT"
[ "$LAST" = "$(cat "$TMP/nl-text.txt")" ]

# Reindex remains an explicit, noninteractive maintenance mode.
REINDEXED=$(bash "$DIR/api-browser.sh" --reindex)
[ -f "$REINDEXED" ]
for file in "$DIR"/*.sh; do bash -n "$file"; done
jq -e . "$DIR/bindings.json" >/dev/null
printf 'phase45 tests: PASS\n'
