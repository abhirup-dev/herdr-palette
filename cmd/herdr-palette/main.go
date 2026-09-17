// herdr-palette owns the protocol-cache phase of the Herdr command palette.
// Later phases deliberately remain in the existing bash implementation until
// they are migrated one at a time with parity tests.
package main

import (
	"bufio"
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	json "github.com/bytedance/sonic"
	"herdr-palette/internal/actions"
	"herdr-palette/internal/datasource"
	"herdr-palette/internal/dispatch"
	"herdr-palette/internal/execute"
	"herdr-palette/internal/graph"
	"herdr-palette/internal/manifest"
	"herdr-palette/internal/schema"
	"herdr-palette/internal/traverse"
)

type current struct {
	HerdrVersion  string `json:"herdr_version"`
	Protocol      int    `json:"protocol"`
	SchemaVersion int    `json:"schema_version"`
	Cache         string `json:"cache"`
	IndexedAt     string `json:"indexed_at"`
	IndexFormat   int    `json:"index_format"`
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "index":
		err = indexMain(os.Args[2:])
	case "traverse":
		err = traverseMain(os.Args[2:])
	case "execute":
		err = executeMain(os.Args[2:])
	case "act":
		err = actMain(os.Args[2:])
	case "api":
		err = apiMain(os.Args[2:])
	case "select":
		err = selectMain(os.Args[2:])
	case "preview":
		err = previewMain(os.Args[2:])
	case "source":
		err = sourceMain(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "herdr-palette:", err)
		// Exit 5 mirrors the bash traversal's validation-failure exit code,
		// keeping wrapper parity (execute.sh treats any nonzero as failure).
		os.Exit(5)
	}
}
func usage() {
	fmt.Fprintln(os.Stderr, "usage: herdr-palette index [--check] [--schema PATH] [--graph METHOD]")
	fmt.Fprintln(os.Stderr, "       herdr-palette traverse METHOD [--answers FILE] [--print-answers-only]")
	fmt.Fprintln(os.Stderr, "       herdr-palette execute METHOD [--answers FILE] [--yes]")
	fmt.Fprintln(os.Stderr, "       herdr-palette act NAME [ARGS...]")
	fmt.Fprintln(os.Stderr, "       herdr-palette api [--reindex]")
	fmt.Fprintln(os.Stderr, "       herdr-palette select 'LABEL<TAB>COMMAND'")
	fmt.Fprintln(os.Stderr, "       herdr-palette preview 'COMMAND'")
	fmt.Fprintln(os.Stderr, "       herdr-palette source")
}

func indexMain(args []string) error {
	fs := flag.NewFlagSet("index", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	check := fs.Bool("check", false, "verify the current cache without parsing the schema")
	schemaPath := fs.String("schema", "", "read raw schema from file instead of herdr")
	method := fs.String("graph", "", "print one operation graph entry")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("invalid arguments: %w", err)
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %v", fs.Args())
	}
	if *check && (*schemaPath != "" || *method != "") {
		return errors.New("--check cannot be combined with --schema or --graph")
	}

	version, err := herdrVersion()
	if err != nil {
		return err
	}
	root := cacheRoot()
	if *check {
		path, err := checkCurrent(root, version)
		if err != nil {
			return err
		}
		fmt.Println(path)
		return nil
	}

	// An explicit schema file is a deterministic maintenance/release input and
	// therefore intentionally rebuilds even when an installed-version cache is
	// otherwise warm. The normal no-flag path is the lazy cache lifecycle.
	var path string
	if *schemaPath != "" {
		path, err = build(root, version, *schemaPath)
	} else {
		path, err = checkCurrent(root, version)
		if err != nil {
			path, err = build(root, version, "")
		}
	}
	if err != nil {
		return err
	}
	if *method != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read cache: %w", err)
		}
		var index graph.Index
		if err := json.Unmarshal(data, &index); err != nil {
			return fmt.Errorf("decode cache: %w", err)
		}
		for _, op := range index.Operations {
			if op.Method == *method {
				return writeJSON(os.Stdout, op)
			}
		}
		return fmt.Errorf("operation %q is not in the current graph", *method)
	}
	fmt.Println(path)
	return nil
}

func cacheRoot() string {
	if root := os.Getenv("HERDR_PALETTE_CACHE_DIR"); root != "" {
		return root
	}
	if root := os.Getenv("XDG_CACHE_HOME"); root != "" {
		return filepath.Join(root, "herdr-palette")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".cache/herdr-palette"
	}
	return filepath.Join(home, ".cache", "herdr-palette")
}
func herdrVersion() (string, error) {
	out, err := exec.Command("herdr", "--version").Output()
	if err != nil {
		return "", fmt.Errorf("get installed Herdr version: %w", err)
	}
	return string(bytes.TrimSpace(out)), nil
}
func checkCurrent(root, version string) (string, error) {
	data, err := os.ReadFile(filepath.Join(root, "current.json"))
	if err != nil {
		return "", fmt.Errorf("cache pointer missing: %w", err)
	}
	var pointer current
	if err := json.Unmarshal(data, &pointer); err != nil {
		return "", fmt.Errorf("cache pointer is invalid JSON: %w", err)
	}
	if pointer.HerdrVersion != version {
		return "", fmt.Errorf("cache version drift: cached %q, installed %q", pointer.HerdrVersion, version)
	}
	if pointer.IndexFormat != graph.IndexFormat {
		return "", fmt.Errorf("cache index format drift: cached %d, need %d", pointer.IndexFormat, graph.IndexFormat)
	}
	if pointer.Protocol < 0 || pointer.SchemaVersion < 0 || pointer.Cache == "" {
		return "", errors.New("cache pointer has invalid metadata")
	}
	if _, err := os.Stat(pointer.Cache); err != nil {
		return "", fmt.Errorf("cache graph missing: %w", err)
	}
	return pointer.Cache, nil
}
func build(root, version, schemaPath string) (string, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", fmt.Errorf("create cache directory: %w", err)
	}
	data, err := schemaBytes(schemaPath)
	if err != nil {
		return "", err
	}
	doc, err := schema.Parse(data)
	if err != nil {
		return "", err
	}
	if doc.Protocol < 0 || doc.SchemaVersion < 0 {
		return "", errors.New("schema has invalid protocol metadata")
	}
	indexedAt := time.Now().UTC().Format(time.RFC3339)
	index, err := graph.Build(doc, posixCksum(data), indexedAt)
	if err != nil {
		return "", err
	}
	cache := filepath.Join(root, fmt.Sprintf("protocol-%d.json", doc.Protocol))
	if err := atomicJSON(cache, index); err != nil {
		return "", err
	}
	pointer := current{HerdrVersion: version, Protocol: doc.Protocol, SchemaVersion: doc.SchemaVersion, Cache: cache, IndexedAt: indexedAt, IndexFormat: graph.IndexFormat}
	if err := atomicJSON(filepath.Join(root, "current.json"), pointer); err != nil {
		return "", err
	}
	return cache, nil
}
func schemaBytes(path string) ([]byte, error) {
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read schema %q: %w", path, err)
		}
		return data, nil
	}
	out, err := exec.Command("herdr", "api", "schema", "--json").Output()
	if err != nil {
		return nil, fmt.Errorf("read installed Herdr API schema: %w", err)
	}
	return out, nil
}
func atomicJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode JSON: %w", err)
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(path), ".herdr-palette-*")
	if err != nil {
		return fmt.Errorf("create temporary cache: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temporary cache: %w", err)
	}
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("atomically replace %q: %w", path, err)
	}
	return nil
}
func writeJSON(w io.Writer, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, string(data))
	return err
}

// posixCksum implements POSIX cksum (CRC-32 with the byte length appended),
// matching `cksum < schema` without making the graph cache depend on a shell.
func posixCksum(data []byte) string {
	var table [256]uint32
	for i := range table {
		crc := uint32(i) << 24
		for bit := 0; bit < 8; bit++ {
			if crc&0x80000000 != 0 {
				crc = crc<<1 ^ 0x04C11DB7
			} else {
				crc <<= 1
			}
		}
		table[i] = crc
	}
	crc := uint32(0)
	update := func(b byte) { crc = crc<<8 ^ table[byte(crc>>24)^b] }
	for _, b := range data {
		update(b)
	}
	for size := len(data); size > 0; size >>= 8 {
		update(byte(size))
	}
	return fmt.Sprintf("cksum:%d:%d", ^crc, len(data))
}

// ---------- traverse subcommand (Phase B) ----------

// traverseMain mirrors traverse.sh's CLI surface exactly:
//
//	herdr-palette traverse METHOD [--answers FILE] [--print-answers-only]
//
// --answers mode is fully non-interactive: it validates the given answer tree
// against the cached param tree and prints it back, making zero Herdr calls.
// Interactive mode resolves the cache like api-browser --ensure-cache, then
// walks the tree with fzf pickers and live snapshot datasources.
func traverseMain(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: herdr-palette traverse METHOD [--answers FILE] [--print-answers-only]")
	}
	method := args[0]
	answersFile := ""
	printOnly := false
	rest := args[1:]
	for len(rest) > 0 {
		switch rest[0] {
		case "--answers":
			if len(rest) < 2 {
				return errors.New("--answers needs a JSON file")
			}
			answersFile = rest[1]
			rest = rest[2:]
		case "--print-answers-only":
			printOnly = true
			rest = rest[1:]
		default:
			return errors.New("usage: herdr-palette traverse METHOD [--answers FILE] [--print-answers-only]")
		}
	}

	root := cacheRoot()
	var cachePath string
	if answersFile != "" {
		// Scripted mode: current.json lookup only — no version/schema calls.
		p, err := currentCachePath(root)
		if err != nil {
			return fmt.Errorf("cache missing; run api-browser once: %w", err)
		}
		cachePath = p
	} else {
		p, err := ensureCache(root)
		if err != nil {
			return err
		}
		cachePath = p
	}

	idx, err := loadIndex(cachePath)
	if err != nil {
		return err
	}
	op, ok := indexOperation(idx, method)
	if !ok {
		return fmt.Errorf("unknown cached operation: %s", method)
	}

	if answersFile != "" {
		data, err := os.ReadFile(answersFile)
		if err != nil {
			return fmt.Errorf("answers file not found: %s", answersFile)
		}
		ordered, err := traverse.DecodeOrdered(data)
		if err != nil {
			return fmt.Errorf("decode answers: %w", err)
		}
		answers := traverse.OrderedToPlain(ordered)
		if _, err := traverse.ValidateAnswers(op.ParamTree, answers); err != nil {
			return err
		}
		// Canonical jq-style pretty print preserving input key order.
		fmt.Println(traverse.MarshalOrdered(ordered))
		return nil
	}

	picker := &traverse.FzfPicker{In: os.Stdin, Out: os.Stdout}
	sess := traverse.New(method, picker, datasource.Fetch)
	if err := sess.Walk(op.ParamTree); err != nil {
		return err
	}
	if !printOnly {
		fmt.Printf("Answers for %s:\n", method)
	}
	out, err := sess.AnswersJSON()
	if err != nil {
		return err
	}
	fmt.Print(out)
	return nil
}

// currentCachePath resolves current.json to the cache file without any
// version check (scripted parity with traverse.sh --answers).
func currentCachePath(root string) (string, error) {
	data, err := os.ReadFile(filepath.Join(root, "current.json"))
	if err != nil {
		return "", err
	}
	var pointer current
	if err := json.Unmarshal(data, &pointer); err != nil {
		return "", err
	}
	if pointer.Cache == "" {
		return "", errors.New("current.json lacks cache path")
	}
	if _, err := os.Stat(pointer.Cache); err != nil {
		return "", err
	}
	return pointer.Cache, nil
}

// ensureCache resolves or lazily builds the cache, then returns its path.
// It reuses the index lifecycle rules; on a warm cache no schema call occurs.
func ensureCache(root string) (string, error) {
	version, err := herdrVersion()
	if err != nil {
		return "", err
	}
	if p, err := checkCurrent(root, version); err == nil {
		return p, nil
	}
	return build(root, version, "")
}

// loadIndex reads and decodes a protocol cache file.
func loadIndex(path string) (*graph.Index, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var idx graph.Index
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, fmt.Errorf("decode cache: %w", err)
	}
	return &idx, nil
}

// indexOperation finds one operation by method name.
func indexOperation(idx *graph.Index, method string) (graph.Operation, bool) {
	for _, op := range idx.Operations {
		if op.Method == method {
			return op, true
		}
	}
	return graph.Operation{}, false
}

// executeMain runs the reviewed execution pipeline (Phase C).
func executeMain(args []string) error {
	method := ""
	answersFile := ""
	assumeYes := false
	pick := false
	rest := args
	for len(rest) > 0 {
		switch rest[0] {
		case "--answers":
			if len(rest) < 2 {
				return errors.New("--answers needs a JSON file")
			}
			answersFile = rest[1]
			rest = rest[2:]
		case "--yes":
			assumeYes = true
			rest = rest[1:]
		case "--pick":
			pick = true
			rest = rest[1:]
		default:
			if method != "" {
				return errors.New("only one METHOD may be given")
			}
			method = rest[0]
			rest = rest[1:]
		}
	}
	if method == "" && !pick {
		return errors.New("usage: herdr-palette execute [METHOD] [--answers FILE] [--yes] [--pick]")
	}

	resolve := func(method string, scripted bool) (graph.Node, error) {
		root := cacheRoot()
		var cachePath string
		var err error
		if scripted {
			cachePath, err = currentCachePath(root)
		} else {
			cachePath, err = ensureCache(root)
		}
		if err != nil {
			return graph.Node{}, err
		}
		idx, err := loadIndex(cachePath)
		if err != nil {
			return graph.Node{}, err
		}
		op, ok := indexOperation(idx, method)
		if !ok {
			return graph.Node{}, fmt.Errorf("unknown cached operation: %s", method)
		}
		return op.ParamTree, nil
	}

	return execute.Run(execute.Options{
		Method:      method,
		Pick:        pick,
		AnswersFile: answersFile,
		Yes:         assumeYes,
		Stdin:       bufio.NewReader(os.Stdin),
		Stdout:      os.Stdout,
		Stderr:      os.Stderr,
		ResolveTree: resolve,
	})
}

// sourceMain emits the full palette manifest (Phase D).
func sourceMain(args []string) error {
	if len(args) > 0 && args[0] == "switch" {
		filter := "all"
		rest := args[1:]
		for len(rest) > 0 {
			switch rest[0] {
			case "--filter":
				if len(rest) < 2 {
					return errors.New("--filter needs all|ws|tabs|agents")
				}
				filter = rest[1]
				rest = rest[2:]
			default:
				return fmt.Errorf("source switch: unknown flag %s", rest[0])
			}
		}
		out, err := manifest.SwitchSource(filter)
		if err != nil {
			return err
		}
		fmt.Print(out)
		return nil
	}
	cacheRoot := manifestCacheRoot()
	out, err := manifest.Source(filepath.Join(cacheRoot, "selections.jsonl"), time.Now())
	if err != nil {
		return err
	}
	fmt.Print(out)
	return nil
}

func manifestCacheRoot() string {
	if v := os.Getenv("HERDR_PALETTE_CACHE_DIR"); v != "" {
		return v
	}
	if v := os.Getenv("XDG_CACHE_HOME"); v != "" {
		return filepath.Join(v, "herdr-palette")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "herdr-palette")
}

// ---------- act subcommand (Phase E) ----------

// actMain runs a composite action: herdr-palette act NAME [ARGS...].
// The manifest self-invokes this for interactive/compound entries; success
// is logged by the palette's normal execution path only when run through
// select/dispatch, so act itself just runs and returns.
func actMain(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: herdr-palette act NAME [ARGS...]")
	}
	return actions.Run(args[0], args[1:], actions.Ports{
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	})
}

// ---------- api subcommand (Phase E) ----------

// apiMain ports api-browser.sh: lazy cache lifecycle, fzf operation browser
// with [reindex] first row, preview via the preview subcommand, and a
// non-mutating typed traversal on selection.
func apiMain(args []string) error {
	reindex := false
	for _, a := range args {
		switch a {
		case "--reindex":
			reindex = true
		default:
			return fmt.Errorf("usage: herdr-palette api [--reindex]")
		}
	}
	root := cacheRoot()
	if reindex {
		if err := os.Remove(filepath.Join(root, "current.json")); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	cachePath, err := ensureCache(root)
	if err != nil {
		return err
	}
	idx, err := loadIndex(cachePath)
	if err != nil {
		return err
	}

	lines := []string{"__reindex__\t[reindex] rebuild cache for installed Herdr protocol"}
	for _, op := range idx.Operations {
		lines = append(lines, fmt.Sprintf("%s\t%s · %s", op.Method, op.Method, op.Description))
	}
	picker := &traverse.FzfPicker{In: os.Stdin, Out: os.Stdout}
	sel, _ := picker.PickTSV("api> ", lines)
	if sel == "" {
		return nil
	}
	method := strings.SplitN(sel, "\t", 2)[0]
	if method == "__reindex__" {
		if err := os.Remove(filepath.Join(root, "current.json")); err != nil && !os.IsNotExist(err) {
			return err
		}
		return apiMain(nil)
	}
	// Browse retains its non-mutating typed traversal; execute --pick is the
	// reviewed transport path exposed separately in the main palette.
	return traverseMain([]string{method})
}

// ---------- select subcommand (Phase E) ----------

// selectMain is the thin popup router: it takes the raw LABEL<TAB>COMMAND
// selection line and routes it into the Go dispatcher. Commands are manifest
// rows: either a direct herdr argv or a "<binary> act NAME [args]"
// self-invocation — both split on tabs/spaces without a shell.
func selectMain(args []string) error {
	var label, command string
	switch len(args) {
	case 1:
		// popup wrapper form: one LABEL<TAB>COMMAND argument
		line := args[0]
		tab := strings.IndexByte(line, '\t')
		if tab < 0 {
			return errors.New("select: expected LABEL<TAB>COMMAND")
		}
		label, command = line[:tab], line[tab+1:]
	case 2:
		// television action form: select COMMAND LABEL
		command, label = args[0], args[1]
	default:
		return errors.New("usage: herdr-palette select 'LABEL<TAB>COMMAND' | select COMMAND LABEL")
	}
	if command == "" {
		return nil
	}
	argv := strings.Fields(command)
	return dispatch.Run(argv, label)
}

// ---------- preview subcommand (Phase E) ----------

// previewMain ports preview.sh: live output for agent/pane targets
// (the pane id embedded in the command), otherwise the command plus the
// relevant herdr subcommand help.
func previewMain(args []string) error {
	command := ""
	if len(args) > 0 {
		command = args[0]
	}
	pane := actions.PaneIDInCommand(command)
	if pane != "" {
		if strings.Contains(command, "agent") {
			return passthrough("herdr", "agent", "read", pane, "--source", "recent-unwrapped", "--lines", "40")
		}
		return passthrough("herdr", "pane", "read", pane, "--source", "recent-unwrapped", "--lines", "40")
	}
	fmt.Printf("$ %s\n\n", command)
	words := strings.Fields(command)
	if len(words) >= 2 {
		return passthroughHelp(words[0], words[1])
	}
	return nil
}

func passthrough(argv ...string) error {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		// preview.sh piped stderr into stdout (2>&1) and always exited 0;
		// preview panes must never fail the channel.
		return nil
	}
	return nil
}

// passthroughHelp runs "<bin> <sub> --help" capped at 40 lines, matching
// preview.sh's `herdr "$sub" --help | head -40` for direct herdr commands.
func passthroughHelp(bin, sub string) error {
	if bin != "herdr" {
		return nil
	}
	// CombinedOutput matches preview.sh's `2>&1 | head -40`: help errors
	// still show their usage text.
	out, _ := exec.Command("herdr", sub, "--help").CombinedOutput()
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if len(lines) > 40 {
		lines = lines[:40]
	}
	fmt.Println(strings.Join(lines, "\n"))
	return nil
}
