// Package execute runs the full reviewed-execution flow in Go: traverse
// (Go) -> render (Go) -> review screen -> the Go dispatcher. The binary is
// the single executor and the writer of selections.jsonl.
package execute

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	json "github.com/bytedance/sonic"
	"herdr-palette/internal/bindings"
	"herdr-palette/internal/datasource"
	"herdr-palette/internal/dispatch"
	"herdr-palette/internal/graph"
	"herdr-palette/internal/pluginroot"
	"herdr-palette/internal/render"
	"herdr-palette/internal/traverse"
)

// bindingsPath resolves bindings.json from the plugin root (single home).
func bindingsPath() (string, error) {
	root, err := pluginroot.Root()
	if err != nil {
		return "", err
	}
	return root + "/bindings.json", nil
}

// Options mirrors execute.sh's flags.
type Options struct {
	Method      string
	AnswersFile string // --answers: deterministic, non-editable review
	Yes         bool   // --yes: accept without prompting
	Pick        bool   // --pick: fzf operation picker when METHOD is omitted
	Stdin       *bufio.Reader
	Stdout      *os.File
	Stderr      *os.File

	// Cache resolution seam (main wires these to its own helpers).
	ResolveTree func(method string, scripted bool) (graph.Node, error)
}

// Run executes one method through the reviewed pipeline. method may be
// empty with Pick set: an fzf operation picker runs first (the palette's
// "[api] execute operation…" row).
func Run(opts Options) error {
	bp, err := bindingsPath()
	if err != nil {
		return err
	}
	table, err := bindings.Load(bp)
	if err != nil {
		return err
	}
	if opts.ResolveTree == nil {
		return fmt.Errorf("execute: ResolveTree seam not wired")
	}
	if opts.Method == "" {
		if !opts.Pick {
			return fmt.Errorf("usage: herdr-palette execute METHOD [--answers FILE] [--yes]")
		}
		method, err := pickOperation(opts.Stdout, table)
		if err != nil {
			return err
		}
		if method == "" {
			return nil
		}
		opts.Method = method
	}
	// API-only refusal comes FIRST, before any traversal or prompting —
	// bash execute.sh checks availability before collecting answers too.
	if b, ok := table.Get(opts.Method); ok && b.Availability != "cli" {
		fmt.Fprintf(opts.Stderr, "palette: %s is API-only (no CLI transport registered); refusing execution.\n", opts.Method)
		return &render.Error{Code: render.CodeAPIOnly, Method: opts.Method,
			Message: "API-only (no CLI transport registered)"}
	}
	tree, err := opts.ResolveTree(opts.Method, opts.AnswersFile != "")
	if err != nil {
		return err
	}

	picker := &traverse.FzfPicker{In: os.Stdin, Out: os.Stdout}

	for {
		var answersJSON string
		if opts.AnswersFile != "" {
			// Non-interactive: decode + validate + canonical print (traverse parity).
			data, err := os.ReadFile(opts.AnswersFile)
			if err != nil {
				return fmt.Errorf("answers file not found: %s", opts.AnswersFile)
			}
			ordered, err := traverse.DecodeOrdered(data)
			if err != nil {
				return fmt.Errorf("decode answers: %w", err)
			}
			plain := traverse.OrderedToPlain(ordered)
			if _, err := traverse.ValidateAnswers(tree, plain); err != nil {
				return err
			}
			answersJSON = traverse.MarshalOrdered(ordered)
		} else {
			sess := traverse.New(opts.Method, picker, datasource.Fetch)
			if err := sess.Walk(tree); err != nil {
				return err
			}
			out, err := sess.AnswersJSON()
			if err != nil {
				return err
			}
			answersJSON = out
		}

		ordered, err := traverse.DecodeOrdered([]byte(answersJSON))
		if err != nil {
			return fmt.Errorf("decode answers: %w", err)
		}

		argv, rerr := render.RenderOrdered(opts.Method, table, tree, ordered)
		if rerr != nil {
			fmt.Fprintln(opts.Stderr, "palette: render failed:")
			enc, _ := json.MarshalIndent(rerr, "", "  ")
			fmt.Fprintln(opts.Stderr, string(enc))
			return rerr
		}

		// Review screen: identical layout to the bash jq review.
		answersTrim := strings.TrimRight(answersJSON, "\n")
		fmt.Fprintf(opts.Stdout, "{\n  \"method\": %q,\n  \"answers\": %s,\n  \"argv\": [", opts.Method, answersTrim)
		for i, a := range argv {
			if i > 0 {
				fmt.Fprint(opts.Stdout, ",")
			}
			fmt.Fprintf(opts.Stdout, "\n    %s", mustJSON(a))
		}
		fmt.Fprint(opts.Stdout, "\n  ]\n}\n")

		var choice string
		if opts.Yes {
			choice = "y"
		} else {
			fmt.Fprint(opts.Stdout, "Execute this operation? [y]es / [n]o / [e]dit answers / [q]uit: ")
			line, _ := opts.Stdin.ReadString('\n')
			choice = strings.TrimSpace(line)
		}
		switch choice {
		case "y", "Y", "yes":
			return dispatch.Run(argv, "[api] "+opts.Method)
		case "e", "E", "edit":
			if opts.AnswersFile != "" {
				fmt.Fprintln(opts.Stderr, "palette: --answers input cannot be edited interactively")
				return fmt.Errorf("--answers input cannot be edited interactively")
			}
			continue
		case "n", "N", "no", "q", "Q", "quit", "":
			return nil
		default:
			fmt.Fprintln(opts.Stderr, "please choose y, n, e, or q")
		}
	}
}

// pickOperation lists cached operations with their binding availability
// (port of execution-operation-lines.jq + fzf) and returns the selection.
func pickOperation(out *os.File, table bindings.Table) (string, error) {
	root := os.Getenv("HERDR_PALETTE_CACHE_DIR")
	if root == "" {
		if v := os.Getenv("XDG_CACHE_HOME"); v != "" {
			root = v + "/herdr-palette"
		} else {
			home, _ := os.UserHomeDir()
			root = home + "/.cache/herdr-palette"
		}
	}
	path, err := currentCacheFile(root)
	if err != nil {
		return "", err
	}
	lines, err := operationLines(path, table)
	if err != nil {
		return "", err
	}
	picker := &traverse.FzfPicker{In: os.Stdin, Out: out}
	sel, _ := picker.PickTSV("execute> ", lines)
	if sel == "" {
		return "", nil
	}
	return strings.SplitN(sel, "\t", 2)[0], nil
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// currentCacheFile resolves current.json to the graph cache path.
func currentCacheFile(root string) (string, error) {
	data, err := os.ReadFile(filepath.Join(root, "current.json"))
	if err != nil {
		return "", fmt.Errorf("cache pointer missing; run herdr-palette index: %w", err)
	}
	var pointer struct {
		Cache string `json:"cache"`
	}
	if err := json.Unmarshal(data, &pointer); err != nil {
		return "", fmt.Errorf("cache pointer invalid: %w", err)
	}
	if pointer.Cache == "" {
		return "", fmt.Errorf("cache pointer lacks cache path")
	}
	return pointer.Cache, nil
}

// operationLines renders cache operations as METHOD<TAB>display rows with
// availability from the bindings table (api-only fallback), port of
// execution-operation-lines.jq.
func operationLines(cachePath string, table bindings.Table) ([]string, error) {
	data, err := os.ReadFile(cachePath)
	if err != nil {
		return nil, err
	}
	var idx struct {
		Operations []struct {
			Method      string `json:"method"`
			Description string `json:"description"`
		} `json:"operations"`
	}
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, fmt.Errorf("decode cache: %w", err)
	}
	lines := make([]string, 0, len(idx.Operations))
	for _, op := range idx.Operations {
		availability := "api-only"
		if b, ok := table.Get(op.Method); ok {
			availability = b.Availability
		}
		lines = append(lines, fmt.Sprintf("%s\t[%s] %s · %s", op.Method, availability, op.Method, op.Description))
	}
	return lines, nil
}
