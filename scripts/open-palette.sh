#!/bin/sh
# Open the herdr palette popup pane (plugin-owned).
set -eu
herdr_bin="${HERDR_BIN_PATH:-herdr}"
exec "$herdr_bin" plugin pane open \
  --plugin herdr-palette \
  --entrypoint palette \
  --focus
