# Render one validated answer tree into a Herdr CLI argv array.
#
# Input: answer JSON object. Named args: method, bindings, tree.
# Output: {ok:true, argv:[strings...]} or {ok:false, error:{...}}.
# This never emits a partial argv: every answer leaf must have a binding and
# every required schema field is validated before rendering.

include "validate-answers";

def path_parts($path): $path | split(".") | map(select(length > 0));
def has_path($value; $path):
  try ($value | getpath(path_parts($path)) | . != null) catch false;
def string_value($spec; $value):
  ($value | tostring) as $raw | ($spec.values[$raw] // $raw);

def leaf_entries($value; $prefix):
  # An empty object/array is still a provided value: emit it as a leaf so the
  # unbound-parameter check cannot be bypassed with `{}` (regression: a
  # pane.split answer carrying "workspace_id":{} once rendered a partial argv).
  if ($value | type) == "object" and ($value | length) > 0 then
    [ $value | to_entries[] as $entry
      | leaf_entries($entry.value; if $prefix == "" then $entry.key else $prefix + "." + $entry.key end)[] ]
  else [{path:$prefix, value:$value}] end;
def answer_leaves: leaf_entries(.; "");

def argv_for_value($spec; $value):
  if ($spec.positional? != null) then [string_value($spec; $value)]
  elif ($spec.boolean? != null) then
    ($spec.boolean[($value | tostring)] // empty) as $flag | if $flag == "" then [] else [$flag] end
  elif ($spec.value_flags? != null) then $spec.value_flags[($value | tostring)]
  elif ($spec.flag? != null) then
    if ($spec.repeat? == true) and ($value | type) == "array" then
      [$value[] | [$spec.flag, string_value($spec; .)][]]
    else [$spec.flag, string_value($spec; $value)] end
  else [] end;

def render($method; $bindings; $tree):
  . as $input
  | (try (validate_answers($tree; $input) | {valid:true, answers:.})
     catch {valid:false, message:.}) as $checked
  | if $checked.valid | not then
      {ok:false, error:{code:"invalid_answers", message:$checked.message}}
    elif ($bindings[$method] // null) == null then
      {ok:false, error:{code:"unbound_operation", method:$method,
                         message:"no CLI binding is registered for this operation"}}
    else ($bindings[$method]) as $binding
      | if $binding.availability != "cli" then
          {ok:false, error:{code:"api_only", method:$method,
                            message:"API-only (no CLI transport registered)"}}
        else ($checked.answers) as $answers
          | ($answers | answer_leaves) as $leaves
          | [$leaves[] | select(($binding.params[.path] // null) == null) | .path] | unique as $unbound
          | if ($unbound | length) > 0 then
              {ok:false, error:{code:"unbound_parameter", method:$method, paths:$unbound,
                                message:"answers include parameter paths without CLI bindings"}}
            else
              ([$leaves[] | {path:.path, value:.value, spec:$binding.params[.path]}]) as $bound
              | [$bound[] | select(.spec.value_flags? != null and (.spec.value_flags[(.value | tostring)] // null) == null)
                 | {path:.path, value:.value}] as $unknown_values
              | if ($unknown_values | length) > 0 then
                  {ok:false, error:{code:"unrenderable_value", method:$method, values:$unknown_values}}
                else
                  ([$bound[] | select(.spec.positional? != null) | {order:.spec.positional, value:.value, spec:.spec}]
                   | sort_by(.order) | map(argv_for_value(.spec; .value)) | add // []) as $positionals
                  | ([$bound[] | select(.spec.positional? == null) | argv_for_value(.spec; .value)] | add // []) as $flags
                  | ([$binding.params | to_entries[]
                      | select(.value.presence_flag? != null and has_path($answers; .key))
                      | .value.presence_flag]) as $presence_flags
                  | {ok:true, argv:(["herdr"] + $binding.argv + $positionals + $presence_flags + $flags)}
                end
            end
        end
    end;

render($ARGS.named.method; $ARGS.named.bindings; $ARGS.named.tree)
