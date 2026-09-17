// Package pluginroot resolves the single home of the herdr-palette plugin:
// its repository root, which holds bindings.json and the binary under bin/.
// Resolution order: $HERDR_PLUGIN_ROOT (set by Herdr for plugin panes and
// actions), else the directory above the running binary (bin/herdr-palette).
package pluginroot

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
)

// Root returns the plugin root directory.
func Root() (string, error) {
	if v := os.Getenv("HERDR_PLUGIN_ROOT"); v != "" {
		return v, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	// Resolve symlinks so an installed copy under ~/.local/bin does not
	// masquerade as the plugin root; only a real <root>/bin layout wins.
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	dir := filepath.Dir(exe)
	if filepath.Base(dir) == "bin" {
		return filepath.Dir(dir), nil
	}
	return "", errors.New("plugin root not found: set HERDR_PLUGIN_ROOT or run from <root>/bin")
}

// Self returns the absolute path of the running binary for self-invocations
// (manifest commands like "…/herdr-palette act move-to-tab").
func Self() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		return resolved, nil
	}
	return exe, nil
}

// HerdrBinary returns the herdr executable to invoke: $HERDR_BIN_PATH if
// set, else "herdr" from PATH, else common absolute installs. The fallback
// matters when we run under television, which may trim PATH.
func HerdrBinary() string {
	if v := os.Getenv("HERDR_BIN_PATH"); v != "" {
		return v
	}
	if p, err := exec.LookPath("herdr"); err == nil && p != "" {
		return "herdr"
	}
	for _, cand := range []string{"/opt/homebrew/bin/herdr", "/usr/local/bin/herdr", os.Getenv("HOME") + "/.local/bin/herdr"} {
		if cand != "" {
			if _, err := os.Stat(cand); err == nil {
				return cand
			}
		}
	}
	return "herdr"
}
