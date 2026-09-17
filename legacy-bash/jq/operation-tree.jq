# Return only the normalized parameter tree for one cached operation.
# Input: generated protocol cache. Named arg: method.
include "herdr-lib";
(indexed_operation($ARGS.named.method) // error("unknown cached operation: " + $ARGS.named.method)).param_tree
