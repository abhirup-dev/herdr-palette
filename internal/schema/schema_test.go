package schema

import (
	"os"
	"testing"
)

const fixture = "/tmp/hdr-schema.json"

func TestParseProtocol20Fixture(t *testing.T) {
	data, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatalf("read fixture %s: %v", fixture, err)
	}
	doc, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Protocol != 20 || doc.SchemaVersion != 1 {
		t.Fatalf("metadata = protocol %d/schema %d, want 20/1", doc.Protocol, doc.SchemaVersion)
	}
	if _, ok := doc.Request()["oneOf"].([]any); !ok {
		t.Fatal("request oneOf is unavailable")
	}
}
