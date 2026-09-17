package datasource

import (
	"testing"

	json "github.com/bytedance/sonic"
)

const cannedSnapshot = `{"result":{"snapshot":{
  "panes":[{"pane_id":"w1:p1","title":"Editor"},{"pane_id":"w1:p2","terminal_title_stripped":"Tests"},{"pane_id":"w1:p3"}],
  "tabs":[{"tab_id":"w1:t1","label":"Main"},{"tab_id":"w1:t2"}],
  "workspaces":[{"workspace_id":"w1","label":"Build"},{"workspace_id":"w2"}],
  "agents":[{"pane_id":"w2:p1","title":"Reviewer\tline"},{"pane_id":"w3:p1","terminal_title_stripped":"Helper"}]
}}}`

func decodeCanned(t *testing.T) *Snapshot {
	t.Helper()
	var snap Snapshot
	if err := json.Unmarshal([]byte(cannedSnapshot), &snap); err != nil {
		t.Fatal(err)
	}
	return &snap
}

// Label formatting must match the jq snapshot-values.jq exactly:
// "label (id)" with the title → terminal_title_stripped → id fallback chain
// and tab/CR/LF collapsed to spaces.
func TestEntriesLabelFormatting(t *testing.T) {
	snap := decodeCanned(t)

	wantPanes := []Entry{
		{"w1:p1", "Editor (w1:p1)"},
		{"w1:p2", "Tests (w1:p2)"},
		{"w1:p3", "w1:p3 (w1:p3)"},
	}
	assertEntries(t, snap.Entries(Panes), wantPanes)

	wantTabs := []Entry{
		{"w1:t1", "Main (w1:t1)"},
		{"w1:t2", "w1:t2 (w1:t2)"},
	}
	assertEntries(t, snap.Entries(Tabs), wantTabs)

	wantWorkspaces := []Entry{
		{"w1", "Build (w1)"},
		{"w2", "w2 (w2)"},
	}
	assertEntries(t, snap.Entries(Workspaces), wantWorkspaces)

	// Agent with a tab inside its title must have it collapsed, not split rows.
	wantAgents := []Entry{
		{"w2:p1", "Reviewer line (w2:p1)"},
		{"w3:p1", "Helper (w3:p1)"},
	}
	assertEntries(t, snap.Entries(Agents), wantAgents)
}

func assertEntries(t *testing.T, got, want []Entry) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d entries, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("entry %d: got %+v want %+v", i, got[i], want[i])
		}
	}
}
