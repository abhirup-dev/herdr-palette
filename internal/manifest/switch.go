package manifest

import (
	"fmt"
	"strings"
)

// SwitchSource emits the "switch surface": live navigation targets as
// LABEL<TAB>COMMAND rows, filtered by preset:
//
//	all    — workspaces, then tabs, then panes/agents (grouped ws → tab)
//	ws     — workspaces only
//	tabs   — tabs only
//	agents — panes/agents only
//
// Labels are hierarchical, mirroring herdr's structure
// (workspace → tab → agent pane) so the deepest entries carry their full
// context and the fuzzy query can hit any level:
//
//	workspace → "herdr"
//	tab       → "herdr / pi"
//	pane      → "herdr / pi / π - abhirupdas (pi)"
//
// Focused targets are skipped (you cannot switch to where you already are).
// Commands are plain herdr focus argvs, executed by the same select/dispatch
// path as actions. The channel uses no_sort so grouping stays stable.
func SwitchSource(filter string) (string, error) {
	snap, _ := FetchSnapshot()
	var b strings.Builder
	emit := func(label, command string) {
		fmt.Fprintf(&b, "%s\t%s\n", label, command)
	}
	if snap == nil {
		emit("[error] herdr snapshot unavailable", "herdr status server")
		return b.String(), nil
	}
	s := &snap.Result.Snapshot
	if filter == "" {
		filter = "all"
	}

	if filter == "all" || filter == "ws" {
		for _, w := range s.Workspaces {
			if w.Focused || w.WorkspaceID == s.FocusedWorkspaceID {
				continue
			}
			emit(clean("ws  "+firstNonEmpty(w.Label, w.WorkspaceID)), "herdr workspace focus "+w.WorkspaceID)
		}
	}

	if filter == "all" || filter == "tabs" {
		for _, t := range s.Tabs {
			if t.TabID == s.FocusedTabID && t.WorkspaceID == s.FocusedWorkspaceID {
				continue
			}
			emit(clean("tab "+wsLabel(s, t.WorkspaceID)+" / "+firstNonEmpty(t.Label, t.TabID)),
				"herdr tab focus "+t.TabID)
		}
	}

	if filter == "all" || filter == "agents" {
		kindOf := map[string]string{}
		for _, a := range s.Agents {
			kindOf[a.PaneID] = firstNonEmpty(a.Agent, "shell")
		}
		for _, p := range s.Panes {
			if p.PaneID == s.FocusedPaneID {
				continue
			}
			title := firstNonEmpty(p.StripTitle, p.Title, p.Cwd, p.PaneID)
			ws := wsLabel(s, p.WorkspaceID)
			tab := tabLabel(s, p.TabID)
			kind := kindOf[p.PaneID]
			var label string
			if kind == "" || kind == "shell" {
				label = clean(fmt.Sprintf("pane %s / %s / %s", ws, tab, title))
			} else {
				label = clean(fmt.Sprintf("agt  %s / %s / %s (%s)", ws, tab, title, kind))
			}
			emit(label, "herdr agent focus "+p.PaneID)
		}
	}
	return b.String(), nil
}

func wsLabel(s *Snapshot, wsID string) string {
	for _, w := range s.Workspaces {
		if w.WorkspaceID == wsID {
			return firstNonEmpty(w.Label, wsID)
		}
	}
	return wsID
}

func tabLabel(s *Snapshot, tabID string) string {
	for _, t := range s.Tabs {
		if t.TabID == tabID {
			return firstNonEmpty(t.Label, tabID)
		}
	}
	return tabID
}
