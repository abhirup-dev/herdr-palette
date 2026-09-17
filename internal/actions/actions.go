// Package actions ports the bash palette's act.sh: interactive/compound
// actions that need prompts, confirmations, or live-target pickers. Each
// action runs herdr CLI subcommands directly (no shell); snapshot parsing
// uses the live `herdr api snapshot` (no jq). Manifest rows self-invoke:
//
//	herdr-palette act <name> [args...]
package actions

import (
	"bufio"
	"fmt"
	"herdr-palette/internal/pluginroot"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"

	json "github.com/bytedance/sonic"
	"herdr-palette/internal/traverse"
)

// Ports are the interactive primitives act.sh used, injected for tests.
type Ports struct {
	Picker *traverse.FzfPicker
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

// Run executes one action by name. args carries positional ids/keys that the
// manifest row appended after the action name (e.g. pane ids, key names).
func Run(name string, args []string, p Ports) error {
	if p.Picker == nil {
		p.Picker = &traverse.FzfPicker{In: p.Stdin, Out: p.Stdout}
	}
	if p.Stdout == nil {
		p.Stdout = os.Stdout
	}
	if p.Stderr == nil {
		p.Stderr = os.Stderr
	}
	reader := bufio.NewReader(p.StdinOr(os.Stdin))
	ask := func(prompt string) string {
		fmt.Fprint(p.Stdout, prompt)
		line, _ := reader.ReadString('\n')
		return strings.TrimRight(line, "\r\n")
	}
	confirm := func(prompt string) bool {
		fmt.Fprintf(p.Stdout, "%s [y/N]: ", prompt)
		line, _ := reader.ReadString('\n')
		a := strings.TrimRight(line, "\r\n")
		return a == "y" || a == "Y"
	}

	need := func(n int) error {
		if len(args) < n {
			return fmt.Errorf("action %s needs %d argument(s), got %d", name, n, len(args))
		}
		return nil
	}

	// herdr runs a subcommand with the terminal passed through.
	herdr := func(hargs ...string) error {
		cmd := exec.Command(pluginroot.HerdrBinary(), hargs...)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}

	switch name {

	case "rename-pane":
		pane, err := ActivePane()
		if err != nil || pane == "" {
			return failNoActivePane(err)
		}
		label := ask("new pane label (empty to clear): ")
		if label != "" {
			return herdr("pane", "rename", pane, label)
		}
		return herdr("pane", "rename", pane, "--clear")

	case "rename-tab":
		id, err := FieldOfActivePane("tab_id")
		if err != nil || id == "" {
			return failNoActivePane(err)
		}
		label := ask("new tab label: ")
		if label == "" {
			return nil
		}
		return herdr("tab", "rename", id, label)

	case "rename-workspace":
		id, err := FieldOfActivePane("workspace_id")
		if err != nil || id == "" {
			return failNoActivePane(err)
		}
		label := ask("new workspace label: ")
		if label == "" {
			return nil
		}
		return herdr("workspace", "rename", id, label)

	case "move-new-tab":
		pane, err := ActivePane()
		if err != nil || pane == "" {
			return failNoActivePane(err)
		}
		return herdr("pane", "move", pane, "--new-tab", "--focus")

	case "move-new-workspace":
		pane, err := ActivePane()
		if err != nil || pane == "" {
			return failNoActivePane(err)
		}
		label := ask("new workspace label (empty = default): ")
		if label != "" {
			return herdr("pane", "move", pane, "--new-workspace", "--label", label, "--focus")
		}
		return herdr("pane", "move", pane, "--new-workspace", "--focus")

	case "move-to-tab":
		// pick a destination tab from the live list, then move the active pane there
		pane, err := ActivePane()
		if err != nil || pane == "" {
			return failNoActivePane(err)
		}
		ws, err := FieldOfActivePane("workspace_id")
		if err != nil {
			return err
		}
		out, err := exec.Command(pluginroot.HerdrBinary(), "tab", "list", "--workspace", ws).Output()
		if err != nil {
			return fmt.Errorf("herdr tab list: %w", err)
		}
		rows, err := TabRows(out)
		if err != nil {
			return err
		}
		sel, _ := p.Picker.Pick("move pane to which tab> ", rows)
		if sel == "" {
			return nil
		}
		tab := strings.Fields(sel)[0]
		dir, _ := p.Picker.Pick("split into "+tab+" in which direction> ", []string{"right", "down"})
		if dir == "" {
			return nil
		}
		return herdr("pane", "move", pane, "--tab", tab, "--split", dir, "--focus")

	case "new-tab":
		cwd, err := FieldOfActivePane("cwd")
		if err != nil {
			return err
		}
		label := ask("new tab label (empty = default): ")
		if label != "" {
			return herdr("tab", "create", "--cwd", cwd, "--label", label, "--focus")
		}
		return herdr("tab", "create", "--cwd", cwd, "--focus")

	case "new-workspace":
		cwd, err := FieldOfActivePane("cwd")
		if err != nil {
			return err
		}
		label := ask("new workspace label (empty = default): ")
		if label != "" {
			return herdr("workspace", "create", "--cwd", cwd, "--label", label, "--focus")
		}
		return herdr("workspace", "create", "--cwd", cwd, "--focus")

	case "close-pane":
		pane, err := ActivePane()
		if err != nil || pane == "" {
			return failNoActivePane(err)
		}
		if confirm("close pane " + pane) {
			return herdr("pane", "close", pane)
		}
		return nil

	case "close-tab":
		id, err := FieldOfActivePane("tab_id")
		if err != nil || id == "" {
			return failNoActivePane(err)
		}
		if confirm("close tab " + id) {
			return herdr("tab", "close", id)
		}
		return nil

	case "close-workspace":
		id, err := FieldOfActivePane("workspace_id")
		if err != nil || id == "" {
			return failNoActivePane(err)
		}
		if confirm("close workspace " + id) {
			return herdr("workspace", "close", id)
		}
		return nil

	case "close-pane-id": // arg: pane id (from live section)
		if err := need(1); err != nil {
			return err
		}
		if confirm("close pane " + args[0]) {
			return herdr("pane", "close", args[0])
		}
		return nil

	case "close-tab-id": // arg: tab id
		if err := need(1); err != nil {
			return err
		}
		if confirm("close tab " + args[0]) {
			return herdr("tab", "close", args[0])
		}
		return nil

	case "close-workspace-id": // arg: workspace id
		if err := need(1); err != nil {
			return err
		}
		if confirm("close workspace " + args[0]) {
			return herdr("workspace", "close", args[0])
		}
		return nil

	case "rename-agent": // arg: pane id hosting agent
		if err := need(1); err != nil {
			return err
		}
		label := ask("new agent name: ")
		if label == "" {
			return nil
		}
		return herdr("agent", "rename", args[0], label)

	case "agent-prompt": // arg: pane id — read text, prompt, wait for settle
		if err := need(1); err != nil {
			return err
		}
		text := ask("prompt text: ")
		if text == "" {
			return nil
		}
		return herdr("agent", "prompt", args[0], text, "--wait", "--timeout", "300000")

	case "send-key": // arg: pane id, key
		if err := need(2); err != nil {
			return err
		}
		return herdr("pane", "send-keys", args[0], args[1])

	case "agent-key": // arg: pane id, key
		if err := need(2); err != nil {
			return err
		}
		return herdr("agent", "send-keys", args[0], args[1])

	case "rename-workspace-id": // arg: workspace id
		if err := need(1); err != nil {
			return err
		}
		label := ask("new workspace label: ")
		if label == "" {
			return nil
		}
		return herdr("workspace", "rename", args[0], label)

	case "rename-tab-id": // arg: tab id
		if err := need(1); err != nil {
			return err
		}
		label := ask("new tab label: ")
		if label == "" {
			return nil
		}
		return herdr("tab", "rename", args[0], label)

	case "send-key-current": // arg: key — send to the invoking pane
		pane, err := ActivePane()
		if err != nil || pane == "" {
			return failNoActivePane(err)
		}
		if err := need(1); err != nil {
			return err
		}
		return herdr("pane", "send-keys", pane, args[0])

	default:
		return fmt.Errorf("unknown action: %s", name)
	}
}

func failNoActivePane(err error) error {
	if err != nil {
		return fmt.Errorf("active pane: %w", err)
	}
	return fmt.Errorf("no active pane")
}

// StdinOr defaults the interactive reader port.
func (p Ports) StdinOr(fallback io.Reader) io.Reader {
	if p.Stdin != nil {
		return p.Stdin
	}
	return fallback
}

// ---------- live snapshot access (sonic, no jq) ----------

type snapDoc struct {
	Result struct {
		Snapshot struct {
			FocusedPaneID string `json:"focused_pane_id"`
			Panes         []struct {
				PaneID      string `json:"pane_id"`
				WorkspaceID string `json:"workspace_id"`
				TabID       string `json:"tab_id"`
				Cwd         string `json:"cwd"`
			} `json:"panes"`
		} `json:"snapshot"`
	} `json:"result"`
}

func fetchSnap() (*snapDoc, error) {
	out, err := exec.Command(pluginroot.HerdrBinary(), "api", "snapshot").Output()
	if err != nil {
		return nil, err
	}
	var doc snapDoc
	if err := json.Unmarshal(out, &doc); err != nil {
		return nil, err
	}
	return &doc, nil
}

// ActivePane returns HERDR_ACTIVE_PANE_ID (popup env) or the focused pane.
// An unavailable snapshot yields "" with nil error, matching act.sh's
// `jq -r '… // empty'` silence.
func ActivePane() (string, error) {
	if p := os.Getenv("HERDR_ACTIVE_PANE_ID"); p != "" {
		return p, nil
	}
	doc, err := fetchSnap()
	if err != nil {
		return "", nil
	}
	return doc.Result.Snapshot.FocusedPaneID, nil
}

// FieldOfActivePane returns workspace_id / tab_id / cwd of the active pane;
// "" (nil error) when the pane is not found or the field is empty.
func FieldOfActivePane(field string) (string, error) {
	pane, err := ActivePane()
	if err != nil || pane == "" {
		return "", err
	}
	doc, err := fetchSnap()
	if err != nil {
		return "", nil
	}
	for _, p := range doc.Result.Snapshot.Panes {
		if p.PaneID != pane {
			continue
		}
		switch field {
		case "workspace_id":
			return p.WorkspaceID, nil
		case "tab_id":
			return p.TabID, nil
		case "cwd":
			return p.Cwd, nil
		}
	}
	return "", nil
}

// TabRows formats `herdr tab list` output as picker lines:
// "w1:t2 (label)" — port of act.sh's jq + awk pipeline.
func TabRows(listOutput []byte) ([]string, error) {
	var doc struct {
		Result struct {
			Tabs []struct {
				TabID string `json:"tab_id"`
				Label string `json:"label"`
			} `json:"tabs"`
		} `json:"result"`
	}
	if err := json.Unmarshal(listOutput, &doc); err != nil {
		return nil, fmt.Errorf("parse tab list: %w", err)
	}
	var rows []string
	for _, t := range doc.Result.Tabs {
		label := t.Label
		if label == "" {
			label = t.TabID
		}
		rows = append(rows, fmt.Sprintf("%s (%s)", t.TabID, label))
	}
	return rows, nil
}

// paneIDRe matches ids like w1:p2 inside command strings (preview port).
var paneIDRe = regexp.MustCompile(`[A-Za-z0-9]+:p[0-9]+`)

// PaneIDInCommand returns the first pane id embedded in a manifest command
// string, or "" (port of preview.sh's grep -oE).
func PaneIDInCommand(command string) string {
	m := paneIDRe.FindString(command)
	return m
}
