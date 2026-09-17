// Package traverse walks a cached Herdr operation's typed parameter tree.
//
// It is the Go port of traverse.sh: required-first field order, enum and union
// variant dispatch via the interactive picker, the live datasource registry
// for identifier fields, validated scalars honoring defaults, and an
// accumulated JSON answer tree. The walker never executes an operation.
package traverse

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	json "github.com/bytedance/sonic"
	"herdr-palette/internal/datasource"
	"herdr-palette/internal/graph"
)

// Session carries walker state: the method being walked, the picker, the
// answers document, and the snapshot fetcher (nil disables live pickers).
type Session struct {
	Method string
	Picker datasource.Picker
	Fetch  func() (*datasource.Snapshot, error)

	// root is the insertion-ordered answer accumulator (jq parity).
	root *orderedMap
}

// New creates a session for method.
func New(method string, picker datasource.Picker, fetch func() (*datasource.Snapshot, error)) *Session {
	return &Session{Method: method, Picker: picker, Fetch: fetch, root: newOrderedMap()}
}

// ---------- answer-tree mutation (mirrors jq setpath) ----------

// setPath assigns value at a dotted path, creating intermediate maps.
func (s *Session) setPath(path string, value any) {
	parts := strings.Split(path, ".")
	cur := s.root
	for _, p := range parts[:len(parts)-1] {
		next, ok := cur.vals[p].(*orderedMap)
		if !ok {
			next = newOrderedMap()
			cur.set(p, next)
		}
		cur = next
	}
	cur.set(parts[len(parts)-1], value)
}

// orderedMap preserves jq-style insertion order for object keys, which Go
// maps do not. jq objects iterate keys in insertion order; parity of the
// emitted answer tree depends on it.
type orderedMap struct {
	keys []string
	vals map[string]any
}

func newOrderedMap() *orderedMap { return &orderedMap{vals: map[string]any{}} }

func (m *orderedMap) set(key string, value any) {
	if _, exists := m.vals[key]; !exists {
		m.keys = append(m.keys, key)
	}
	m.vals[key] = value
}

// marshalJSON writes the tree with 2-space indent, matching jq's default
// pretty output shape (": " separators, no trailing spaces).
func (m *orderedMap) marshalJSON(indent string) string {
	var b strings.Builder
	b.WriteString("{\n")
	for i, k := range m.keys {
		b.WriteString(indent + "  \"" + k + "\": ")
		b.WriteString(marshalValue(m.vals[k], indent+"  "))
		if i < len(m.keys)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	b.WriteString(indent + "}")
	return b.String()
}

func marshalValue(v any, indent string) string {
	switch t := v.(type) {
	case *orderedMap:
		return t.marshalJSON(indent)
	case []any:
		if len(t) == 0 {
			return "[]"
		}
		var b strings.Builder
		b.WriteString("[\n")
		for i, item := range t {
			b.WriteString(indent + "  ")
			b.WriteString(marshalValue(item, indent+"  "))
			if i < len(t)-1 {
				b.WriteString(",")
			}
			b.WriteString("\n")
		}
		b.WriteString(indent + "]")
		return b.String()
	case string:
		q, _ := json.Marshal(t)
		return string(q)
	case bool:
		return fmt.Sprint(t)
	case float64:
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'g', -1, 64)
	case nil:
		return "null"
	default:
		q, _ := json.Marshal(t)
		return string(q)
	}
}

// OrderedKeys returns the insertion-ordered keys of an ordered document
// object, or nil when v is not an ordered object.
func OrderedKeys(v any) []string {
	if m, ok := v.(*orderedMap); ok {
		return m.keys
	}
	return nil
}

// OrderedGet returns the value at key in an ordered document object.
func OrderedGet(v any, key string) (any, bool) {
	if m, ok := v.(*orderedMap); ok {
		val, exists := m.vals[key]
		return val, exists
	}
	return nil, false
}

// IsOrderedObject reports whether v is an ordered document object.
func IsOrderedObject(v any) bool {
	_, ok := v.(*orderedMap)
	return ok
}

// sortedTree removed: jq preserves insertion order, so answers serialize in
// walk order via orderedMap. See marshalJSON.

// AnswersJSON returns the pretty-printed answer tree in insertion order,
// 2-space indented like jq's default output, with a trailing newline.
func (s *Session) AnswersJSON() (string, error) {
	return s.root.marshalJSON("") + "\n", nil
}

// ---------- validation (port of jq/validate-answers.jq) ----------

// Validate checks answers against node recursively. Error messages mirror the
// jq fail_at format: "path: message" (path joined with dots, "parameters"
// at the root).
func Validate(node graph.Node, value any, path []string) error {
	switch node.Kind {
	case "enum":
		for _, v := range node.Values {
			if fmt.Sprint(v) == fmt.Sprint(value) {
				return nil
			}
		}
		return failAt(path, "expected one of "+strings.Join(valuesStrings(node.Values), ", "))
	case "union":
		obj, ok := value.(map[string]any)
		if !ok {
			return failAt(path, "expected object with type discriminator")
		}
		tag, _ := obj["type"].(string)
		var variant *graph.Variant
		for i := range node.Variants {
			if node.Variants[i].Tag == tag {
				variant = &node.Variants[i]
				break
			}
		}
		if variant == nil {
			t := tag
			if t == "" {
				t = "null"
			}
			return failAt(path, "unknown variant "+t)
		}
		for _, f := range variant.Fields {
			fv, present := obj[f.Name]
			if f.Required && !presentNonNil(obj, f.Name) {
				return failAt(append(path, f.Name), "required")
			}
			if present {
				if err := Validate(f.Node, fv, append(path, f.Name)); err != nil {
					return err
				}
			}
		}
		return nil
	case "object":
		obj, ok := value.(map[string]any)
		if !ok {
			return failAt(path, "expected object")
		}
		for _, f := range node.Fields {
			fv, present := obj[f.Name]
			if f.Required && !presentNonNil(obj, f.Name) {
				return failAt(append(path, f.Name), "required")
			}
			if present {
				if err := Validate(f.Node, fv, append(path, f.Name)); err != nil {
					return err
				}
			}
		}
		return nil
	case "array":
		arr, ok := value.([]any)
		if !ok {
			return failAt(path, "expected array")
		}
		if node.Items == nil {
			return nil
		}
		for i, item := range arr {
			if err := Validate(*node.Items, item, append(path, strconv.Itoa(i))); err != nil {
				return err
			}
		}
		return nil
	case "scalar":
		return validateScalar(node, value, path)
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
		f, ok := toNumber(value)
		if !ok || f != float64(int64(f)) {
			return failAt(path, "expected integer")
		}
	case "number":
		if _, ok := toNumber(value); !ok {
			return failAt(path, "expected number")
		}
	case "string":
		if _, ok := value.(string); !ok {
			return failAt(path, "expected string")
		}
	}
	if node.Minimum != nil {
		if min, ok := toNumber(node.Minimum); ok {
			if v, ok := toNumber(value); ok && v < min {
				return failAt(path, "below minimum "+fmt.Sprint(node.Minimum))
			}
		}
	}
	if node.Maximum != nil {
		if max, ok := toNumber(node.Maximum); ok {
			if v, ok := toNumber(value); ok && v > max {
				return failAt(path, "above maximum "+fmt.Sprint(node.Maximum))
			}
		}
	}
	return nil
}

func toNumber(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case float32:
		return float64(t), true
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	case string:
		f, err := strconv.ParseFloat(t, 64)
		return f, err == nil
	}
	return 0, false
}

func valuesStrings(vals []any) []string {
	out := make([]string, len(vals))
	for i, v := range vals {
		out[i] = fmt.Sprint(v)
	}
	return out
}

func presentNonNil(obj map[string]any, name string) bool {
	v, ok := obj[name]
	return ok && v != nil
}

func failAt(path []string, msg string) error {
	if len(path) == 0 {
		return fmt.Errorf("parameters: %s", msg)
	}
	return fmt.Errorf("%s: %s", strings.Join(path, "."), msg)
}

// ValidateAnswers validates a complete answer document against the method's
// param tree and returns it unchanged on success.
func ValidateAnswers(tree graph.Node, answers map[string]any) (map[string]any, error) {
	if err := Validate(tree, answers, nil); err != nil {
		return nil, err
	}
	return answers, nil
}

// ---------- interactive walker (port of walk_node/walk_field) ----------

// Walk traverses the method's root node interactively.
func (s *Session) Walk(root graph.Node) error {
	return s.walkNode(root, "", s.Method)
}

func (s *Session) walkNode(node graph.Node, path, name string) error {
	switch node.Kind {
	case "enum":
		lines := valuesStrings(node.Values)
		choice, err := s.Picker.Pick(name+"> ", lines)
		if err != nil {
			return err
		}
		if choice == "" {
			return fmt.Errorf("no selection for %s", name)
		}
		s.setPath(path, choice)
		return nil

	case "union":
		tags := make([]string, len(node.Variants))
		for i, v := range node.Variants {
			tags[i] = v.Tag
		}
		choice, err := s.Picker.Pick(name+" type> ", tags)
		if err != nil {
			return err
		}
		if choice == "" {
			return fmt.Errorf("no selection for %s", name)
		}
		s.setPath(path+".type", choice)
		for _, v := range node.Variants {
			if v.Tag != choice {
				continue
			}
			for _, f := range v.Fields {
				if err := s.walkField(f, path); err != nil {
					return err
				}
			}
		}
		return nil

	case "object":
		// Required fields first, then optional — deterministic order and
		// protocol semantics, exactly like traverse.sh's ordered_fields.
		for _, req := range []bool{true, false} {
			for _, f := range node.Fields {
				if f.Required != req {
					continue
				}
				if err := s.walkField(f, path); err != nil {
					return err
				}
			}
		}
		return nil

	case "array":
		arr := []any{}
		for {
			line, err := s.Picker.ReadScalar(fmt.Sprintf("%s item (empty finishes): ", name))
			if err != nil {
				return err
			}
			if strings.TrimSpace(line) == "" {
				break
			}
			arr = append(arr, line)
		}
		s.setPath(path, arr)
		return nil

	case "scalar":
		if src, ok := datasource.RegistryFor(s.Method, name); ok {
			snap, err := s.Fetch()
			if err != nil {
				return err
			}
			entries := snap.Entries(src)
			lines := make([]string, len(entries))
			for i, e := range entries {
				lines[i] = e.ID + "\t" + e.Label
			}
			choice, err := s.Picker.PickTSV(name+"> ", lines)
			if err != nil {
				return err
			}
			if choice == "" {
				return fmt.Errorf("no selection for %s", name)
			}
			s.setPath(path, strings.SplitN(choice, "\t", 2)[0])
			return nil
		}
		value, err := s.readScalar(node, name)
		if err != nil {
			return err
		}
		s.setPath(path, value)
		return nil
	}
	return fmt.Errorf("unsupported schema node for %s: %s", path, node.Kind)
}

func (s *Session) walkField(field graph.Field, parent string) error {
	path := field.Name
	if parent != "" {
		path = parent + "." + field.Name
	}
	if !field.Required {
		choice, err := s.Picker.Pick(field.Name+"> ", []string{"skip", "configure"})
		if err != nil {
			return err
		}
		if choice != "configure" {
			return nil
		}
	}
	return s.walkNode(field.Node, path, field.Name)
}

// readScalar prompts and parses a typed scalar, honoring defaults.
// Mirrors traverse.sh read_scalar including its y/n and numeric regexes.
func (s *Session) readScalar(node graph.Node, name string) (any, error) {
	prompt := name
	if d, ok := node.Description.(string); ok && d != "" {
		// bash prepends the field name to the description (sed "s/^/$name /")
		prompt = name + " " + d
	}
	suffix := ": "
	if node.Default != nil {
		suffix = fmt.Sprintf(" [%v]: ", node.Default)
	}
	line, err := s.Picker.ReadScalar(prompt + suffix)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(line) == "" && node.Default != nil {
		return node.Default, nil
	}
	switch node.Type {
	case "boolean":
		switch line {
		case "y", "Y", "yes", "true", "1":
			return true, nil
		case "n", "N", "no", "false", "0":
			return false, nil
		}
		return nil, fmt.Errorf("invalid boolean")
	case "integer":
		// Match bash ^-?[0-9]+$ — no plus sign, no exponents.
		if integerRe.MatchString(line) {
			n, _ := strconv.ParseInt(line, 10, 64)
			return float64(n), nil
		}
		return nil, fmt.Errorf("invalid integer")
	case "number":
		// Match bash ^-?([0-9]+([.][0-9]*)?|[.][0-9]+)$ — plain decimals only.
		if numberRe.MatchString(line) {
			f, _ := strconv.ParseFloat(line, 64)
			return f, nil
		}
		return nil, fmt.Errorf("invalid number")
	default:
		return line, nil
	}
}

// ---------- concrete picker: fzf subprocess + stdin ----------

// Strict scalar formats mirroring the bash traversal's read_scalar regexes.
var (
	integerRe = regexp.MustCompile(`^-?[0-9]+$`)
	numberRe  = regexp.MustCompile(`^-?([0-9]+([.][0-9]*)?|[.][0-9]+)$`)
)

// FzfPicker runs fzf for selections and reads stdin lines for scalars.
// UX is identical to the bash palette: same prompts, same abort semantics.
type FzfPicker struct {
	In  io.Reader
	Out io.Writer
	// lastOut captures fzf stdout (the selected line) per invocation.
	lastOut bytes.Buffer
}

// Pick runs fzf over lines (plain list, first line is the value).
func (p *FzfPicker) Pick(prompt string, lines []string) (string, error) {
	return p.runFzf(prompt, lines, false)
}

// PickTSV runs fzf over TSV rows showing only column 2, like pick_live.
func (p *FzfPicker) PickTSV(prompt string, lines []string) (string, error) {
	return p.runFzf(prompt, lines, true)
}

func (p *FzfPicker) runFzf(prompt string, lines []string, tsv bool) (string, error) {
	if len(lines) == 0 {
		return "", fmt.Errorf("no entries for %s", strings.TrimSuffix(prompt, "> "))
	}
	args := []string{"--prompt", prompt}
	if tsv {
		args = append(args, "--delimiter", "\t", "--with-nth", "2")
	}
	cmd := exec.Command("fzf", args...)
	cmd.Stdin = newStringsReader(lines)
	// fzf draws its TUI on stderr (must reach the terminal) and writes the
	// selected line to stdout (captured). Run, not Output(): Output() errors
	// when Stdout is set, which would silently skip fzf entirely.
	cmd.Stdout = &p.lastOut
	cmd.Stderr = os.Stderr
	p.lastOut.Reset()
	if err := cmd.Run(); err != nil {
		// fzf exits 130 on abort; treat as empty selection like bash `|| true`.
		return "", nil
	}
	return strings.TrimRight(p.lastOut.String(), "\n"), nil
}

func (p *FzfPicker) outFile() *os.File {
	if f, ok := p.Out.(*os.File); ok {
		return f
	}
	return os.Stdout
}

// lastOut captures fzf's stdout (the selected line) per invocation — see FzfPicker.lastOut.

func newStringsReader(lines []string) io.Reader {
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(l)
		b.WriteByte('\n')
	}
	return strings.NewReader(b.String())
}

// ReadScalar prints a prompt and reads one line from stdin.
func (p *FzfPicker) ReadScalar(prompt string) (string, error) {
	fmt.Fprint(p.Out, prompt)
	reader := bufio.NewReader(p.In)
	line, err := reader.ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// sortedKeys is retained for debugging output.
func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// ---------- order-preserving decode of answer files ----------

// MarshalOrdered renders any decoded JSON value (sonic AST or plain) in jq
// pretty style: 2-space indent, "key: value" separators. Object key order is
// preserved when the value came from DecodeOrdered.
func MarshalOrdered(v any) string { return marshalValue(v, "") }

// DecodeOrdered decodes JSON preserving object key insertion order.
// Order matters for byte-equal parity with the bash/jq traversal output.
func DecodeOrdered(data []byte) (any, error) {
	return decodeOrderedBytes(data)
}

func decodeOrderedBytes(data []byte) (any, error) {
	dec := decoderPool.Get().(*orderDecoder)
	defer decoderPool.Put(dec)
	dec.buf = data
	dec.pos = 0
	v, err := dec.value()
	if err != nil {
		return nil, err
	}
	dec.skipWS()
	if dec.pos < len(dec.buf) {
		return nil, fmt.Errorf("trailing data at %d", dec.pos)
	}
	return v, nil
}

type orderDecoder struct {
	buf []byte
	pos int
}

var decoderPool = sync.Pool{New: func() any { return &orderDecoder{} }}

func (d *orderDecoder) skipWS() {
	for d.pos < len(d.buf) {
		switch d.buf[d.pos] {
		case ' ', '\t', '\n', '\r':
			d.pos++
		default:
			return
		}
	}
}

func (d *orderDecoder) value() (any, error) {
	d.skipWS()
	if d.pos >= len(d.buf) {
		return nil, fmt.Errorf("unexpected end of input")
	}
	switch c := d.buf[d.pos]; {
	case c == '{':
		d.pos++
		m := newOrderedMap()
		d.skipWS()
		if d.pos < len(d.buf) && d.buf[d.pos] == '}' {
			d.pos++
			return m, nil
		}
		for {
			d.skipWS()
			k, err := d.stringLit()
			if err != nil {
				return nil, err
			}
			d.skipWS()
			if d.pos >= len(d.buf) || d.buf[d.pos] != ':' {
				return nil, fmt.Errorf("expected ':' at %d", d.pos)
			}
			d.pos++
			v, err := d.value()
			if err != nil {
				return nil, err
			}
			m.set(k, v)
			d.skipWS()
			if d.pos >= len(d.buf) {
				return nil, fmt.Errorf("unterminated object")
			}
			if d.buf[d.pos] == ',' {
				d.pos++
				continue
			}
			if d.buf[d.pos] == '}' {
				d.pos++
				return m, nil
			}
			return nil, fmt.Errorf("expected ',' or '}' at %d", d.pos)
		}
	case c == '[':
		d.pos++
		arr := []any{}
		d.skipWS()
		if d.pos < len(d.buf) && d.buf[d.pos] == ']' {
			d.pos++
			return arr, nil
		}
		for {
			v, err := d.value()
			if err != nil {
				return nil, err
			}
			arr = append(arr, v)
			d.skipWS()
			if d.pos >= len(d.buf) {
				return nil, fmt.Errorf("unterminated array")
			}
			if d.buf[d.pos] == ',' {
				d.pos++
				continue
			}
			if d.buf[d.pos] == ']' {
				d.pos++
				return arr, nil
			}
			return nil, fmt.Errorf("expected ',' or ']' at %d", d.pos)
		}
	case c == '"':
		return d.stringLit()
	default:
		// number | true | false | null — delegate to sonic for exact parsing
		start := d.pos
		for d.pos < len(d.buf) && !isWS(d.buf[d.pos]) && d.buf[d.pos] != ',' && d.buf[d.pos] != '}' && d.buf[d.pos] != ']' {
			d.pos++
		}
		tok := d.buf[start:d.pos]
		switch string(tok) {
		case "true":
			return true, nil
		case "false":
			return false, nil
		case "null":
			return nil, nil
		}
		f, err := strconv.ParseFloat(string(tok), 64)
		if err != nil {
			return nil, fmt.Errorf("invalid token %q", tok)
		}
		return f, nil
	}
}

func isWS(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }

func (d *orderDecoder) stringLit() (string, error) {
	d.skipWS()
	if d.pos >= len(d.buf) || d.buf[d.pos] != '"' {
		return "", fmt.Errorf("expected string at %d", d.pos)
	}
	start := d.pos
	d.pos++
	for d.pos < len(d.buf) {
		if d.buf[d.pos] == '\\' {
			d.pos += 2
			continue
		}
		if d.buf[d.pos] == '"' {
			d.pos++
			// sonic handles escape decoding exactly
			var s string
			if err := json.Unmarshal(d.buf[start:d.pos], &s); err != nil {
				return "", err
			}
			return s, nil
		}
		d.pos++
	}
	return "", fmt.Errorf("unterminated string")
}

// OrderedToPlain converts an order-preserved decode into plain map/slice/any
// values for validation, which is order-agnostic.
func OrderedToPlain(v any) map[string]any {
	m, ok := v.(*orderedMap)
	if !ok {
		return nil
	}
	return m.toPlain().(map[string]any)
}

func (m *orderedMap) toPlain() any {
	out := map[string]any{}
	for _, k := range m.keys {
		out[k] = plainValue(m.vals[k])
	}
	return out
}

func plainValue(v any) any {
	switch t := v.(type) {
	case *orderedMap:
		return t.toPlain()
	case []any:
		arr := make([]any, len(t))
		for i, item := range t {
			arr[i] = plainValue(item)
		}
		return arr
	default:
		return v
	}
}
