package manifest

import (
	"strings"
	"testing"
)

// fixture: two workspaces, two tabs, three panes.
// w1:p1 is an agent, w1:p2 is a plain shell, w2:p1 is the focused pane.
func switchFixture() *Snapshot {
	return &Snapshot{
		FocusedPaneID:      "w2:p1",
		FocusedTabID:       "w2:t1",
		FocusedWorkspaceID: "w2",
		Workspaces: []Workspace{
			{WorkspaceID: "w1", Label: "herdr"},
			{WorkspaceID: "w2", Label: "here", Focused: true},
		},
		Tabs: []Tab{
			{TabID: "w1:t1", WorkspaceID: "w1", Label: "pi"},
			{TabID: "w2:t1", WorkspaceID: "w2", Label: "main"},
		},
		Panes: []Pane{
			{PaneID: "w1:p1", WorkspaceID: "w1", TabID: "w1:t1", StripTitle: "claude run"},
			{PaneID: "w1:p2", WorkspaceID: "w1", TabID: "w1:t1", Cwd: "~/Codes/ci"},
			{PaneID: "w2:p1", WorkspaceID: "w2", TabID: "w2:t1", StripTitle: "focused"},
		},
		Agents: []Agent{
			{PaneID: "w1:p1", Agent: "claude"},
		},
	}
}

// A plain shell pane cannot be focused by `herdr agent focus` — that resolves an
// agent target and answers agent_not_found. pane.focus is api-only (no CLI), so
// shell rows must fall back to focusing the containing tab.
func TestSwitchRowsShellPaneFocusesTab(t *testing.T) {
	out := switchRows(switchFixture(), "agents")

	var shell, agent string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		label, command, ok := strings.Cut(line, "\t")
		if !ok {
			t.Fatalf("row is not LABEL<TAB>COMMAND: %q", line)
		}
		switch {
		case strings.HasPrefix(label, "pane "):
			shell = command
		case strings.HasPrefix(label, "agt "):
			agent = command
		}
	}

	if shell != "herdr tab focus w1:t1" {
		t.Errorf("shell pane: got %q, want %q", shell, "herdr tab focus w1:t1")
	}
	if strings.Contains(shell, "agent focus") {
		t.Errorf("shell pane routed to agent focus, which returns agent_not_found: %q", shell)
	}
	if agent != "herdr agent focus w1:p1" {
		t.Errorf("agent pane: got %q, want %q", agent, "herdr agent focus w1:p1")
	}
}

// The focused workspace/tab/pane are never emitted — you cannot switch to where
// you already are.
func TestSwitchRowsSkipsFocused(t *testing.T) {
	out := switchRows(switchFixture(), "all")
	for _, dead := range []string{"workspace focus w2", "tab focus w2:t1", "w2:p1"} {
		if strings.Contains(out, dead) {
			t.Errorf("focused target %q should be skipped, got:\n%s", dead, out)
		}
	}
	for _, want := range []string{"herdr workspace focus w1", "herdr tab focus w1:t1"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing unfocused target %q, got:\n%s", want, out)
		}
	}
}

// Filters select their own section only; an empty filter means "all".
func TestSwitchRowsFilters(t *testing.T) {
	s := switchFixture()
	if got := switchRows(s, "ws"); !strings.Contains(got, "workspace focus w1") ||
		strings.Contains(got, "agent focus") {
		t.Errorf("ws filter leaked non-workspace rows:\n%s", got)
	}
	if got := switchRows(s, "tabs"); !strings.Contains(got, "tab focus w1:t1") ||
		strings.Contains(got, "workspace focus") {
		t.Errorf("tabs filter leaked non-tab rows:\n%s", got)
	}
	if switchRows(s, "") != switchRows(s, "all") {
		t.Error(`empty filter should behave as "all"`)
	}
}
