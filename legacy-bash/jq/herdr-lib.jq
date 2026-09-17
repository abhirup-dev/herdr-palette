# Shared jq helpers for the Herdr schema-driven palette.
#
# Static graph model
# ------------------
# The generated index describes protocol affordances only: operation → typed
# parameter tree. It deliberately never contains session values such as pane
# IDs. `traverse.sh` obtains those separately from a live snapshot.
#
# Keep schema normalization here rather than shell snippets: jq has one
# portable interpretation on macOS, Linux, and Windows builds of the palette.

# Bump when the on-disk index representation changes. api-browser.sh includes
# this in current.json so older caches automatically rebuild.
def index_format: 6;

def ref_name: split("/") | last;

# Resolve local request-schema references. The depth guard makes recursive
# schemas safe: at the limit the traverser receives an opaque object instead
# of recursively expanding forever.
def resolve_ref($defs; $depth):
  if $depth <= 0 then .
  elif has("$ref") then ($defs[(."$ref" | ref_name)] | resolve_ref($defs; $depth - 1))
  else . end;

# JSON Schema represents nullable values as anyOf/type arrays in protocol 20.
def nullable:
  ((.type? | type == "array") and (.type | index("null")))
  or ((.anyOf? | type == "array") and any(.anyOf[]; .type? == "null"));

def non_null_shape:
  if (.anyOf? | type) == "array" then
    (.anyOf | map(select(.type? != "null")) | .[0] // .)
  elif (.type? | type) == "array" then
    . + {type: (.type | map(select(. != "null")) | .[0] // "string")}
  else . end;

# Turn JSON Schema into a compact, dependency-free node tree. This is cached;
# traversal never reparses `herdr api schema`.
def normalized_node($defs; $depth):
  . as $original
  | (resolve_ref($defs; $depth) | non_null_shape | resolve_ref($defs; $depth)) as $s
  | ($original | nullable) as $nullable
  | {
      description: ($s.description // null),
      default: ($s.default // null),
      nullable: $nullable
    }
  | if $depth <= 0 then . + {kind:"opaque"}
    elif ($s.oneOf? | type) == "array" then
      . + {
        kind: "union",
        variants: [ $s.oneOf[]
          | (resolve_ref($defs; $depth - 1) | non_null_shape) as $v
          | {
              tag: ($v.properties.type.const? // "unlabelled"),
              description: ($v.description // null),
              fields: ([ ($v.properties // {}) | to_entries[] as $field
                | select($field.key != "type")
                | {name: $field.key, required: (($v.required // []) | index($field.key) != null),
                   node: ($field.value | normalized_node($defs; $depth - 1))}
              ] | sort_by([if .required then 0 else 1 end, .name]))
            }
        ]
      }
    elif ($s.enum? | type) == "array" then . + {kind:"enum", values:$s.enum}
    elif (($s.properties? | type) == "object") then
      . + {
        kind:"object",
        fields:([ ($s.properties | to_entries[]) as $field | {
          name:$field.key,
          required:(($s.required // []) | index($field.key) != null),
          node:($field.value | normalized_node($defs; $depth - 1))
        }] | sort_by([if .required then 0 else 1 end, .name]))
      }
    elif ($s.type? == "array" or ($s.type | type) == "array") then
      . + {kind:"array", items:(($s.items // {}) | normalized_node($defs; $depth - 1))}
    else
      . + {kind:"scalar", type:($s.type // "string"), minimum:($s.minimum // null), maximum:($s.maximum // null)}
    end;

# Extract request operations. Event notification branches do not have a params
# reference and are excluded by this predicate.
def schema_operations:
  .schemas.request as $request
  | ($request."$defs") as $defs
  | [ $request.oneOf[]
      | select(.properties.method.const? and .properties.params."$ref"?)
      | {
          method:.properties.method.const,
          params_def:(.properties.params."$ref" | ref_name),
          params_schema:(.properties.params | resolve_ref($defs; 6))
        }
    ];

def method_description: split(".") | map(gsub("_"; " ")) | join(" · ");

def enum_values($defs):
  resolve_ref($defs; 12) | if (.enum? | type) == "array" then .enum else null end;

def union_summary($defs):
  resolve_ref($defs; 12) as $shape
  | if ($shape.oneOf? | type) == "array" then
      {ref:($shape.title // null), variants:[$shape.oneOf[] | .properties.type.const? // "unlabelled"]}
    else null end;

def param_shapes($defs):
  . as $params | ($params.required // []) as $required | ($params.properties // {}) as $properties
  | {required:($required | sort), optional:([$properties | keys[] as $name | select(($required | index($name)) | not) | $name] | sort),
     enums:(reduce ($properties | to_entries[]) as $field ({}; ($field.value | enum_values($defs)) as $values | if $values == null then . else . + {($field.key):$values} end)),
     unions:([$properties | to_entries[] | (.value | union_summary($defs)) as $union | if $union == null then empty else $union + {field:.key} end])};

# Index-only rendering helpers. The cache is trusted structured data, while
# callers treat these values as TSV fields for fzf.
def indexed_operation_lines: .operations[] | [.method, (.method + " · " + .description)] | @tsv;
def indexed_operation($method): .operations[] | select(.method == $method);
