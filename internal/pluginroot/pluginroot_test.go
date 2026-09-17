package pluginroot

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRootFromEnv(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_ROOT", "/tmp/some-root")
	got, err := Root()
	if err != nil || got != "/tmp/some-root" {
		t.Fatalf("Root() = %q, %v; want /tmp/some-root", got, err)
	}
}

func TestRootFromExecutableParent(t *testing.T) {
	// go test runs a temp binary; only the bin/ layout heuristic applies.
	t.Setenv("HERDR_PLUGIN_ROOT", "")
	self, err := os.Executable()
	if err != nil {
		t.Skipf("executable: %v", err)
	}
	got, err := Root()
	if err != nil {
		t.Skipf("no plugin root for test binary: %v", err)
	}
	// The test binary is not under bin/, so Root must have failed loudly —
	// unless the temp dir happens to be named bin (never true for go test).
	if filepath.Base(filepath.Dir(self)) == "bin" && got != filepath.Dir(filepath.Dir(self)) {
		t.Fatalf("Root() = %q, want grandparent of binary", got)
	}
}
