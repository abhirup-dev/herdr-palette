#!/usr/bin/env bash
# herdr command palette — entry source for the `tv herdr` channel.
# Emits one entry per line: LABEL<TAB>COMMAND.
# Layout: current-context action catalog first (default view), live targets after.
# A JSONL selection log may add up to eight ★ ranked duplicates before that
# catalog; the original catalog order and all entries always remain intact.

CATALOG=$(mktemp "${TMPDIR:-/tmp}/herdr-palette-catalog.XXXXXX")
trap 'rm -f "$CATALOG"' EXIT
emit() { printf '%s\t%s\n' "$1" "$2" >> "$CATALOG"; }
clean() { printf '%s' "$1" | tr -d "'\"\t"; }
ACT="bash $HOME/.config/herdr/palette/act.sh"
DIR=$(cd "$(dirname "$0")" && pwd)
CACHE_ROOT=${HERDR_PALETTE_CACHE_DIR:-"${XDG_CACHE_HOME:-$HOME/.cache}/herdr-palette"}
SELECTION_LOG="$CACHE_ROOT/selections.jsonl"

SNAP=$(herdr api snapshot 2>/dev/null)
[ -z "$SNAP" ] && SNAP='{"result":{"snapshot":{}}}'

ACTIVE_PANE=${HERDR_ACTIVE_PANE_ID:-}
[ -z "$ACTIVE_PANE" ] && ACTIVE_PANE=$(printf '%s' "$SNAP" | jq -r '.result.snapshot.focused_pane_id // empty')

# ============ SECTION 1: actions on the CURRENT pane / tab / workspace ========

P="[pane]"
emit "$P zoom toggle"            "herdr pane zoom --current --toggle"
emit "$P zoom on"                "herdr pane zoom --current --on"
emit "$P zoom off"               "herdr pane zoom --current --off"
emit "$P split right (keep focus)" "herdr pane split --current --direction right --no-focus"
emit "$P split right (focus new)"   "herdr pane split --current --direction right --focus"
emit "$P split down (keep focus)"   "herdr pane split --current --direction down --no-focus"
emit "$P split down (focus new)"    "herdr pane split --current --direction down --focus"
emit "$P resize ←"               "herdr pane resize --current --direction left --amount 0.05"
emit "$P resize →"               "herdr pane resize --current --direction right --amount 0.05"
emit "$P resize ↑"               "herdr pane resize --current --direction up --amount 0.05"
emit "$P resize ↓"               "herdr pane resize --current --direction down --amount 0.05"
emit "$P swap ←"                 "herdr pane swap --current --direction left"
emit "$P swap →"                 "herdr pane swap --current --direction right"
emit "$P swap ↑"                 "herdr pane swap --current --direction up"
emit "$P swap ↓"                 "herdr pane swap --current --direction down"
emit "$P move → new tab"         "$ACT move-new-tab"
emit "$P move → new workspace"   "$ACT move-new-workspace"
emit "$P move → existing tab…"   "$ACT move-to-tab"
emit "$P rename…"                "$ACT rename-pane"
emit "$P close…"                 "$ACT close-pane"
emit "$P send Esc"               "$ACT send-key-current esc"
emit "$P send Ctrl+C"            "$ACT send-key-current ctrl+c"
emit "$P info: layout"           "herdr pane layout --current"
emit "$P info: process"          "herdr pane process-info --current"
emit "$P info: edges"            "herdr pane edges --current"
emit "$P info: full get"         "herdr pane current --current"

T="[tab]"
emit "$T rename…"                "$ACT rename-tab"
emit "$T new tab (cwd = here)"   "$ACT new-tab"
emit "$T close…"                 "$ACT close-tab"

W="[workspace]"
emit "$W rename…"                "$ACT rename-workspace"
emit "$W new workspace (cwd = here)" "$ACT new-workspace"
emit "$W close…"                 "$ACT close-workspace"

S="[server/session]"
emit "$S reload config"          "herdr server reload-config"
emit "$S status"                 "herdr status server"
emit "$S sessions list"          "herdr session list"
emit "$S worktree list"          "herdr worktree list"
emit "$S notifications show"     "herdr notification show"

# ============ SECTION 2: live targets (agents / panes / tabs / workspaces) =====

# --- agents ---
printf '%s' "$SNAP" | jq -r '
  .result.snapshot.agents[]? |
  [(.title // .terminal_title_stripped // .pane_id), .agent, .agent_status, .pane_id, .workspace_id] | @tsv' |
while IFS=$'\t' read -r title kind state pane ws; do
  [ -z "$pane" ] && continue
  title=$(clean "$title")
  tag="[agent $kind $state]"
  emit "$tag focus · $title"     "herdr agent focus $pane"
  emit "$tag read · $title"      "herdr agent read $pane --lines 60"
  emit "$tag attach · $title"    "herdr agent attach $pane"
  emit "$tag prompt · $title"    "$ACT agent-prompt $pane"
  emit "$tag rename · $title"    "$ACT rename-agent $pane"
  emit "$tag send Esc · $title"  "$ACT agent-key $pane esc"
  emit "$tag send Ctrl+C · $title" "$ACT agent-key $pane ctrl+c"
  emit "$tag explain detection · $title" "herdr agent explain $pane"
done

# --- workspaces ---
printf '%s' "$SNAP" | jq -r '
  .result.snapshot.workspaces[]? |
  [(.label // .workspace_id), .workspace_id, (.focused // false | tostring), (.pane_count // 0 | tostring)] | @tsv' |
while IFS=$'\t' read -r label ws focused panes; do
  label=$(clean "$label")
  star=""; [ "$focused" = "true" ] && star=", focused"
  emit "[workspace] goto · $label (${panes} panes$star)"  "herdr workspace focus $ws"
  emit "[workspace] rename · $label"                      "$ACT rename-workspace-id $ws"
  emit "[workspace] close · ${label}…"                  "$ACT close-workspace-id $ws"
done

# --- tabs ---
printf '%s' "$SNAP" | jq -r '
  .result.snapshot.tabs[]? |
  [(.label // .tab_id), .tab_id, .workspace_id, (.pane_count // 0 | tostring)] | @tsv' |
while IFS=$'\t' read -r label tab ws panes; do
  label=$(clean "$label")
  emit "[tab] goto · $ws / $label (${panes} panes)"  "herdr tab focus $tab"
  emit "[tab] rename · $ws / ${label}…"              "$ACT rename-tab-id $tab"
  emit "[tab] close · $ws / ${label}…"               "$ACT close-tab-id $tab"
done

# --- panes ---
printf '%s' "$SNAP" | jq -r '
  .result.snapshot.panes[]? |
  [(.title // .terminal_title_stripped // .pane_id), .pane_id, .workspace_id] | @tsv' |
while IFS=$'\t' read -r title pane ws; do
  title=$(clean "$title")
  mark=""; [ "$pane" = "$ACTIVE_PANE" ] && mark=", active"
  emit "[pane] zoom · $title ($ws/$pane$mark)"      "herdr pane zoom $pane --toggle"
  emit "[pane] read · $title ($ws/$pane$mark)"      "herdr pane read $pane --lines 60"
  emit "[pane] close · $title ($ws/$pane$mark)…"    "$ACT close-pane-id $pane"
done

# ============ SECTION 3: raw API introspection ================================

emit "[api] browse all operations…" "bash $HOME/.config/herdr/palette/api-browser.sh"
emit "[api] execute operation…" "bash $HOME/.config/herdr/palette/execute.sh"
emit "[api] snapshot"  "herdr api snapshot"
emit "[api] schema"    "herdr api schema --json"

# History is optional and deliberately independent from the static protocol
# index. An empty/missing log produces no rows, preserving the catalog exactly.
if [ -s "$SELECTION_LOG" ]; then
  jq -R -s -r --rawfile catalog "$CATALOG" -L "$DIR/jq" \
    'include "rank-lib"; ranked_rows($catalog)' "$SELECTION_LOG" 2>/dev/null || true
fi
cat "$CATALOG"
