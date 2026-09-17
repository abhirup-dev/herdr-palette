# Small runtime accessors used by traverse.sh.
# Keeping these operations here prevents the shell walker from growing inline jq
# programs. Each action has a narrow input/output contract documented below.

include "traverse-lib";

def path_parts($path): $path | split(".") | map(select(length > 0));

def runtime($action):
  if $action == "tree" then operation_tree($ARGS.named.method)
  elif $action == "compact" then .
  elif $action == "property" then .[$ARGS.named.property]
  elif $action == "field" then .fields[$ARGS.named.index]
  elif $action == "field-count" then (.fields | length)
  elif $action == "variant-count" then ([.variants[] | select(.tag == $ARGS.named.tag)][0].fields | length)
  elif $action == "variant-field" then ([.variants[] | select(.tag == $ARGS.named.tag)][0].fields[$ARGS.named.index])
  elif $action == "variant-tags" then .variants[].tag
  elif $action == "variant-values" then .values[]
  elif $action == "set-path" then setpath(path_parts($ARGS.named.path); $ARGS.named.value)
  elif $action == "append-string" then . + [$ARGS.named.value]
  elif $action == "string" then $ARGS.named.value
  else error("unknown traverse runtime action: " + $action) end;

runtime($ARGS.named.action)
