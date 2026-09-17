#!/usr/bin/env bash
# herdr palette — interactive/compound actions that need input or confirmation.
# Invoked from the television channel manifest; runs inside the popup terminal.
set -u
HERDR_PALETTE_DIR="$(cd "$(dirname "$0")" && pwd)"

snap() { herdr api snapshot 2>/dev/null; }

# Active pane = pane the palette was invoked from (popup env), else focused.
active_pane() {
  local p=${HERDR_ACTIVE_PANE_ID:-}
  if [ -z "$p" ]; then
    p=$(snap | jq -r '.result.snapshot.focused_pane_id // empty')
  fi
  printf '%s' "$p"
}

# field-of-pane <field> — workspace_id / tab_id / cwd of the active pane
field_of_pane() {
  snap | jq -r --arg p "$(active_pane)" \
    '.result.snapshot.panes[]? | select(.pane_id == $p) | .'"$1"' // empty'
}

ask() { printf '%s' "$1"; IFS= read -r REPLY; printf '%s' "$REPLY"; }
confirm() {
  printf '%s [y/N]: ' "$1"
  local a; IFS= read -r a
  [ "$a" = "y" ] || [ "$a" = "Y" ]
}

cmd=${1:-}
shift || true
case "$cmd" in

  rename-pane)
    P=$(active_pane); [ -z "$P" ] && exit 1
    L=$(ask "new pane label (empty to clear): ")
    if [ -n "$L" ]; then herdr pane rename "$P" "$L"
    else herdr pane rename "$P" --clear; fi ;;

  rename-tab)
    T=$(field_of_pane tab_id); [ -z "$T" ] && exit 1
    L=$(ask "new tab label: "); [ -z "$L" ] && exit 0
    herdr tab rename "$T" "$L" ;;

  rename-workspace)
    W=$(field_of_pane workspace_id); [ -z "$W" ] && exit 1
    L=$(ask "new workspace label: "); [ -z "$L" ] && exit 0
    herdr workspace rename "$W" "$L" ;;

  move-new-tab)
    P=$(active_pane); [ -z "$P" ] && exit 1
    herdr pane move "$P" --new-tab --focus ;;

  move-new-workspace)
    P=$(active_pane); [ -z "$P" ] && exit 1
    L=$(ask "new workspace label (empty = default): ")
    if [ -n "$L" ]; then herdr pane move "$P" --new-workspace --label "$L" --focus
    else herdr pane move "$P" --new-workspace --focus; fi ;;

  move-to-tab)
    # pick a destination tab from the live list, then move the active pane there
    P=$(active_pane); [ -z "$P" ] && exit 1
    W=$(field_of_pane workspace_id)
    T=$(herdr tab list --workspace "$W" 2>/dev/null | jq -r '.result.tabs[] | "\(.tab_id)\t\(.label // .tab_id)"' \
       | awk -F'\t' '{printf "%s (%s)\n", $1, $2}' | fzf --header="move pane to which tab" | awk '{print $1}')
    [ -z "$T" ] && exit 0
    D=$(printf 'right\ndown\n' | fzf --header="split into $T in which direction")
    [ -z "$D" ] && exit 0
    herdr pane move "$P" --tab "$T" --split "$D" --focus ;;

  new-tab)
    D=$(field_of_pane cwd); L=$(ask "new tab label (empty = default): ")
    if [ -n "$L" ]; then herdr tab create --cwd "$D" --label "$L" --focus
    else herdr tab create --cwd "$D" --focus; fi ;;

  new-workspace)
    D=$(field_of_pane cwd); L=$(ask "new workspace label (empty = default): ")
    if [ -n "$L" ]; then herdr workspace create --cwd "$D" --label "$L" --focus
    else herdr workspace create --cwd "$D" --focus; fi ;;

  close-pane)
    P=$(active_pane); [ -z "$P" ] && exit 1
    confirm "close pane $P" && herdr pane close "$P" ;;

  close-tab)
    T=$(field_of_pane tab_id); [ -z "$T" ] && exit 1
    confirm "close tab $T" && herdr tab close "$T" ;;

  close-workspace)
    W=$(field_of_pane workspace_id); [ -z "$W" ] && exit 1
    confirm "close workspace $W" && herdr workspace close "$W" ;;

  close-pane-id)   # arg: pane id (from live section)
    confirm "close pane $1" && herdr pane close "$1" ;;

  close-tab-id)    # arg: tab id
    confirm "close tab $1" && herdr tab close "$1" ;;

  close-workspace-id)
    confirm "close workspace $1" && herdr workspace close "$1" ;;

  rename-agent)    # arg: pane id hosting agent
    L=$(ask "new agent name: "); [ -z "$L" ] && exit 0
    herdr agent rename "$1" "$L" ;;

  agent-prompt)    # arg: pane id — read text, prompt, wait for settle
    T=$(ask "prompt text: "); [ -z "$T" ] && exit 0
    herdr agent prompt "$1" "$T" --wait --timeout 300000 ;;

  send-key)        # arg: pane id, key
    herdr pane send-keys "$1" "$2" ;;

  agent-key)       # arg: pane id, key
    herdr agent send-keys "$1" "$2" ;;

  rename-workspace-id)  # arg: workspace id
    L=$(ask "new workspace label: "); [ -z "$L" ] && exit 0
    herdr workspace rename "$1" "$L" ;;

  rename-tab-id)        # arg: tab id
    L=$(ask "new tab label: "); [ -z "$L" ] && exit 0
    herdr tab rename "$1" "$L" ;;

  send-key-current)     # arg: key — send to the invoking pane
    P=$(active_pane); [ -z "$P" ] && exit 1
    herdr pane send-keys "$P" "$1" ;;

  *)
    echo "unknown action: $cmd" >&2; exit 2 ;;
esac
