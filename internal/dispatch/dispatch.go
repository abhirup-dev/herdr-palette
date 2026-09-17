// Package dispatch is the palette's single executor, ported from bash
// dispatch.sh. It runs commands with the terminal passed through (prompts,
// fzf, and attach keep working), captures stderr for the error report, and
// on failure renders the same red report surface (command, exit code, parsed
// herdr JSON error, press-any-key wait) so errors can never vanish. On
// success it appends {ts,label,command} to selections.jsonl and pages
// substantial output through less.
package dispatch

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	json "github.com/bytedance/sonic"
)

// Command kinds the dispatcher accepts:
//   - a direct argv (rendered api operations, herdr CLI rows) — exec directly
//   - a self-invocation "<binary> act <name> [args…]" — exec directly too;
//     both arrive as parsed words, never re-parsed from a shell string.

// Run executes argv, reports failures, and logs successful selections.
func Run(argv []string, label string) error {
	if len(argv) == 0 {
		return nil
	}
	display := strings.Join(argv, " ")

	capFile, err := os.CreateTemp(os.TempDir(), "herdr-palette.")
	if err == nil {
		defer os.Remove(capFile.Name())
	}

	rc, err := runPassthrough(argv, capFile)
	if rc != 0 || err != nil {
		reportFailure(display, rc, capFile)
		if err != nil {
			return err
		}
		return fmt.Errorf("command failed with exit code %d", rc)
	}

	// Record only successful runs. History failure must never block an action.
	logSelection(label, display)

	// Success: page substantial output, skip trivial one-liners.
	if capFile != nil {
		if lines := countLines(capFile); lines > 5 {
			pageOutput(display, capFile, lines)
		}
	}
	return nil
}

// runPassthrough execs argv with stdio on the tty so read/fzf/attach work;
// stderr is tee'd into the capture file like dispatch.sh's `2> >(tee …)`.
func runPassthrough(argv []string, capFile *os.File) (int, error) {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	if capFile != nil {
		cmd.Stderr = io.MultiWriter(os.Stderr, capFile)
	} else {
		cmd.Stderr = os.Stderr
	}
	err := cmd.Run()
	if err == nil {
		return 0, nil
	}
	var ee *exec.ExitError
	if ok := errorsAs(err, &ee); ok {
		return ee.ExitCode(), nil
	}
	return 1, err
}

func errorsAs(err error, target **exec.ExitError) bool {
	ee, ok := err.(*exec.ExitError)
	if ok {
		*target = ee
	}
	return ok
}

// reportFailure renders dispatch.sh's failure surface: red rule, command,
// exit code, herdr JSON error message (parsed from captured stderr), wait.
func reportFailure(display string, rc int, capFile *os.File) {
	fmt.Fprintf(os.Stderr, "\n\033[1;31m── palette: command failed ────────────────────────────\033[0m\n")
	fmt.Fprintf(os.Stderr, "\033[1m$\033[0m %s\n", display)
	fmt.Fprintf(os.Stderr, "\033[1mexit:\033[0m %d\n", rc)
	if msg := herdrErrorMessage(capFile); msg != "" {
		fmt.Fprintf(os.Stderr, "\033[1mherdr:\033[0m %s\n", msg)
	}
	fmt.Fprintf(os.Stderr, "\033[1;31m───────────────────────────────────────────────────────\033[0m\n")
	fmt.Fprint(os.Stderr, "press any key to close\n")
	waitForKey()
}

// herdrErrorMessage extracts the message of the first {"error":…} JSON line
// in the captured stderr, mirroring dispatch.sh's grep+jq extraction.
func herdrErrorMessage(capFile *os.File) string {
	if capFile == nil {
		return ""
	}
	data, err := os.ReadFile(capFile.Name())
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		idx := strings.Index(line, `{"error"`)
		if idx < 0 {
			continue
		}
		payload := line[idx:]
		var doc struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal([]byte(payload), &doc); err != nil {
			continue
		}
		if doc.Error.Message != "" {
			return doc.Error.Message
		}
		// `jq '.error.message // .error'`: fall back to the raw object.
		var raw any
		if err := json.Unmarshal([]byte(payload), &raw); err == nil {
			if m, ok := raw.(map[string]any); ok {
				if e, ok := m["error"].(map[string]any); ok {
					if s, ok := e["message"].(string); ok && s != "" {
						return s
					}
				}
			}
		}
		return ""
	}
	return ""
}

// waitForKey blocks for one keypress on the controlling terminal.
func waitForKey() {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return
	}
	defer tty.Close()
	reader := bufio.NewReader(tty)
	_, _ = reader.ReadByte()
}

// countLines counts newline-terminated lines in the capture file.
func countLines(f *os.File) int {
	if _, err := f.Seek(0, 0); err != nil {
		return 0
	}
	n := 0
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		n++
	}
	return n
}

// pageOutput pipes the capture through less -R behind a header line,
// matching dispatch.sh's success pager.
func pageOutput(display string, capFile *os.File, lines int) {
	if _, err := capFile.Seek(0, 0); err != nil {
		return
	}
	less := exec.Command("less", "-R")
	less.Stdin = capFile
	less.Stdout = os.Stdout
	less.Stderr = os.Stderr
	fmt.Fprintf(os.Stderr, "\033[1m$ %s\033[0m — exit 0, %d lines (q to close)\n\n", display, lines)
	_ = less.Run()
}

// CacheRoot mirrors dispatch.sh: $HERDR_PALETTE_CACHE_DIR, else
// $XDG_CACHE_HOME/herdr-palette, else ~/.cache/herdr-palette.
func CacheRoot() string {
	if v := os.Getenv("HERDR_PALETTE_CACHE_DIR"); v != "" {
		return v
	}
	if v := os.Getenv("XDG_CACHE_HOME"); v != "" {
		return filepath.Join(v, "herdr-palette")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "herdr-palette")
}

// logSelection appends one JSONL record; failures are silently ignored.
func logSelection(label, command string) {
	root := CacheRoot()
	_ = os.MkdirAll(root, 0o755)
	rec := struct {
		Ts      string `json:"ts"`
		Label   string `json:"label"`
		Command string `json:"command"`
	}{
		Ts:      time.Now().UTC().Format("2006-01-02T15:04:05Z"),
		Label:   label,
		Command: command,
	}
	data, err := json.Marshal(rec)
	if err != nil {
		return
	}
	f, err := os.OpenFile(filepath.Join(root, "selections.jsonl"),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(data, '\n'))
}

// ArgvFromB64Words decodes a slice of base64 words into argv elements — the
// byte-safe channel previously used between the renderer and dispatch.
func ArgvFromB64Words(words []string) ([]string, error) {
	out := make([]string, 0, len(words))
	for _, w := range words {
		b, err := base64.StdEncoding.DecodeString(w)
		if err != nil {
			return nil, fmt.Errorf("decode argv word: %w", err)
		}
		out = append(out, string(b))
	}
	return out, nil
}

var _ = time.Now // keep time import for logSelection
