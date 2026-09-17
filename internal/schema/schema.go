// Package schema parses Herdr's JSON Schema document without imposing a
// protocol-version-specific Go struct on its extensible definitions.
package schema

import (
	"fmt"
	"os"

	json "github.com/bytedance/sonic"
)

// Document is the raw Herdr API schema. JSON Schema definitions intentionally
// remain dynamic maps: new protocols may add fields the indexer does not need.
type Document struct {
	Protocol      int            `json:"protocol"`
	SchemaVersion int            `json:"schema_version"`
	Schemas       map[string]any `json:"schemas"`
}

// Parse decodes one raw Herdr API schema document.
func Parse(data []byte) (*Document, error) {
	var doc Document
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("decode Herdr API schema: %w", err)
	}
	if doc.Protocol < 0 || doc.SchemaVersion < 0 || doc.Schemas == nil {
		return nil, fmt.Errorf("invalid Herdr API schema metadata")
	}
	if _, ok := doc.Schemas["request"].(map[string]any); !ok {
		return nil, fmt.Errorf("schema has no request schema")
	}
	return &doc, nil
}

// ParseFile decodes a schema stored on disk. It is used for deterministic
// indexing tests and release maintenance; production normally calls Herdr.
func ParseFile(path string) (*Document, []byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("read schema %q: %w", path, err)
	}
	doc, err := Parse(data)
	return doc, data, err
}

// Request returns the schema's request branch as a dynamic JSON object.
func (d *Document) Request() map[string]any { return d.Schemas["request"].(map[string]any) }
