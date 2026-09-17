package traverse

import (
	"testing"

	json "github.com/bytedance/sonic"
	"herdr-palette/internal/datasource"
	"herdr-palette/internal/graph"
)

// scriptedPicker replays deterministic selections for interactive-walk tests.
type scriptedPicker struct {
	picks   []string // consumed by Pick/PickTSV in order
	reads   []string // consumed by ReadScalar in order
	pickIdx int
	readIdx int
}

func (p *scriptedPicker) Pick(prompt string, lines []string) (string, error) {
	if p.pickIdx >= len(p.picks) {
		p.pickIdx++
		return "", nil
	}
	s := p.picks[p.pickIdx]
	p.pickIdx++
	return s, nil
}
func (p *scriptedPicker) PickTSV(prompt string, lines []string) (string, error) {
	return p.Pick(prompt, lines)
}
func (p *scriptedPicker) ReadScalar(prompt string) (string, error) {
	if p.readIdx >= len(p.reads) {
		p.readIdx++
		return "", nil
	}
	s := p.reads[p.readIdx]
	p.readIdx++
	return s, nil
}

// paneMoveTree mirrors the cached pane.move param tree shape.
func paneMoveTree() graph.Node {
	return graph.Node{
		Kind: "object",
		Fields: []graph.Field{
			{Name: "pane_id", Required: true, Node: graph.Node{Kind: "scalar", Type: "string"}},
			{Name: "destination", Required: true, Node: graph.Node{
				Kind: "union",
				Variants: []graph.Variant{
					{Tag: "tab", Fields: []graph.Field{
						{Name: "tab_id", Required: true, Node: graph.Node{Kind: "scalar", Type: "string"}},
						{Name: "split", Required: true, Node: graph.Node{Kind: "enum", Values: []any{"right", "down"}}},
					}},
					{Tag: "new_workspace", Fields: []graph.Field{
						{Name: "label", Required: false, Node: graph.Node{Kind: "scalar", Type: "string"}},
					}},
				},
			}},
			{Name: "focus", Required: false, Node: graph.Node{Kind: "scalar", Type: "boolean"}},
		},
	}
}

func TestWalkPaneMoveTabParity(t *testing.T) {
	// Walk order: pane_id (pick live) → destination type (tab) → tab_id (live)
	// → split (right) → focus: skip.
	p := &scriptedPicker{
		picks: []string{"w1:p1\tEditor (w1:p1)", "tab", "w1:t2\tTwo (w1:t2)", "right", "skip"},
	}
	sess := New("pane.move", p, func() (*datasource.Snapshot, error) {
		var snap datasource.Snapshot
		doc := `{"result":{"snapshot":{"panes":[{"pane_id":"w1:p1","title":"Editor"}],"tabs":[{"tab_id":"w1:t2","label":"Two"}]}}}`
		if err := json.Unmarshal([]byte(doc), &snap); err != nil {
			t.Fatal(err)
		}
		return &snap, nil
	})
	if err := sess.Walk(paneMoveTree()); err != nil {
		t.Fatal(err)
	}
	got, err := sess.AnswersJSON()
	if err != nil {
		t.Fatal(err)
	}
	want := `{
  "pane_id": "w1:p1",
  "destination": {
    "type": "tab",
    "tab_id": "w1:t2",
    "split": "right"
  }
}
`
	if got != want {
		t.Fatalf("answer tree mismatch:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestWalkUnionVariantBranching(t *testing.T) {
	// Choosing new_workspace must walk only that variant's fields.
	p := &scriptedPicker{
		// order: pane_id live pick, variant tag, label optional→configure (pick),
		// label value (scalar read), focus→skip (pick)
		picks: []string{"w1:p1\tEditor (w1:p1)", "new_workspace", "configure", "skip"},
		reads: []string{"work"},
	}
	sess := New("pane.move", p, func() (*datasource.Snapshot, error) {
		var snap datasource.Snapshot
		_ = json.Unmarshal([]byte(`{"result":{"snapshot":{"panes":[{"pane_id":"w1:p1","title":"Editor"}]}}}`), &snap)
		return &snap, nil
	})
	if err := sess.Walk(paneMoveTree()); err != nil {
		t.Fatal(err)
	}
	got, _ := sess.AnswersJSON()
	want := `{
  "pane_id": "w1:p1",
  "destination": {
    "type": "new_workspace",
    "label": "work"
  }
}
`
	if got != want {
		t.Fatalf("variant walk mismatch:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestValidateAnswers(t *testing.T) {
	tree := paneMoveTree()
	valid := map[string]any{
		"pane_id":     "w1:p1",
		"destination": map[string]any{"type": "tab", "tab_id": "w1:t2", "split": "right"},
		"focus":       true,
	}
	if _, err := ValidateAnswers(tree, valid); err != nil {
		t.Fatalf("valid tree rejected: %v", err)
	}
	badSplit := map[string]any{
		"pane_id":     "w1:p1",
		"destination": map[string]any{"type": "tab", "tab_id": "w1:t2", "split": "sideways"},
	}
	if _, err := ValidateAnswers(tree, badSplit); err == nil {
		t.Fatal("invalid enum value accepted")
	}
	missingReq := map[string]any{"pane_id": "w1:p1"}
	if _, err := ValidateAnswers(tree, missingReq); err == nil {
		t.Fatal("missing required destination accepted")
	}
	wrongType := map[string]any{
		"pane_id":     "w1:p1",
		"destination": "tab",
	}
	if _, err := ValidateAnswers(tree, wrongType); err == nil {
		t.Fatal("non-object union accepted")
	}
}

func TestValidateScalarBounds(t *testing.T) {
	node := graph.Node{Kind: "scalar", Type: "integer", Minimum: float64(3), Maximum: float64(10)}
	if err := Validate(node, float64(5), nil); err != nil {
		t.Fatalf("in-range rejected: %v", err)
	}
	if err := Validate(node, float64(2), nil); err == nil {
		t.Fatal("below minimum accepted")
	}
	if err := Validate(node, float64(11), nil); err == nil {
		t.Fatal("above maximum accepted")
	}
	if err := Validate(node, float64(5.5), nil); err == nil {
		t.Fatal("non-integer accepted as integer")
	}
}

func TestDispatchTable(t *testing.T) {
	cases := []struct {
		method, field string
		want          datasource.Source
		ok            bool
	}{
		{"pane.move", "pane_id", datasource.Panes, true},
		{"pane.move", "source_pane_id", datasource.Panes, true},
		{"pane.move", "tab_id", datasource.Tabs, true},
		{"workspace.focus", "workspace_id", datasource.Workspaces, true},
		{"agent.read", "target", datasource.Agents, true},
		{"pane.close", "target", datasource.Panes, true},
		{"pane.split", "cwd", "", false},
	}
	for _, c := range cases {
		got, ok := datasource.RegistryFor(c.method, c.field)
		if ok != c.ok || got != c.want {
			t.Errorf("RegistryFor(%s,%s) = %q,%v want %q,%v", c.method, c.field, got, ok, c.want, c.ok)
		}
	}
}
