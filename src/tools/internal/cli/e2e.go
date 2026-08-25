package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/sonquer/opendba/src/tools/internal/exec"
	"github.com/sonquer/opendba/src/tools/internal/workspace"
	"github.com/sonquer/opendba/src/tools/pkg/tuitest"
)

const (
	suiteDir     = "tests/e2e"
	artifactsDir = ".e2e"
	productPath  = "./src/cli/cmd/opendba"
)

// e2e walks every screen of the interface through a real terminal and fails
// when one of them is not the screen that was kept.
func (a App) e2e(ctx context.Context, opts options) int {
	space, err := workspace.Discover(a.Dir)
	if err != nil {
		fmt.Fprintln(a.Stderr, err)
		return ExitFailure
	}
	suite, binary, cleanup, err := a.prepare(ctx, space, opts)
	if err != nil {
		fmt.Fprintln(a.Stderr, err)
		return ExitFailure
	}
	defer cleanup()

	runner := tuitest.Runner{
		Suite: suite, Binary: binary, Update: opts.update, Artifact: a.artifacts(space, opts),
	}
	filter := tuitest.Filter{Scenario: opts.only, Size: opts.size}
	return a.reportE2E(tuitest.Walk(runner, filter, opts.jobs), space, opts)
}

func (a App) prepare(ctx context.Context, space workspace.Workspace, opts options,
) (tuitest.Suite, string, func(), error) {
	root := filepath.Join(space.Root, suiteDir)
	suite, err := tuitest.Load(space.Root, root)
	if err != nil {
		return tuitest.Suite{}, "", nil, err
	}
	work, err := os.MkdirTemp("", "opendba-e2e-")
	if err != nil {
		return tuitest.Suite{}, "", nil, fmt.Errorf("make room for the build: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(work) }
	if opts.binary != "" {
		cleanup()
		return suite, opts.binary, func() {}, nil
	}
	binary := filepath.Join(work, "opendba"+binarySuffix())
	runner := a.Runner
	if runner == nil {
		runner = exec.OS{}
	}
	built, err := runner.Run(ctx, space.Root, "go", "build", "-o", binary, productPath)
	if err != nil {
		cleanup()
		return tuitest.Suite{}, "", nil, fmt.Errorf("build the program: %w", err)
	}
	if !built.OK() {
		cleanup()
		return tuitest.Suite{}, "", nil, fmt.Errorf("build the program: %s", built.Output())
	}
	return suite, binary, cleanup, nil
}

func binarySuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

// artifacts is where what a failure looked like is written.
func (a App) artifacts(space workspace.Workspace, opts options) string {
	if opts.out != "" {
		return opts.out
	}
	return filepath.Join(space.Root, artifactsDir)
}

func (a App) reportE2E(results []tuitest.Result, space workspace.Workspace, opts options) int {
	artifacts := a.artifacts(space, opts)
	failed, updated := 0, 0
	for _, result := range results {
		updated += len(result.Updated)
		if result.OK() {
			fmt.Fprintf(a.Stdout, "  ok    %s %s (%s)\n",
				result.Scenario, result.Size, result.Elapsed.Round(time.Millisecond))
			continue
		}
		failed++
		fmt.Fprintf(a.Stdout, "  FAIL  %s %s\n", result.Scenario, result.Size)
		for _, failure := range result.Failures {
			fmt.Fprintln(a.Stdout, indent(failure.String()))
		}
	}
	if opts.update {
		fmt.Fprintf(a.Stdout, "%d screens kept\n", updated)
	}
	fmt.Fprintf(a.Stdout, "%d of %d screens are what they were\n", len(results)-failed, len(results))
	if failed > 0 {
		fmt.Fprintf(a.Stdout, "what the failures looked like is in %s\n", artifacts)
	}
	a.summariseE2E(results, opts)
	if failed > 0 {
		return ExitFailure
	}
	return ExitOK
}

func indent(text string) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	for i, line := range lines {
		lines[i] = "        " + line
	}
	return strings.Join(lines, "\n")
}

func (a App) summariseE2E(results []tuitest.Result, opts options) {
	if opts.summary == "" {
		return
	}
	var out strings.Builder
	out.WriteString("\n## screens\n\n| screen | size | result |\n| --- | --- | --- |\n")
	for _, result := range results {
		verdict := "ok"
		if !result.OK() {
			verdict = "**failed**"
		}
		fmt.Fprintf(&out, "| %s | %s | %s |\n", result.Scenario, result.Size, verdict)
	}
	for _, result := range results {
		if result.OK() {
			continue
		}
		fmt.Fprintf(&out, "\n### %s at %s\n\n", result.Scenario, result.Size)
		for _, failure := range result.Failures {
			fmt.Fprintf(&out, "```\n%s\n```\n", failure.String())
		}
		fmt.Fprintf(&out, "\n<details><summary>the screen</summary>\n\n```\n%s\n```\n\n</details>\n",
			result.Frame.Plain())
	}
	file, err := os.OpenFile(opts.summary, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer func() { _ = file.Close() }()
	fmt.Fprint(file, out.String())
}
