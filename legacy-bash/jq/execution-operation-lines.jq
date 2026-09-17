# Produce fzf rows for executable-operation selection.
# Input: generated protocol cache. Named arg: bindings (object).
# Unknown operations are API-only until a reviewed CLI binding is added.
include "herdr-lib";
.operations[]
| . as $operation
| ($ARGS.named.bindings[$operation.method].availability // "api-only") as $availability
| [$operation.method, ("[" + $availability + "] " + $operation.method + " · " + $operation.description)]
| @tsv
