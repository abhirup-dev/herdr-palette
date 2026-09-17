# Emit reviewed CLI binding coverage rows for tests/release checks.
# Input: bindings.json. Each row is method<TAB>command words<TAB>US-separated
# documented flags. API-only methods are emitted with the literal api-only.
def flags($params):
  [$params | to_entries[] | .value as $spec
   | if $spec.flag? then $spec.flag
     elif $spec.presence_flag? then $spec.presence_flag
     elif $spec.boolean? then $spec.boolean[]
     elif $spec.value_flags? then $spec.value_flags[] | .[]
     else empty end]
  | unique | join("\u001f");
to_entries[]
| [.key, .value.availability, (.value.argv | join(" ")), (flags(.value.params))]
| @tsv
