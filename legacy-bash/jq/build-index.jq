# Build the static cache for one installed Herdr protocol schema.
#
# Input:  `herdr api schema --json`
# Output: a normalized operation graph. Runtime identifiers (panes, tabs,
# workspaces, agents) never belong here; they are resolved live by later phases.
#
# The caller supplies generation metadata because jq receives parsed JSON and
# therefore cannot preserve the original schema bytes for hashing.

include "herdr-lib";

.schemas.request."$defs" as $defs
| {
    metadata: {
      protocol: .protocol,
      schema_version: .schema_version,
      digest: $digest,
      generated_at: $generated_at,
      index_format: index_format
    },
    operations: (
      schema_operations
      | map(
          . as $operation
          | $operation.params_schema
          | {
              method: $operation.method,
              # Protocol 20 does not include operation descriptions. Keep a
              # deterministic human-readable fallback until a future protocol
              # adds them to operation branches.
              description: ($operation.method | method_description),
              params_def: $operation.params_def,
              param_shapes: param_shapes($defs),
              # Fully resolved bounded-depth tree used by traverse.sh. The raw
              # schema is deliberately not retained in the cache.
              param_tree: ($operation.params_schema | normalized_node($defs; 6))
            }
        )
      | sort_by(.method)
    )
  }
