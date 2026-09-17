# Convert a live `herdr api snapshot` response into picker rows.
#
# Output: opaque-id<TAB>human label. IDs are real-time values and are never
# written to the static protocol cache. Invoke with --arg source panes|tabs|
# workspaces|agents.

def clean_label: tostring | gsub("[\t\r\n]"; " ");

.result.snapshot as $s
| if $source == "panes" then
    $s.panes[]? | [ .pane_id, ((.title // .terminal_title_stripped // .pane_id | clean_label) + " (" + .pane_id + ")") ] | @tsv
  elif $source == "tabs" then
    $s.tabs[]? | [ .tab_id, ((.label // .tab_id | clean_label) + " (" + .tab_id + ")") ] | @tsv
  elif $source == "workspaces" then
    $s.workspaces[]? | [ .workspace_id, ((.label // .workspace_id | clean_label) + " (" + .workspace_id + ")") ] | @tsv
  elif $source == "agents" then
    $s.agents[]? | [ .pane_id, (((.title // .terminal_title_stripped // .pane_id) | clean_label) + " (" + .pane_id + ")") ] | @tsv
  else empty end
