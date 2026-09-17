package execute

import (
	"os"
	"testing"

	json "github.com/bytedance/sonic"
	"herdr-palette/internal/bindings"
	"herdr-palette/internal/dispatch"
)

// The above needs a real bindings.Table; use the package type.
func TestOperationLinesWithTable(t *testing.T) {
	dir := t.TempDir()
	cache := dir + "/protocol-20.json"
	doc := map[string]any{
		"operations": []map[string]any{
			{"method": "pane.move", "description": "move a pane"},
			{"method": "made.up", "description": "unbound op"},
		},
	}
	data, _ := json.Marshal(doc)
	os.WriteFile(cache, data, 0o644)
	table := bindings.Table{
		"pane.move": bindings.Binding{Availability: "cli", Argv: []string{"herdr", "pane", "move"}},
	}
	lines, err := operationLines(cache, table)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"pane.move\t[cli] pane.move · move a pane",
		"made.up\t[api-only] made.up · unbound op",
	}
	if len(lines) != len(want) {
		t.Fatalf("lines = %v", lines)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Errorf("lines[%d]=%q want %q", i, lines[i], want[i])
		}
	}
}

// TestCurrentCacheFileMissing: a missing pointer must fail loudly (the
// dispatcher never silently shows an empty picker).
func TestCurrentCacheFileMissing(t *testing.T) {
	if _, err := currentCacheFile(t.TempDir()); err == nil {
		t.Fatal("expected error for missing current.json")
	}
}

// TestArgvPassthroughNoShell: the dispatcher must exec argv directly —
// metacharacters in argv elements must never reach a shell. Prove the
// contract by running a harmless argv with shell-special text through
// dispatch.Run's exec path (true is silent: rc 0, no log pollution via
// HERDR_PALETTE_CACHE_DIR tempdir).
func TestArgvPassthroughNoShell(t *testing.T) {
	t.Setenv("HERDR_PALETTE_CACHE_DIR", t.TempDir())
	// true ignores its arguments entirely.
	if err := dispatch.Run([]string{"/usr/bin/true", "a;rm -rf /", "$(x)", "it's", "a|b"}, "test"); err != nil {
		t.Fatalf("metachar argv failed: %v", err)
	}
}
