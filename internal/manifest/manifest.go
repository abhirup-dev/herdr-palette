// Package manifest emits the full palette manifest (LABEL<TAB>COMMAND per
// line) — ranked (*) head when a selections log exists, then the complete
// catalog (curated static ops + live snapshot sections + API browser
// entries). Interactive and api rows self-invoke the Go binary.
package manifest

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	json "github.com/bytedance/sonic"
	"herdr-palette/internal/pluginroot"
	"herdr-palette/internal/ranking"
)

// selfPath resolves this binary once for manifest self-invocations; falls
// back to "herdr-palette" (PATH lookup) when resolution fails so a broken
// os.Executable never blanks the manifest.
var selfPath = func() string {
	if exe, err := pluginroot.Self(); err == nil {
		return exe
	}
	return "herdr-palette"
}()

// actPath returns the prefix for composite actions: "<binary> act".
func actPath() string { return selfPath + " act" }

// clean mirrors source.sh clean(): tr -d "'\"\t" — a BYTE-level delete
// (as tr does), not a rune-level one: multi-byte UTF-8 sequences containing
// those byte values would be mangled. Deleting only the single-byte ASCII
// characters themselves keeps behavior identical to tr -d.
func clean(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\'' || c == '"' || c == '\t' {
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

// snapshot is the subset of herdr api snapshot the manifest consumes.
type snapshot struct {
	Result struct {
		Snapshot Snapshot `json:"snapshot"`
	} `json:"result"`
}

// Snapshot is the decoded live-state section.
type Snapshot struct {
	FocusedPaneID      string      `json:"focused_pane_id"`
	FocusedTabID       string      `json:"focused_tab_id"`
	FocusedWorkspaceID string      `json:"focused_workspace_id"`
	Agents             []Agent     `json:"agents"`
	Workspaces         []Workspace `json:"workspaces"`
	Tabs               []Tab       `json:"tabs"`
	Panes              []Pane      `json:"panes"`
}

type Agent struct {
	Title       string `json:"title"`
	StripTitle  string `json:"terminal_title_stripped"`
	PaneID      string `json:"pane_id"`
	Agent       string `json:"agent"`
	AgentStatus string `json:"agent_status"`
	WorkspaceID string `json:"workspace_id"`
}

type Workspace struct {
	Label       string `json:"label"`
	WorkspaceID string `json:"workspace_id"`
	Focused     bool   `json:"focused"`
	Number      int    `json:"number"`
	PaneCount   int    `json:"pane_count"`
}

type Tab struct {
	Label       string `json:"label"`
	TabID       string `json:"tab_id"`
	WorkspaceID string `json:"workspace_id"`
	PaneCount   int    `json:"pane_count"`
}

type Pane struct {
	Title       string `json:"title"`
	StripTitle  string `json:"terminal_title_stripped"`
	PaneID      string `json:"pane_id"`
	WorkspaceID string `json:"workspace_id"`
	TabID       string `json:"tab_id"`
	Cwd         string `json:"cwd"`
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// Catalog builds the full unranked catalog (source.sh sections 1-3).
// A nil snap (herdr unavailable) behaves like the bash empty-snapshot path.
func Catalog(snap *snapshot, activePane string) []ranking.Row {
	var rows []ranking.Row
	emit := func(label, command string) {
		rows = append(rows, ranking.Row{Label: label, Command: command})
	}
	ACT := actPath()
	or := func(s, fallback string) string {
		if s != "" {
			return s
		}
		return fallback
	}

	// ---- Section 1: actions on the CURRENT pane / tab / workspace ----
	P := "[pane]"
	emit(P+" zoom toggle", "herdr pane zoom --current --toggle")
	emit(P+" zoom on", "herdr pane zoom --current --on")
	emit(P+" zoom off", "herdr pane zoom --current --off")
	emit(P+" split right (keep focus)", "herdr pane split --current --direction right --no-focus")
	emit(P+" split right (focus new)", "herdr pane split --current --direction right --focus")
	emit(P+" split down (keep focus)", "herdr pane split --current --direction down --no-focus")
	emit(P+" split down (focus new)", "herdr pane split --current --direction down --focus")
	emit(P+" resize ←", "herdr pane resize --current --direction left --amount 0.05")
	emit(P+" resize →", "herdr pane resize --current --direction right --amount 0.05")
	emit(P+" resize ↑", "herdr pane resize --current --direction up --amount 0.05")
	emit(P+" resize ↓", "herdr pane resize --current --direction down --amount 0.05")
	emit(P+" swap ←", "herdr pane swap --current --direction left")
	emit(P+" swap →", "herdr pane swap --current --direction right")
	emit(P+" swap ↑", "herdr pane swap --current --direction up")
	emit(P+" swap ↓", "herdr pane swap --current --direction down")
	emit(P+" move → new tab", ACT+" move-new-tab")
	emit(P+" move → new workspace", ACT+" move-new-workspace")
	emit(P+" move → existing tab…", ACT+" move-to-tab")
	emit(P+" rename…", ACT+" rename-pane")
	emit(P+" close…", ACT+" close-pane")
	emit(P+" send Esc", ACT+" send-key-current esc")
	emit(P+" send Ctrl+C", ACT+" send-key-current ctrl+c")
	emit(P+" info: layout", "herdr pane layout --current")
	emit(P+" info: process", "herdr pane process-info --current")
	emit(P+" info: edges", "herdr pane edges --current")
	emit(P+" info: full get", "herdr pane current --current")

	T := "[tab]"
	emit(T+" rename…", ACT+" rename-tab")
	emit(T+" new tab (cwd = here)", ACT+" new-tab")
	emit(T+" close…", ACT+" close-tab")

	W := "[workspace]"
	emit(W+" rename…", ACT+" rename-workspace")
	emit(W+" new workspace (cwd = here)", ACT+" new-workspace")
	emit(W+" close…", ACT+" close-workspace")

	S := "[server/session]"
	emit(S+" reload config", "herdr server reload-config")
	emit(S+" status", "herdr status server")
	emit(S+" sessions list", "herdr session list")
	emit(S+" worktree list", "herdr worktree list")
	emit(S+" notifications show", "herdr notification show")

	if snap == nil {
		snap = &snapshot{}
	}

	// ---- Section 2: live targets ----
	for _, a := range snap.Result.Snapshot.Agents {
		if a.PaneID == "" {
			continue
		}
		title := clean(firstNonEmpty(a.Title, a.StripTitle, a.PaneID))
		tag := "[agent " + a.Agent + " " + a.AgentStatus + "]"
		emit(tag+" focus · "+title, "herdr agent focus "+a.PaneID)
		emit(tag+" read · "+title, "herdr agent read "+a.PaneID+" --lines 60")
		emit(tag+" attach · "+title, "herdr agent attach "+a.PaneID)
		emit(tag+" prompt · "+title, ACT+" agent-prompt "+a.PaneID)
		emit(tag+" rename · "+title, ACT+" rename-agent "+a.PaneID)
		emit(tag+" send Esc · "+title, ACT+" agent-key "+a.PaneID+" esc")
		emit(tag+" send Ctrl+C · "+title, ACT+" agent-key "+a.PaneID+" ctrl+c")
		emit(tag+" explain detection · "+title, "herdr agent explain "+a.PaneID)
	}

	for _, w := range snap.Result.Snapshot.Workspaces {
		label := clean(or(w.Label, w.WorkspaceID))
		star := ""
		if w.Focused {
			star = ", focused"
		}
		emit("[workspace] goto · "+label+fmt.Sprintf(" (%d panes%s)", w.PaneCount, star),
			"herdr workspace focus "+w.WorkspaceID)
		emit("[workspace] rename · "+label, ACT+" rename-workspace-id "+w.WorkspaceID)
		emit("[workspace] close · "+label+"…", ACT+" close-workspace-id "+w.WorkspaceID)
	}

	for _, t := range snap.Result.Snapshot.Tabs {
		label := clean(or(t.Label, t.TabID))
		emit("[tab] goto · "+t.WorkspaceID+" / "+label+fmt.Sprintf(" (%d panes)", t.PaneCount),
			"herdr tab focus "+t.TabID)
		emit("[tab] rename · "+t.WorkspaceID+" / "+label+"…", ACT+" rename-tab-id "+t.TabID)
		emit("[tab] close · "+t.WorkspaceID+" / "+label+"…", ACT+" close-tab-id "+t.TabID)
	}

	for _, p := range snap.Result.Snapshot.Panes {
		title := clean(firstNonEmpty(p.Title, p.StripTitle, p.PaneID))
		mark := ""
		if p.PaneID == activePane {
			mark = ", active"
		}
		emit("[pane] zoom · "+title+" ("+p.WorkspaceID+"/"+p.PaneID+mark+")",
			"herdr pane zoom "+p.PaneID+" --toggle")
		emit("[pane] read · "+title+" ("+p.WorkspaceID+"/"+p.PaneID+mark+")",
			"herdr pane read "+p.PaneID+" --lines 60")
		emit("[pane] close · "+title+" ("+p.WorkspaceID+"/"+p.PaneID+mark+")…",
			ACT+" close-pane-id "+p.PaneID)
	}

	// ---- Section 3: raw API introspection ----
	emit("[api] browse all operations…", selfPath+" api")
	emit("[api] execute operation…", selfPath+" execute --pick")
	emit("[api] snapshot", "herdr api snapshot")
	emit("[api] schema", "herdr api schema --json")

	return rows
}

// FetchSnapshot runs herdr api snapshot; on any failure returns nil (parity
// with the bash `|| SNAP='{"result":{"snapshot":{}}}'` fallback).
func FetchSnapshot() (*snapshot, error) {
	out, err := exec.Command(pluginroot.HerdrBinary(), "api", "snapshot").Output()
	if err != nil {
		return nil, err
	}
	var snap snapshot
	if err := json.Unmarshal(out, &snap); err != nil {
		return nil, err
	}
	return &snap, nil
}

// Source emits the full manifest: ranked (*) head (when the log exists) then
// the catalog. Output is byte-compatible with bash source.sh.
func Source(selectionLog string, now time.Time) (string, error) {
	snap, _ := FetchSnapshot()
	activePane := os.Getenv("HERDR_ACTIVE_PANE_ID")
	if activePane == "" && snap != nil {
		activePane = snap.Result.Snapshot.FocusedPaneID
	}
	rows := Catalog(snap, activePane)

	var b strings.Builder
	if data, err := os.ReadFile(selectionLog); err == nil && len(data) > 0 {
		records := ranking.ParseRecords(data)
		if len(records) > 0 {
			b.WriteString(ranking.Format(ranking.Ranked(rows, records, now)))
		}
	}
	b.WriteString(ranking.Format(rows))
	return b.String(), nil
}

// Format renders catalog rows as LABEL<TAB>COMMAND lines.
func Format(rows []ranking.Row) string {
	return ranking.Format(rows)
}
