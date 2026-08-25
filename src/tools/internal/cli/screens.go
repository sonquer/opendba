package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sonquer/opendba/src/tools/internal/envfile"
	"github.com/sonquer/opendba/src/tools/internal/exec"
	"github.com/sonquer/opendba/src/tools/internal/workspace"
)

// script is one path through the interface, named so that a change can be
// pinned to the screen it moved.
type script struct {
	name string
	keys string
}

var scripts = []script{
	{"dashboard", "shot:dashboard"},
	{"editor", "e,shot:editor"},
	{"ask", "a,shot:ask"},
	{"chooser", "a,enter,shot:chooser"},
	{"ai", "ctrl+k,ai settings,enter,shot:ai"},
	{"tables", "s,shot:tables"},
	{"indexes", "i,shot:indexes"},
	{"tree", "e,tab,space,shot:tree"},
	{"results", "e,SELECT * FROM orders,ctrl+r,shot:results"},
	{"record", "e,SELECT * FROM orders,ctrl+r,tab,enter,shot:record"},
	{"completion", "e,SELECT * FROM ord,shot:completion"},
	{"switcher", "ctrl+p,shot:switcher"},
	{"switcher-twins", "ctrl+p,down,enter,ctrl+p,shot:switcher-twins"},
	{"tabs", "e,ctrl+n,ctrl+p,down,enter,e,shot:tabs"},
	{"catalog", "ctrl+d,shot:catalog"},
	{"palette", "ctrl+k,shot:palette"},
	{"help", "?,shot:help"},
	{"quit", "q,shot:quit"},
}

var sizes = []string{"120x36", "80x24"}

// screensDir is where rendered screens land when nobody says otherwise.
const screensDir = ".screens"

// screens renders every screen at every size, so a change can be looked at
// everywhere at once instead of one screenshot at a time.
func (a App) screens(ctx context.Context, opts options) int {
	space, err := workspace.Discover(a.Dir)
	if err != nil {
		fmt.Fprintln(a.Stderr, err)
		return ExitFailure
	}
	values, err := envfile.Load(space.Root)
	if err != nil {
		fmt.Fprintln(a.Stderr, err)
		return ExitFailure
	}
	out := opts.out
	if out == "" {
		out = filepath.Join(space.Root, screensDir)
	}
	environment := values.Environment(os.Environ())

	changed, written := 0, 0
	for _, size := range sizes {
		for _, current := range scripts {
			drawn, err := a.draw(ctx, space, environment, opts, size, current)
			if err != nil {
				fmt.Fprintln(a.Stderr, err)
				return ExitFailure
			}
			path := filepath.Join(out, size, current.name+".txt")
			before, _ := os.ReadFile(path)
			if string(before) != drawn {
				changed++
				if opts.against != "" {
					fmt.Fprintf(a.Stdout, "%s %s changed\n", size, current.name)
					fmt.Fprintln(a.Stdout, difference(string(before), drawn))
				}
			}
			if err := write(path, drawn); err != nil {
				fmt.Fprintln(a.Stderr, err)
				return ExitFailure
			}
			written++
		}
	}
	fmt.Fprintf(a.Stdout, "%d screens in %s, %d of them different\n", written, out, changed)
	return ExitOK
}

func (a App) draw(ctx context.Context, space workspace.Workspace, environment []string,
	opts options, size string, current script,
) (string, error) {
	args := []string{"run", "./src/cli/cmd/screens", "--plain", "--size", size, "--keys", current.keys}
	if opts.connection != "" {
		args = append(args, "--connection", opts.connection)
	}
	runner := a.Runner
	if runner == nil {
		runner = exec.OS{Env: environment}
	}
	result, err := runner.Run(ctx, space.Root, "go", args...)
	if err != nil {
		return "", fmt.Errorf("render %s at %s: %w", current.name, size, err)
	}
	if !result.OK() {
		return "", fmt.Errorf("render %s at %s: %s", current.name, size, result.Output())
	}
	return result.Stdout, nil
}

func write(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

// difference shows the lines that moved, which is the whole point of running
// this twice.
func difference(before, after string) string {
	was := strings.Split(before, "\n")
	now := strings.Split(after, "\n")
	var lines []string
	for i := 0; i < max(len(was), len(now)); i++ {
		left, right := at(was, i), at(now, i)
		if left == right {
			continue
		}
		if strings.TrimSpace(left) != "" {
			lines = append(lines, "  - "+strings.TrimRight(left, " "))
		}
		if strings.TrimSpace(right) != "" {
			lines = append(lines, "  + "+strings.TrimRight(right, " "))
		}
	}
	if len(lines) == 0 {
		return "  (only spacing)"
	}
	return strings.Join(lines, "\n")
}

func at(lines []string, i int) string {
	if i < len(lines) {
		return lines[i]
	}
	return ""
}
