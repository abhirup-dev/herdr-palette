package dispatch

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLogSelectionAppendsJSONL(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HERDR_PALETTE_CACHE_DIR", root)
	logSelection("lbl", "herdr pane zoom --current --toggle")
	logSelection("lbl2", "cmd2")
	data, err := os.ReadFile(filepath.Join(root, "selections.jsonl"))
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 records, got %d: %q", len(lines), data)
	}
	if !strings.Contains(lines[0], `"label":"lbl"`) || !strings.Contains(lines[0], `"command":"herdr pane zoom --current --toggle"`) {
		t.Fatalf("record malformed: %s", lines[0])
	}
	if !strings.HasPrefix(lines[0], `{"ts":"`) || !strings.HasSuffix(lines[0], "Z\"}") && !strings.Contains(lines[0], `"ts":"`) {
		t.Logf("record: %s", lines[0])
	}
}

func TestHerdrErrorMessage(t *testing.T) {
	f, _ := os.CreateTemp(t.TempDir(), "cap")
	f.WriteString("some noise\n{\"error\":{\"code\":\"x\",\"message\":\"boom\"}}\n")
	f.Close()
	if got := herdrErrorMessage(f); got != "boom" {
		t.Fatalf("message = %q, want boom", got)
	}
}

func TestHerdrErrorMessageNoError(t *testing.T) {
	f, _ := os.CreateTemp(t.TempDir(), "cap")
	f.WriteString("plain stderr\n")
	f.Close()
	if got := herdrErrorMessage(f); got != "" {
		t.Fatalf("message = %q, want empty", got)
	}
}

func TestArgvFromB64Words(t *testing.T) {
	// newline-containing argv element survives the b64 channel intact
	nl := "line one\nline two\n"
	argv, err := ArgvFromB64Words([]string{b64("herdr"), b64("agent"), b64("prompt"), b64("--text"), b64(nl)})
	if err != nil {
		t.Fatal(err)
	}
	if argv[len(argv)-1] != nl {
		t.Fatalf("newline argv mangled: %q", argv[len(argv)-1])
	}
}

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }
