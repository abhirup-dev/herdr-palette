package graph

import (
	"os"
	"testing"

	"herdr-palette/internal/schema"
)

const fixture = "/tmp/hdr-schema.json"

func fixtureIndex(t *testing.T) Index {
	t.Helper()
	data, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatalf("read fixture %s: %v", fixture, err)
	}
	doc, err := schema.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	index, err := Build(doc, "cksum:test:1", "2026-01-01T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	return index
}
func operation(t *testing.T, index Index, method string) Operation {
	t.Helper()
	for _, op := range index.Operations {
		if op.Method == method {
			return op
		}
	}
	t.Fatalf("operation %q not found", method)
	return Operation{}
}
func field(t *testing.T, node Node, name string) Node {
	t.Helper()
	for _, f := range node.Fields {
		if f.Name == name {
			return f.Node
		}
	}
	t.Fatalf("field %q not found", name)
	return Node{}
}

func TestBuildIncludesOnlyInvocableOperations(t *testing.T) {
	index := fixtureIndex(t)
	if got, want := len(index.Operations), 91; got != want {
		t.Fatalf("operation count = %d, want %d", got, want)
	}
	for _, excluded := range []string{"pane.closed", "tab.renamed", "workspace.updated", "layout.updated"} {
		for _, op := range index.Operations {
			if op.Method == excluded {
				t.Fatalf("event %q was indexed as an operation", excluded)
			}
		}
	}
}

func TestPaneMoveUnionGraph(t *testing.T) {
	op := operation(t, fixtureIndex(t), "pane.move")
	if got := op.ParamShapes.Required; len(got) != 2 || got[0] != "destination" || got[1] != "pane_id" {
		t.Fatalf("required = %#v", got)
	}
	if len(op.ParamShapes.Unions) != 1 {
		t.Fatalf("unions = %#v", op.ParamShapes.Unions)
	}
	variants := op.ParamShapes.Unions[0].Variants
	want := []string{"tab", "new_tab", "new_workspace"}
	if len(variants) != len(want) {
		t.Fatalf("variants = %#v", variants)
	}
	for i := range want {
		if variants[i] != want[i] {
			t.Fatalf("variants = %#v, want %#v", variants, want)
		}
	}
	destination := field(t, op.ParamTree, "destination")
	if destination.Kind != "union" || len(destination.Variants) != 3 {
		t.Fatalf("destination = %#v", destination)
	}
	for i, tag := range want {
		if destination.Variants[i].Tag != tag {
			t.Fatalf("variant %d = %q, want %q", i, destination.Variants[i].Tag, tag)
		}
	}
}

func TestAgentPromptWaitUntilEnumArray(t *testing.T) {
	op := operation(t, fixtureIndex(t), "agent.prompt")
	wait := field(t, op.ParamTree, "wait")
	if wait.Kind != "object" || !wait.Nullable {
		t.Fatalf("wait = %#v", wait)
	}
	until := field(t, wait, "until")
	if until.Kind != "array" || until.Items == nil || until.Items.Kind != "enum" {
		t.Fatalf("wait.until = %#v", until)
	}
	want := []string{"idle", "working", "blocked", "done", "unknown"}
	if len(until.Items.Values) != len(want) {
		t.Fatalf("until enum = %#v", until.Items.Values)
	}
	for i, value := range want {
		if until.Items.Values[i] != value {
			t.Fatalf("until enum = %#v", until.Items.Values)
		}
	}
}
