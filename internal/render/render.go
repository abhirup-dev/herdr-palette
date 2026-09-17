// Package render ports jq/render-argv.jq and jq/validate-answers.jq semantics
// exactly: validate an answer tree against the cached param tree, then render
// a Herdr CLI argv. Like the jq renderer, this never emits a partial argv —
// every answer leaf must have a binding and every required field is validated
// first. The argv stays a []string end to end; no shell command string is
// assembled here.
package render

import (
	"fmt"
	"sort"
	"strings"

	"herdr-palette/internal/bindings"
	"herdr-palette/internal/graph"
	"herdr-palette/internal/traverse"
)

// Error codes mirror the jq renderer's error.code values one-for-one.
const (
	CodeInvalidAnswers   = "invalid_answers"
	CodeUnboundOperation = "unbound_operation"
	CodeAPIOnly          = "api_only"
	CodeUnboundParameter = "unbound_parameter"
	CodeUnrenderable     = "unrenderable_value"
)

// Error is the typed render failure. Paths/Values mirror the jq payloads.
type Error struct {
	Code    string      `json:"code"`
	Method  string      `json:"method,omitempty"`
	Paths   []string    `json:"paths,omitempty"`
	Values  []pathValue `json:"values,omitempty"`
	Message string      `json:"message"`
}

type pathValue struct {
	Path  string `json:"path"`
	Value any    `json:"value"`
}

func (e *Error) Error() string { return e.Message }

func errf(code, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// ---------- validation (validate-answers.jq parity) ----------

func failAt(path []string, format string, args ...any) error {
	name := "parameters"
	if len(path) > 0 {
		name = strings.Join(path, ".")
	}
	return fmt.Errorf("%s: %s", name, fmt.Sprintf(format, args...))
}

func present(obj map[string]any, name string) bool {
	v, ok := obj[name]
	return ok && v != nil
}

func numLike(v any) (float64, bool) {
	f, ok := v.(float64)
	return f, ok
}

func validateNode(node graph.Node, value any, path []string) error {
	switch node.Kind {
	case "enum":
		for _, want := range node.Values {
			if fmt.Sprint(want) == fmt.Sprint(value) {
				return nil
			}
		}
		vals := make([]string, len(node.Values))
		for i, v := range node.Values {
			vals[i] = fmt.Sprint(v)
		}
		return failAt(path, "expected one of %s", strings.Join(vals, ", "))
	case "union":
		obj, ok := value.(map[string]any)
		if !ok {
			return failAt(path, "expected object with type discriminator")
		}
		tagAny, hasTag := obj["type"]
		tag := fmt.Sprint(tagAny)
		var variant *graph.Variant
		for i := range node.Variants {
			if node.Variants[i].Tag == tag {
				variant = &node.Variants[i]
				break
			}
		}
		if variant == nil {
			if !hasTag {
				tag = "null"
			}
			return failAt(path, "unknown variant %s", tag)
		}
		return validateFields(variant.Fields, obj, path)
	case "object":
		obj, ok := value.(map[string]any)
		if !ok {
			return failAt(path, "expected object")
		}
		return validateFields(node.Fields, obj, path)
	case "array":
		arr, ok := value.([]any)
		if !ok {
			return failAt(path, "expected array")
		}
		for i := range arr {
			if err := validateNode(*node.Items, arr[i], append(path, fmt.Sprint(i))); err != nil {
				return err
			}
		}
		return nil
	case "scalar":
		return validateScalar(node, value, path)
	}
	return nil
}

func validateFields(fields []graph.Field, obj map[string]any, path []string) error {
	for _, f := range fields {
		switch {
		case f.Required && !present(obj, f.Name):
			return failAt(append(path, f.Name), "required")
		case present(obj, f.Name):
			if err := validateNode(f.Node, obj[f.Name], append(path, f.Name)); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateScalar(node graph.Node, value any, path []string) error {
	switch node.Type {
	case "boolean":
		if _, ok := value.(bool); !ok {
			return failAt(path, "expected boolean")
		}
	case "integer":
		f, ok := numLike(value)
		if !ok || f != float64(int64(f)) {
			return failAt(path, "expected integer")
		}
	case "number":
		if _, ok := numLike(value); !ok {
			return failAt(path, "expected number")
		}
	case "string":
		if _, ok := value.(string); !ok {
			return failAt(path, "expected string")
		}
	}
	if node.Minimum != nil {
		if f, ok := numLike(value); ok && f < toFloat(node.Minimum) {
			return failAt(path, "below minimum %v", node.Minimum)
		}
	}
	if node.Maximum != nil {
		if f, ok := numLike(value); ok && f > toFloat(node.Maximum) {
			return failAt(path, "above maximum %v", node.Maximum)
		}
	}
	return nil
}

func toFloat(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	}
	return 0
}

// Validate checks answers against the param tree (jq validate_answers parity).
func Validate(tree graph.Node, answers map[string]any) error {
	return validateNode(tree, answers, nil)
}

// ---------- leaf collection (answer_leaves parity, incl. the {} bypass fix) ----------

type leaf struct {
	path  string
	value any
}

// leafEntries mirrors jq leaf_entries: an empty object/array is still a
// provided value and is emitted as a leaf, so "workspace_id":{} cannot bypass
// the unbound-parameter check (regression guarded in tests/phase45.sh).
// When value is an ordered document object (from traverse.DecodeOrdered),
// keys are walked in insertion order — jq to_entries parity — which decides
// flag order in the rendered argv.
func leafEntries(value any, prefix string) []leaf {
	if keys := traverse.OrderedKeys(value); keys != nil {
		var out []leaf
		for _, k := range keys {
			v, _ := traverse.OrderedGet(value, k)
			p := k
			if prefix != "" {
				p = prefix + "." + k
			}
			out = append(out, leafEntries(v, p)...)
		}
		return out
	}
	if obj, ok := value.(map[string]any); ok && len(obj) > 0 {
		var out []leaf
		keys := make([]string, 0, len(obj))
		for k := range obj {
			keys = append(keys, k)
		}
		sort.Strings(keys) // unordered map fallback: deterministic
		for _, k := range keys {
			p := k
			if prefix != "" {
				p = prefix + "." + k
			}
			out = append(out, leafEntries(obj[k], p)...)
		}
		return out
	}
	return []leaf{{path: prefix, value: value}}
}

// ---------- rendering (render-argv.jq parity) ----------

// stringValue mirrors jq string_value: map the value through spec.values if
// present, else its string form.
func stringValue(spec bindings.ParamSpec, value any) string {
	raw := fmt.Sprint(valueToPrint(value))
	if spec.Values != nil {
		if m, ok := spec.Values[raw]; ok {
			return m
		}
	}
	return raw
}

// valueToPrint renders JSON scalars the way jq tostring does.
func valueToPrint(v any) any {
	switch t := v.(type) {
	case string:
		return t
	case bool:
		if t {
			return "true"
		}
		return "false"
	case float64:
		if t == float64(int64(t)) {
			return fmt.Sprintf("%d", int64(t))
		}
		return fmt.Sprintf("%g", t)
	case nil:
		return "null"
	}
	return v
}

// argvForValue mirrors jq argv_for_value.
func argvForValue(spec bindings.ParamSpec, value any) []string {
	switch {
	case spec.Positional != nil:
		return []string{stringValue(spec, value)}
	case spec.Boolean != nil:
		key := fmt.Sprint(valueToPrint(value))
		if flag, ok := spec.Boolean[key]; ok && flag != "" {
			return []string{flag}
		}
		return nil
	case spec.ValueFlags != nil:
		key := fmt.Sprint(valueToPrint(value))
		return append([]string(nil), spec.ValueFlags[key]...)
	case spec.Flag != "":
		if spec.Repeat {
			if arr, ok := value.([]any); ok {
				var out []string
				for _, item := range arr {
					out = append(out, spec.Flag, stringValue(spec, item))
				}
				return out
			}
		}
		return []string{spec.Flag, stringValue(spec, value)}
	}
	return nil
}

// hasPath mirrors jq has_path: true when the dotted path resolves non-null.
func hasPath(value map[string]any, dotted string) bool {
	cur := any(value)
	for _, part := range strings.Split(dotted, ".") {
		if part == "" {
			continue
		}
		obj, ok := cur.(map[string]any)
		if !ok {
			return false
		}
		v, exists := obj[part]
		if !exists {
			return false
		}
		cur = v
	}
	return cur != nil
}

// Render validates answers against the param tree, then renders the argv.
// On failure it returns a *Error carrying the jq-compatible code.
func Render(method string, table bindings.Table, tree graph.Node, answers map[string]any) ([]string, error) {
	if err := Validate(tree, answers); err != nil {
		return nil, &Error{Code: CodeInvalidAnswers, Message: err.Error()}
	}
	b, ok := table.Get(method)
	if !ok {
		return nil, &Error{Code: CodeUnboundOperation, Method: method,
			Message: "no CLI binding is registered for this operation"}
	}
	if b.Availability != "cli" {
		return nil, &Error{Code: CodeAPIOnly, Method: method,
			Message: "API-only (no CLI transport registered)"}
	}

	var leaves []leaf
	for _, l := range leafEntries(answers, "") {
		leaves = append(leaves, l)
	}

	// Unbound-parameter check first (jq parity: unique paths).
	seen := map[string]bool{}
	var unbound []string
	for _, l := range leaves {
		if _, bound := b.Params[l.path]; !bound && !seen[l.path] {
			seen[l.path] = true
			unbound = append(unbound, l.path)
		}
	}
	if len(unbound) > 0 {
		sort.Strings(unbound)
		return nil, &Error{Code: CodeUnboundParameter, Method: method, Paths: unbound,
			Message: "answers include parameter paths without CLI bindings"}
	}

	// value_flags coverage check (jq unrenderable_value parity).
	var unknown []pathValue
	for _, l := range leaves {
		spec := b.Params[l.path]
		if spec.ValueFlags != nil {
			key := fmt.Sprint(valueToPrint(l.value))
			if _, ok := spec.ValueFlags[key]; !ok {
				unknown = append(unknown, pathValue{Path: l.path, Value: l.value})
			}
		}
	}
	if len(unknown) > 0 {
		return nil, &Error{Code: CodeUnrenderable, Method: method, Values: unknown,
			Message: "answers include values without value_flags mappings"}
	}

	// Positionals ordered by spec index, then presence flags, then the rest.
	type posEntry struct {
		order int
		spec  bindings.ParamSpec
		value any
	}
	var positionals []posEntry
	var flags []string
	for _, l := range leaves {
		spec := b.Params[l.path]
		if spec.Positional != nil {
			positionals = append(positionals, posEntry{*spec.Positional, spec, l.value})
		} else {
			flags = append(flags, argvForValue(spec, l.value)...)
		}
	}
	sort.SliceStable(positionals, func(i, j int) bool { return positionals[i].order < positionals[j].order })

	var presence []string
	paramKeys := make([]string, 0, len(b.Params))
	for k := range b.Params {
		paramKeys = append(paramKeys, k)
	}
	sort.Strings(paramKeys)
	for _, k := range paramKeys {
		spec := b.Params[k]
		if spec.PresenceFlag != "" && hasPath(answers, k) {
			presence = append(presence, spec.PresenceFlag)
		}
	}

	argv := []string{"herdr"}
	argv = append(argv, b.Argv...)
	for _, p := range positionals {
		argv = append(argv, argvForValue(p.spec, p.value)...)
	}
	argv = append(argv, presence...)
	argv = append(argv, flags...)
	return argv, nil
}

// OrderedAsMap converts an ordered document object to a plain map recursively
// (validation only needs presence/types, not order).
func OrderedAsMap(v any) any {
	if keys := traverse.OrderedKeys(v); keys != nil {
		out := make(map[string]any, len(keys))
		for _, k := range keys {
			val, _ := traverse.OrderedGet(v, k)
			out[k] = OrderedAsMap(val)
		}
		return out
	}
	if arr, ok := v.([]any); ok {
		out := make([]any, len(arr))
		for i, item := range arr {
			out[i] = OrderedAsMap(item)
		}
		return out
	}
	return v
}

// RenderOrdered renders from an insertion-ordered answer document (produced
// by traverse.DecodeOrdered), guaranteeing jq-parity flag order.
func RenderOrdered(method string, table bindings.Table, tree graph.Node, ordered any) ([]string, error) {
	plain := OrderedAsMap(ordered)
	answers, ok := plain.(map[string]any)
	if !ok {
		return nil, &Error{Code: CodeInvalidAnswers, Message: "answers must be a JSON object"}
	}
	if err := Validate(tree, answers); err != nil {
		return nil, &Error{Code: CodeInvalidAnswers, Message: err.Error()}
	}
	b, okB := table.Get(method)
	if !okB {
		return nil, &Error{Code: CodeUnboundOperation, Method: method,
			Message: "no CLI binding is registered for this operation"}
	}
	if b.Availability != "cli" {
		return nil, &Error{Code: CodeAPIOnly, Method: method,
			Message: "API-only (no CLI transport registered)"}
	}
	var leaves []leaf
	for _, l := range leafEntries(ordered, "") {
		leaves = append(leaves, l)
	}
	seen := map[string]bool{}
	var unbound []string
	for _, l := range leaves {
		if _, bound := b.Params[l.path]; !bound && !seen[l.path] {
			seen[l.path] = true
			unbound = append(unbound, l.path)
		}
	}
	if len(unbound) > 0 {
		sort.Strings(unbound)
		return nil, &Error{Code: CodeUnboundParameter, Method: method, Paths: unbound,
			Message: "answers include parameter paths without CLI bindings"}
	}
	var unknown []pathValue
	for _, l := range leaves {
		spec := b.Params[l.path]
		if spec.ValueFlags != nil {
			key := fmt.Sprint(valueToPrint(l.value))
			if _, ok := spec.ValueFlags[key]; !ok {
				unknown = append(unknown, pathValue{Path: l.path, Value: l.value})
			}
		}
	}
	if len(unknown) > 0 {
		return nil, &Error{Code: CodeUnrenderable, Method: method, Values: unknown,
			Message: "answers include values without value_flags mappings"}
	}
	type posEntry struct {
		order int
		spec  bindings.ParamSpec
		value any
	}
	var positionals []posEntry
	var flags []string
	for _, l := range leaves {
		spec := b.Params[l.path]
		if spec.Positional != nil {
			positionals = append(positionals, posEntry{*spec.Positional, spec, l.value})
		} else {
			flags = append(flags, argvForValue(spec, l.value)...)
		}
	}
	sort.SliceStable(positionals, func(i, j int) bool { return positionals[i].order < positionals[j].order })
	var presence []string
	paramKeys := make([]string, 0, len(b.Params))
	for k := range b.Params {
		paramKeys = append(paramKeys, k)
	}
	sort.Strings(paramKeys)
	for _, k := range paramKeys {
		spec := b.Params[k]
		if spec.PresenceFlag != "" && hasPath(answers, k) {
			presence = append(presence, spec.PresenceFlag)
		}
	}
	argv := []string{"herdr"}
	argv = append(argv, b.Argv...)
	for _, p := range positionals {
		argv = append(argv, argvForValue(p.spec, p.value)...)
	}
	argv = append(argv, presence...)
	argv = append(argv, flags...)
	return argv, nil
}
