# Validate a non-interactive answer tree against a normalized cache node.
# Inputs: --argjson tree <node>, --argjson answers <object>. Output is the
# answers tree unchanged on success. This keeps --answers deterministic and
# makes traversal testable without fzf or a tty.

# Path arrays are used internally so dotted keys remain a presentation choice.
def fail_at($path; $message): error(($path | if length == 0 then "parameters" else join(".") end) + ": " + $message);
def present($obj; $name):
  ($obj | type == "object") and ($obj | has($name)) and ($obj[$name] != null);

def validate_node($node; $value; $path):
  if $node.kind == "enum" then
    if ($node.values | index($value)) == null then fail_at($path; "expected one of " + ($node.values | join(", "))) else . end
  elif $node.kind == "union" then
    if ($value | type) != "object" then fail_at($path; "expected object with type discriminator")
    else ($value.type // null) as $tag
      | ([$node.variants[] | select(.tag == $tag)][0]) as $variant
      | if $variant == null then fail_at($path; "unknown variant " + ($tag // "null"))
        else reduce $variant.fields[] as $field (.;
          if $field.required and (present($value; $field.name) | not) then fail_at($path + [$field.name]; "required")
          elif present($value; $field.name) then validate_node($field.node; $value[$field.name]; $path + [$field.name])
          else . end)
        end
    end
  elif $node.kind == "object" then
    if ($value | type) != "object" then fail_at($path; "expected object")
    else reduce $node.fields[] as $field (.;
      if $field.required and (present($value; $field.name) | not) then fail_at($path + [$field.name]; "required")
      elif present($value; $field.name) then validate_node($field.node; $value[$field.name]; $path + [$field.name])
      else . end)
    end
  elif $node.kind == "array" then
    if ($value | type) != "array" then fail_at($path; "expected array")
    else reduce range(0; $value | length) as $i (.;
      validate_node($node.items; $value[$i]; $path + [$i | tostring]))
    end
  elif $node.kind == "scalar" then
    if $node.type == "boolean" and ($value | type) != "boolean" then fail_at($path; "expected boolean")
    elif ($node.type == "integer") and (($value | type) != "number" or ($value | floor) != $value) then fail_at($path; "expected integer")
    elif ($node.type == "number") and ($value | type) != "number" then fail_at($path; "expected number")
    elif ($node.type == "string") and ($value | type) != "string" then fail_at($path; "expected string")
    elif ($node.minimum != null) and ($value < $node.minimum) then fail_at($path; "below minimum " + ($node.minimum | tostring))
    elif ($node.maximum != null) and ($value > $node.maximum) then fail_at($path; "above maximum " + ($node.maximum | tostring))
    else . end
  else . end;

def validate_answers($tree; $answers):
  validate_node($tree; $answers; []) | $answers;
