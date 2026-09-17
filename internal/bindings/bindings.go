// Package bindings loads and validates the palette's CLI bindings file.
//
// The SAME <plugin-root>/bindings.json the bash renderer uses is
// loaded here — the file is shared, never duplicated, so there is exactly one
// hand-verified mapping table per machine.
package bindings

import (
	"fmt"
	"os"

	json "github.com/bytedance/sonic"
)

// ParamSpec describes how one answer path renders onto the CLI.
// Exactly one of the following must be non-zero: Positional, Flag,
// Boolean, ValueFlags, PresenceFlag (Flag may combine with Repeat).
type ParamSpec struct {
	Positional   *int                `json:"positional"`
	Flag         string              `json:"flag"`
	Boolean      map[string]string   `json:"boolean"`     // "true"/"false" -> flag ("" = no flag)
	ValueFlags   map[string][]string `json:"value_flags"` // enum value -> argv fragment
	PresenceFlag string              `json:"presence_flag"`
	Repeat       bool                `json:"repeat"`
	Values       map[string]string   `json:"values"` // optional value mapping (jq parity)
}

// Empty reports whether the spec carries no rendering information.
func (p ParamSpec) Empty() bool {
	return p.Positional == nil && p.Flag == "" && p.Boolean == nil &&
		p.ValueFlags == nil && p.PresenceFlag == ""
}

// Binding is one method's CLI transport mapping.
type Binding struct {
	Availability string               `json:"availability"` // "cli" or "api-only"
	Argv         []string             `json:"argv"`
	Params       map[string]ParamSpec `json:"params"`
}

// Table maps method name -> binding.
type Table map[string]Binding

// Load reads and validates a bindings.json file. Validation fails loudly on
// structural mistakes (empty argv, unknown availability, empty param specs,
// boolean maps missing true/false) so a hand-edit cannot silently break
// rendering at runtime.
func Load(path string) (Table, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("bindings: %w", err)
	}
	var t Table
	if err := json.Unmarshal(data, &t); err != nil {
		return nil, fmt.Errorf("bindings: decode %s: %w", path, err)
	}
	for method, b := range t {
		if b.Availability == "" {
			return nil, fmt.Errorf("bindings: %s: missing availability", method)
		}
		if b.Availability != "cli" && b.Availability != "api-only" {
			return nil, fmt.Errorf("bindings: %s: unknown availability %q", method, b.Availability)
		}
		if b.Availability == "cli" && len(b.Argv) == 0 {
			return nil, fmt.Errorf("bindings: %s: cli binding with empty argv", method)
		}
		for path, spec := range b.Params {
			if spec.Empty() {
				return nil, fmt.Errorf("bindings: %s: param %q has no positional/flag/boolean/value_flags/presence_flag", method, path)
			}
			for k := range spec.Boolean {
				if k != "true" && k != "false" {
					return nil, fmt.Errorf("bindings: %s: param %q boolean key %q must be true/false", method, path, k)
				}
			}
		}
	}
	return t, nil
}

// Get returns the binding for a method.
func (t Table) Get(method string) (Binding, bool) {
	b, ok := t[method]
	return b, ok
}
