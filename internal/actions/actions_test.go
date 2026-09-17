package actions

import (
	"strings"
	"testing"
)

// TestTabRows ports act.sh's tab-list formatting pipeline (jq + awk).
func TestTabRows(t *testing.T) {
	in := []byte(`{"result":{"tabs":[
		{"tab_id":"w1:t1","label":"main"},
		{"tab_id":"w1:t2","label":""}
	]}}`)
	rows, err := TabRows(in)
	if err != nil {
		t.Fatalf("TabRows: %v", err)
	}
	want := []string{"w1:t1 (main)", "w1:t2 (w1:t2)"}
	if len(rows) != len(want) {
		t.Fatalf("rows = %v, want %v", rows, want)
	}
	for i := range want {
		if rows[i] != want[i] {
			t.Errorf("rows[%d] = %q, want %q", i, rows[i], want[i])
		}
	}
}

// TestTabRowsRejectsGarbage: non-JSON output must error loudly, not silently
// present an empty picker.
func TestTabRowsRejectsGarbage(t *testing.T) {
	if _, err := TabRows([]byte("not json")); err == nil {
		t.Fatal("expected error for non-JSON tab list")
	}
}

// TestActivePaneEnvOverride: popup env wins over the snapshot lookup.
func TestActivePaneEnvOverride(t *testing.T) {
	t.Setenv("HERDR_ACTIVE_PANE_ID", "w9:p9")
	p, err := ActivePane()
	if err != nil || p != "w9:p9" {
		t.Fatalf("ActivePane() = %q, %v; want w9:p9", p, err)
	}
}

// TestUnknownActionErrors matches act.sh's "unknown action" exit 2.
func TestUnknownActionErrors(t *testing.T) {
	err := Run("no-such-action", nil, Ports{})
	if err == nil || !strings.Contains(err.Error(), "unknown action") {
		t.Fatalf("err = %v; want unknown action", err)
	}
}

// TestArgArity: id-taking actions reject missing arguments.
func TestArgArity(t *testing.T) {
	for _, name := range []string{"close-pane-id", "close-tab-id", "close-workspace-id", "rename-agent", "agent-prompt", "rename-workspace-id", "rename-tab-id", "send-key-current"} {
		if err := Run(name, nil, Ports{}); err == nil || !strings.Contains(err.Error(), "needs") {
			t.Errorf("%s: err = %v; want arity error", name, err)
		}
	}
	if err := Run("send-key", []string{"only-one"}, Ports{}); err == nil || !strings.Contains(err.Error(), "needs") {
		t.Errorf("send-key arity: err = %v", err)
	}
}
