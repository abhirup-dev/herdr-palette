// Package graph normalizes the installed Herdr JSON Schema into the palette's
// static affordance graph.
//
// Graph model
// -----------
// The generated graph describes protocol affordances only:
// operation -> typed parameter tree. It deliberately never stores runtime
// session values such as pane IDs, tabs, workspaces, or agents. Those are
// resolved separately from a live `herdr api snapshot` while traversing.
//
// The graph is intentionally a bounded, dependency-free JSON document so it
// can be cached once per installed protocol and consumed by the existing bash,
// jq, and future Go palette phases without reparsing `herdr api schema`.
package graph

import (
	"fmt"
	"sort"
	"strings"

	json "github.com/bytedance/sonic"
	"herdr-palette/internal/schema"
)

const IndexFormat = 6

// Index is the durable protocol graph cache written as protocol-N.json.
type Index struct {
	Metadata   Metadata    `json:"metadata"`
	Operations []Operation `json:"operations"`
}

type Metadata struct {
	Protocol      int    `json:"protocol"`
	SchemaVersion int    `json:"schema_version"`
	Digest        string `json:"digest"`
	GeneratedAt   string `json:"generated_at"`
	IndexFormat   int    `json:"index_format"`
}

// Operation is one invocable request branch. Notification/event branches are
// excluded because they do not carry a request params reference.
type Operation struct {
	Method      string      `json:"method"`
	Description string      `json:"description"`
	ParamsDef   string      `json:"params_def"`
	ParamShapes ParamShapes `json:"param_shapes"`
	ParamTree   Node        `json:"param_tree"`
}

type ParamShapes struct {
	Required []string         `json:"required"`
	Optional []string         `json:"optional"`
	Enums    map[string][]any `json:"enums"`
	Unions   []UnionSummary   `json:"unions"`
}

type UnionSummary struct {
	Ref      any      `json:"ref"`
	Variants []string `json:"variants"`
	Field    string   `json:"field"`
}

// Node is the recursively normalized JSON Schema type used by traversal.
// MarshalJSON emits exactly the historical jq graph representation: common
// fields plus only the properties meaningful for its kind.
type Node struct {
	Description any
	Default     any
	Nullable    bool
	Kind        string
	Variants    []Variant
	Values      []any
	Fields      []Field
	Items       *Node
	Type        string
	Minimum     any
	Maximum     any
}

type Variant struct {
	Tag         string  `json:"tag"`
	Description any     `json:"description"`
	Fields      []Field `json:"fields"`
}

type Field struct {
	Name     string `json:"name"`
	Required bool   `json:"required"`
	Node     Node   `json:"node"`
}

func (n Node) MarshalJSON() ([]byte, error) {
	out := map[string]any{
		"description": n.Description,
		"default":     n.Default,
		"nullable":    n.Nullable,
		"kind":        n.Kind,
	}
	switch n.Kind {
	case "union":
		out["variants"] = n.Variants
	case "enum":
		out["values"] = n.Values
	case "object":
		out["fields"] = n.Fields
	case "array":
		out["items"] = n.Items
	case "scalar":
		out["type"] = n.Type
		out["minimum"] = n.Minimum
		out["maximum"] = n.Maximum
	}
	return json.Marshal(out)
}

// Build converts request operations in doc into the historical jq graph shape.
func Build(doc *schema.Document, digest, generatedAt string) (Index, error) {
	request := doc.Request()
	defs, ok := object(request["$defs"])
	if !ok {
		return Index{}, fmt.Errorf("request schema has no $defs")
	}
	branches, ok := array(request["oneOf"])
	if !ok {
		return Index{}, fmt.Errorf("request schema has no oneOf branches")
	}

	operations := make([]Operation, 0, len(branches))
	for _, rawBranch := range branches {
		branch, ok := object(rawBranch)
		if !ok {
			continue
		}
		properties, ok := object(branch["properties"])
		if !ok {
			continue
		}
		methodObject, _ := object(properties["method"])
		paramsObject, _ := object(properties["params"])
		method, methodOK := stringValue(methodObject["const"])
		ref, refOK := stringValue(paramsObject["$ref"])
		// This is intentionally the same predicate as jq's schema_operations:
		// event subscription references have a method const but no params $ref.
		if !methodOK || !refOK {
			continue
		}
		paramsDef := refName(ref)
		paramsSchema, ok := resolveRef(paramsObject, defs, 6)
		if !ok {
			return Index{}, fmt.Errorf("%s: unresolved params reference %q", method, ref)
		}
		shapes := makeParamShapes(paramsSchema, defs)
		operations = append(operations, Operation{
			Method: method, Description: methodDescription(method), ParamsDef: paramsDef,
			ParamShapes: shapes, ParamTree: normalizeNode(paramsSchema, defs, 6),
		})
	}
	sort.Slice(operations, func(i, j int) bool { return operations[i].Method < operations[j].Method })
	return Index{Metadata: Metadata{Protocol: doc.Protocol, SchemaVersion: doc.SchemaVersion, Digest: digest, GeneratedAt: generatedAt, IndexFormat: IndexFormat}, Operations: operations}, nil
}

func object(v any) (map[string]any, bool) { x, ok := v.(map[string]any); return x, ok }
func array(v any) ([]any, bool)           { x, ok := v.([]any); return x, ok }
func stringValue(v any) (string, bool)    { x, ok := v.(string); return x, ok }
func refName(ref string) string           { pieces := strings.Split(ref, "/"); return pieces[len(pieces)-1] }

func resolveRef(raw map[string]any, defs map[string]any, depth int) (map[string]any, bool) {
	if depth <= 0 {
		return raw, true
	}
	ref, hasRef := stringValue(raw["$ref"])
	if !hasRef {
		return raw, true
	}
	target, ok := object(defs[refName(ref)])
	if !ok {
		return nil, false
	}
	return resolveRef(target, defs, depth-1)
}

func nullable(raw map[string]any) bool {
	if types, ok := array(raw["type"]); ok {
		for _, typ := range types {
			if typ == "null" {
				return true
			}
		}
	}
	if choices, ok := array(raw["anyOf"]); ok {
		for _, choice := range choices {
			if obj, ok := object(choice); ok && obj["type"] == "null" {
				return true
			}
		}
	}
	return false
}

// nonNullShape exactly follows jq's non_null_shape helper.
func nonNullShape(raw map[string]any) map[string]any {
	if choices, ok := array(raw["anyOf"]); ok {
		for _, choice := range choices {
			if obj, ok := object(choice); ok && obj["type"] != "null" {
				return obj
			}
		}
		return raw
	}
	if types, ok := array(raw["type"]); ok {
		copy := cloneObject(raw)
		for _, typ := range types {
			if typ != "null" {
				copy["type"] = typ
				return copy
			}
		}
		copy["type"] = "string"
		return copy
	}
	return raw
}

func normalizeShape(raw map[string]any, defs map[string]any, depth int) map[string]any {
	resolved, _ := resolveRef(raw, defs, depth)
	nonNull := nonNullShape(resolved)
	resolvedAgain, _ := resolveRef(nonNull, defs, depth)
	return resolvedAgain
}

func normalizeNode(raw map[string]any, defs map[string]any, depth int) Node {
	shape := normalizeShape(raw, defs, depth)
	node := Node{Description: jqAlternative(shape["description"]), Default: jqAlternative(shape["default"]), Nullable: nullable(raw)}
	if depth <= 0 {
		node.Kind = "opaque"
		return node
	}
	if variants, ok := array(shape["oneOf"]); ok {
		node.Kind = "union"
		for _, rawVariant := range variants {
			variantObj, _ := object(rawVariant)
			resolved, _ := resolveRef(variantObj, defs, depth-1)
			variant := nonNullShape(resolved)
			props, _ := object(variant["properties"])
			tag := "unlabelled"
			if typeObj, ok := object(props["type"]); ok {
				if value, ok := stringValue(typeObj["const"]); ok {
					tag = value
				}
			}
			node.Variants = append(node.Variants, Variant{Tag: tag, Description: jqAlternative(variant["description"]), Fields: normalizeFields(props, requiredSet(variant), defs, depth-1, true)})
		}
		return node
	}
	if values, ok := array(shape["enum"]); ok {
		node.Kind = "enum"
		node.Values = values
		return node
	}
	if props, ok := object(shape["properties"]); ok {
		node.Kind = "object"
		node.Fields = normalizeFields(props, requiredSet(shape), defs, depth-1, false)
		return node
	}
	if typ, isArray := stringValue(shape["type"]); isArray && typ == "array" || isTypeArray(shape["type"]) {
		node.Kind = "array"
		items, _ := object(shape["items"])
		itemNode := normalizeNode(items, defs, depth-1)
		node.Items = &itemNode
		return node
	}
	node.Kind = "scalar"
	node.Type, _ = stringValue(shape["type"])
	if node.Type == "" {
		node.Type = "string"
	}
	node.Minimum = jqAlternative(shape["minimum"])
	node.Maximum = jqAlternative(shape["maximum"])
	return node
}

func isTypeArray(v any) bool { _, ok := array(v); return ok }

func normalizeFields(props map[string]any, required map[string]bool, defs map[string]any, depth int, skipType bool) []Field {
	fields := make([]Field, 0, len(props))
	for name, raw := range props {
		if skipType && name == "type" {
			continue
		}
		value, _ := object(raw)
		fields = append(fields, Field{Name: name, Required: required[name], Node: normalizeNode(value, defs, depth)})
	}
	sort.Slice(fields, func(i, j int) bool {
		if fields[i].Required != fields[j].Required {
			return fields[i].Required
		}
		return fields[i].Name < fields[j].Name
	})
	return fields
}

func requiredSet(obj map[string]any) map[string]bool {
	out := map[string]bool{}
	if values, ok := array(obj["required"]); ok {
		for _, value := range values {
			if s, ok := stringValue(value); ok {
				out[s] = true
			}
		}
	}
	return out
}

func makeParamShapes(params map[string]any, defs map[string]any) ParamShapes {
	requiredSet := requiredSet(params)
	required, optional := []string{}, []string{}
	props, _ := object(params["properties"])
	enums := map[string][]any{}
	unions := []UnionSummary{}
	for name, raw := range props {
		if requiredSet[name] {
			required = append(required, name)
		} else {
			optional = append(optional, name)
		}
		field, _ := object(raw)
		if values := enumValues(field, defs); values != nil {
			enums[name] = values
		}
		if summary := unionSummary(field, defs); summary != nil {
			summary.Field = name
			unions = append(unions, *summary)
		}
	}
	sort.Strings(required)
	sort.Strings(optional)
	return ParamShapes{Required: required, Optional: optional, Enums: enums, Unions: unions}
}

func enumValues(raw map[string]any, defs map[string]any) []any {
	shape, _ := resolveRef(raw, defs, 12)
	values, _ := array(shape["enum"])
	return values
}
func unionSummary(raw map[string]any, defs map[string]any) *UnionSummary {
	shape, _ := resolveRef(raw, defs, 12)
	branches, ok := array(shape["oneOf"])
	if !ok {
		return nil
	}
	variants := make([]string, 0, len(branches))
	for _, branch := range branches {
		obj, _ := object(branch)
		props, _ := object(obj["properties"])
		typ, _ := object(props["type"])
		tag, ok := stringValue(typ["const"])
		if !ok {
			tag = "unlabelled"
		}
		variants = append(variants, tag)
	}
	return &UnionSummary{Ref: jqAlternative(shape["title"]), Variants: variants}
}
func methodDescription(method string) string {
	parts := strings.Split(method, ".")
	for i := range parts {
		parts[i] = strings.ReplaceAll(parts[i], "_", " ")
	}
	return strings.Join(parts, " · ")
}

// jqAlternative mirrors jq's `value // null`: false and null become null.
// This preserves the existing cache precisely (notably JSON Schema default:false).
func jqAlternative(value any) any {
	if value == nil {
		return nil
	}
	if boolean, ok := value.(bool); ok && !boolean {
		return nil
	}
	return value
}

func cloneObject(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
