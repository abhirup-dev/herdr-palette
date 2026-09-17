# Helpers for traverse.sh. Input is the generated static protocol index unless
# stated otherwise. Live data lives in snapshot-values.jq, never in this file.

include "herdr-lib";

# Return the normalized root object for a cached operation, failing clearly for
# missing methods or an index built by an older palette representation.
def operation_tree($method):
  indexed_operation($method).param_tree
  // error("operation is absent or cache lacks param_tree; reopen api browser to rebuild cache");

# Required fields first preserves protocol semantics and makes both interactive
# and --answers traversal deterministic.
def ordered_fields:
  [.fields[] | select(.required)] + [.fields[] | select(.required | not)];

# Safely retrieve an answer at one dotted path. `getpath` handles nested
# objects without eval or shell interpolation.
def answer_at($path):
  getpath($path | split(".") | map(select(length > 0))) // null;

# Serialize the final answer tree as stable, readable JSON.
def pretty_answers: .;
