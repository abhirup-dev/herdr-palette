// Package datasource resolves live Herdr session values (panes, tabs,
// workspaces, agents) for interactive traversal pickers.
//
// The registry maps schema field names to a live snapshot source. It is the Go
// counterpart of the datasource registry in traverse.sh and intentionally
// lives outside internal/graph so a maintainer can add a semantic field
// without touching the static protocol graph. Values come from a fresh
// `herdr api snapshot` at call time and are never cached.
package datasource

import (
	"fmt"
	"herdr-palette/internal/pluginroot"
	"os/exec"
	"strings"

	json "github.com/bytedance/sonic"
)

// Entry is one picker row: the opaque live identifier and the human label.
// Label formatting matches the historical jq picker exactly: "label (id)".
type Entry struct {
	ID    string
	Label string
}

// Source names the snapshot collection backing a registry entry.
type Source string

const (
	Panes      Source = "panes"
	Tabs       Source = "tabs"
	Workspaces Source = "workspaces"
	Agents     Source = "agents"
)

// RegistryFor resolves a schema field name to a live source for a method.
// The target rule mirrors traverse.sh: for agent.* methods "target" means
// agents, otherwise panes. Returns ok=false when the field is not dynamic.
func RegistryFor(method, name string) (Source, bool) {
	switch name {
	case "pane_id", "source_pane_id", "target_pane_id":
		return Panes, true
	case "tab_id":
		return Tabs, true
	case "workspace_id":
		return Workspaces, true
	case "target":
		if strings.HasPrefix(method, "agent.") {
			return Agents, true
		}
		return Panes, true
	}
	return "", false
}

// cleanLabel collapses characters that would corrupt picker rows, matching the
// jq clean_label helper (gsub of tab/CR/LF with a space).
func cleanLabel(s string) string {
	r := strings.NewReplacer("\t", " ", "\r", " ", "\n", " ")
	return r.Replace(s)
}

// Snapshot is the minimal decoded shape of `herdr api snapshot` output.
type Snapshot struct {
	Result struct {
		Snapshot struct {
			Panes []struct {
				PaneID                string `json:"pane_id"`
				Title                 string `json:"title"`
				TerminalTitleStripped string `json:"terminal_title_stripped"`
			} `json:"panes"`
			Tabs []struct {
				TabID string `json:"tab_id"`
				Label string `json:"label"`
			} `json:"tabs"`
			Workspaces []struct {
				WorkspaceID string `json:"workspace_id"`
				Label       string `json:"label"`
			} `json:"workspaces"`
			Agents []struct {
				PaneID                string `json:"pane_id"`
				Title                 string `json:"title"`
				TerminalTitleStripped string `json:"terminal_title_stripped"`
			} `json:"agents"`
		} `json:"snapshot"`
	} `json:"result"`
}

// Fetch runs `herdr api snapshot` and decodes it. The command is executed
// fresh for every call: identifiers are live state, never cached.
func Fetch() (*Snapshot, error) {
	out, err := exec.Command(pluginroot.HerdrBinary(), "api", "snapshot").Output()
	if err != nil {
		return nil, fmt.Errorf("herdr api snapshot: %w", err)
	}
	var snap Snapshot
	if err := json.Unmarshal(out, &snap); err != nil {
		return nil, fmt.Errorf("decode snapshot: %w", err)
	}
	return &snap, nil
}

// Entries converts the snapshot into picker rows for one source. The label
// fallback chain (title → terminal_title_stripped → id) matches the jq
// snapshot-values.jq helper.
func (s *Snapshot) Entries(src Source) []Entry {
	var entries []Entry
	switch src {
	case Panes:
		for _, p := range s.Result.Snapshot.Panes {
			title := p.Title
			if title == "" {
				title = p.TerminalTitleStripped
			}
			if title == "" {
				title = p.PaneID
			}
			entries = append(entries, Entry{ID: p.PaneID, Label: fmt.Sprintf("%s (%s)", cleanLabel(title), p.PaneID)})
		}
	case Tabs:
		for _, t := range s.Result.Snapshot.Tabs {
			label := t.Label
			if label == "" {
				label = t.TabID
			}
			entries = append(entries, Entry{ID: t.TabID, Label: fmt.Sprintf("%s (%s)", cleanLabel(label), t.TabID)})
		}
	case Workspaces:
		for _, w := range s.Result.Snapshot.Workspaces {
			label := w.Label
			if label == "" {
				label = w.WorkspaceID
			}
			entries = append(entries, Entry{ID: w.WorkspaceID, Label: fmt.Sprintf("%s (%s)", cleanLabel(label), w.WorkspaceID)})
		}
	case Agents:
		for _, a := range s.Result.Snapshot.Agents {
			title := a.Title
			if title == "" {
				title = a.TerminalTitleStripped
			}
			if title == "" {
				title = a.PaneID
			}
			entries = append(entries, Entry{ID: a.PaneID, Label: fmt.Sprintf("%s (%s)", cleanLabel(title), a.PaneID)})
		}
	}
	return entries
}

// Picker abstracts the interactive selection layer so tests can drive the
// walker without fzf or a tty.
type Picker interface {
	// Pick shows entries with a prompt and returns the chosen line, or an
	// empty string when the user aborted (ctrl-c / esc / empty input).
	Pick(prompt string, lines []string) (string, error)
	// PickTSV shows tab-separated rows displaying only the label column,
	// returning the full selected row (caller splits the id off column 1).
	PickTSV(prompt string, lines []string) (string, error)
	// ReadScalar prompts on stdin and returns one raw line.
	ReadScalar(prompt string) (string, error)
}
