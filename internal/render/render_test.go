package render

import (
	"os"
	"path/filepath"
	"testing"

	json "github.com/bytedance/sonic"
	"herdr-palette/internal/bindings"
	"herdr-palette/internal/graph"
	"herdr-palette/internal/traverse"
)

// repoRoot locates the plugin repo root (bindings.json, testdata answers).
// go test runs with the package directory as cwd: internal/render -> root.
func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "bindings.json")); err != nil {
		t.Skipf("bindings.json not found under %s: %v", root, err)
	}
	return root
}

func loadTable(t *testing.T) bindings.Table {
	t.Helper()
	table, err := bindings.Load(filepath.Join(repoRoot(t), "bindings.json"))
	if err != nil {
		t.Fatalf("load bindings: %v", err)
	}
	return table
}

// treeFor loads the cache via the same graph module the binary uses.
func treeFor(t *testing.T, method string) graph.Node {
	t.Helper()
	home, _ := os.UserHomeDir()
	data, err := os.ReadFile(filepath.Join(home, ".cache", "herdr-palette", "current.json"))
	if err != nil {
		t.Skipf("no palette cache: %v", err)
	}
	var pointer struct {
		Cache string `json:"cache"`
	}
	if err := json.Unmarshal(data, &pointer); err != nil || pointer.Cache == "" {
		t.Skip("no current cache pointer")
	}
	idxData, err := os.ReadFile(pointer.Cache)
	if err != nil {
		t.Skipf("cache file missing: %v", err)
	}
	var idx graph.Index
	if err := json.Unmarshal(idxData, &idx); err != nil {
		t.Fatalf("decode cache: %v", err)
	}
	for _, op := range idx.Operations {
		if op.Method == method {
			return op.ParamTree
		}
	}
	t.Fatalf("method not in cache: %s", method)
	return graph.Node{}
}

// TestRenderFixtureParity reproduces the three phase23/45 fixtures and checks
// the rendered argv against the bash renderer's expected output.
func TestRenderFixtureParity(t *testing.T) {
	table := loadTable(t)
	cases := []struct {
		method  string
		fixture string
		want    []string
	}{
		{
			method:  "pane.move",
			fixture: "pane-move-tab.json",
			want:    []string{"herdr", "pane", "move", "PLACEHOLDER", "--tab", "w1:t2", "--split", "right", "--focus"},
		},
		{
			method:  "pane.move",
			fixture: "pane-move-new-workspace.json",
			want:    []string{"herdr", "pane", "move", "PLACEHOLDER", "--new-workspace", "--label", "work", "--tab-label", "main"},
		},
		{
			// the only fixture exercising presence_flag (--wait) and repeat (--until)
			method:  "agent.prompt",
			fixture: "agent-prompt.json",
			want:    []string{"herdr", "agent", "prompt", "w1:p1", "review this", "--wait", "--timeout", "5000", "--until", "idle", "--until", "done"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.fixture, func(t *testing.T) {
			tree := treeFor(t, tc.method)
			data, err := os.ReadFile(filepath.Join(repoRoot(t), "internal", "manifest", "testdata", "answers", tc.fixture))
			if err != nil {
				t.Fatalf("fixture: %v", err)
			}
			ordered, err := traverse.DecodeOrdered(data)
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			argv, err := RenderOrdered(tc.method, table, tree, ordered)
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			if len(argv) != len(tc.want) {
				t.Fatalf("argv length %d, want %d: %q", len(argv), len(tc.want), argv)
			}
			for i := range argv {
				if tc.want[i] == "PLACEHOLDER" {
					continue // pane_id comes from the fixture; order is what matters
				}
				if argv[i] != tc.want[i] {
					t.Fatalf("argv[%d] = %q, want %q (full: %q)", i, argv[i], tc.want[i], argv)
				}
			}
		})
	}
}

// TestRenderEmptyObjectBypass guards the phase45 regression: an empty object
// value must never bypass validation and render a partial argv.
func TestRenderEmptyObjectBypass(t *testing.T) {
	table := loadTable(t)
	tree := treeFor(t, "pane.split")
	answers := map[string]any{
		"direction":    "right",
		"workspace_id": map[string]any{},
	}
	_, err := Render("pane.split", table, tree, answers)
	re, ok := err.(*Error)
	if !ok {
		t.Fatalf("expected *Error, got %v", err)
	}
	if re.Code != CodeInvalidAnswers && re.Code != CodeUnboundParameter {
		t.Fatalf("code = %s, want invalid_answers or unbound_parameter", re.Code)
	}
}

// TestRenderScalarUnbound guards the other phase45 regression.
func TestRenderScalarUnbound(t *testing.T) {
	table := loadTable(t)
	tree := treeFor(t, "pane.split")
	answers := map[string]any{
		"direction": "right",
		"bogus_key": "x",
	}
	_, err := Render("pane.split", table, tree, answers)
	re, ok := err.(*Error)
	if !ok {
		t.Fatalf("expected *Error, got %v", err)
	}
	if re.Code != CodeUnboundParameter {
		t.Fatalf("code = %s, want unbound_parameter", re.Code)
	}
	if len(re.Paths) != 1 || re.Paths[0] != "bogus_key" {
		t.Fatalf("paths = %v, want [bogus_key]", re.Paths)
	}
}

// TestRenderAPIOnly checks the api_only refusal.
func TestRenderAPIOnly(t *testing.T) {
	table := loadTable(t)
	tree := treeFor(t, "pane.focus")
	_, err := Render("pane.focus", table, tree, map[string]any{"pane_id": "w1:p1"})
	re, ok := err.(*Error)
	if !ok || re.Code != CodeAPIOnly {
		t.Fatalf("expected api_only, got %v", err)
	}
}
