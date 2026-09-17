#!/usr/bin/env bash
# herdr palette — preview command for the `tv herdr` channel.
# Arg 1 is the full command string from the manifest. For entries targeting
# an agent or pane, show live output; otherwise show what will run.
set -u
cmd=${1:-}

pane=$(printf '%s' "$cmd" | grep -oE '[A-Za-z0-9]+:p[0-9]+' | head -1)

if [ -n "$pane" ]; then
  if printf '%s' "$cmd" | grep -q 'agent'; then
    herdr agent read "$pane" --source recent-unwrapped --lines 40 2>&1
  else
    herdr pane read "$pane" --source recent-unwrapped --lines 40 2>&1
  fi
  exit 0
fi

# Generic: show the command plus the subcommand's help.
printf '$ %s\n\n' "$cmd"
sub=$(printf '%s' "$cmd" | awk '{print $2}')
[ -n "$sub" ] && herdr "$sub" --help 2>&1 | head -40
exit 0
