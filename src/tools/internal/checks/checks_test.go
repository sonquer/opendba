package checks

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sonquer/opendba/src/tools/internal/core"
	"github.com/sonquer/opendba/src/tools/internal/exec"
	"github.com/sonquer/opendba/src/tools/internal/policy"
	"github.com/sonquer/opendba/src/tools/internal/toolbin"
	"github.com/sonquer/opendba/src/tools/internal/workspace"
	"github.com/sonquer/opendba/src/tools/pkg/cover"
)

type fakeRunner struct {
	results     map[string]exec.Result
	errs        map[string]error
	calls       []string
	dirs        []string
	profile     string
	buildsTools bool
	toolExit    int
	toolOutput  string
	toolErr     error
}

func (f *fakeRunner) Run(_ context.Context, dir string, name string, args ...string) (exec.Result, error) {
	command := exec.Format(name, args...)
	f.calls = append(f.calls, command)
	f.dirs = append(f.dirs, dir)
	if err, ok := f.errs[name]; ok {
		return exec.Result{}, err
	}
	if len(args) > 1 && args[0] == "build" && args[1] == "-o" {
		if !f.buildsTools {
			return exec.Result{ExitCode: 1, Stderr: "cannot build the tool"}, nil
		}
		if err := os.WriteFile(args[2], []byte("binary"), 0o755); err != nil {
			return exec.Result{}, err
		}
		return exec.Result{}, nil
	}
	if strings.Contains(name, string(filepath.Separator)) {
		if f.toolErr != nil {
			return exec.Result{}, f.toolErr
		}
		return exec.Result{ExitCode: f.toolExit, Stdout: f.toolOutput}, nil
	}
	for _, arg := range args {
		if strings.HasPrefix(arg, "-coverprofile=") && f.profile != "" {
			if err := os.WriteFile(strings.TrimPrefix(arg, "-coverprofile="), []byte(f.profile), 0o600); err != nil {
				return exec.Result{}, err
			}
		}
	}
	key := name + " " + firstArg(args)
	result, ok := f.results[key]
	if !ok {
		result = exec.Result{}
	}
	result.Command = command
	result.Dir = dir
	return result, nil
}

func firstArg(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return args[0]
}

const profileAllCovered = `mode: atomic
example.com/m/src/cli/a/one.go:3.10,5.2 2 1
example.com/m/src/cli/b/two.go:3.10,5.2 2 1
`

const profileHalfCovered = `mode: atomic
example.com/m/src/cli/a/one.go:3.10,5.2 2 1
example.com/m/src/cli/b/two.go:3.10,5.2 2 0
`

func testModule(t *testing.T) workspace.Module {
	t.Helper()
	return workspace.Module{Name: "cli", Path: "example.com/m/src/cli", Dir: t.TempDir()}
}

func TestCommentsCheckPasses(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package a\n\nfunc A() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	report, err := Comments(root).Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report.Status != core.StatusPass || report.Summary != "no comments found" {
		t.Fatalf("report = %+v", report)
	}
	if Comments(root).Name() != "comments" || Comments(root).Describe() == "" {
		t.Error("check metadata missing")
	}
}

func TestCommentsCheckFails(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package a\n\nfunc A() {\n\t// nope\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	report, err := Comments(root).Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report.Status != core.StatusFail || len(report.Detail) != 1 {
		t.Fatalf("report = %+v", report)
	}
}

func TestCommentsCheckPropagatesScanError(t *testing.T) {
	if _, err := Comments(filepath.Join(t.TempDir(), "missing")).Run(context.Background()); err == nil {
		t.Fatal("want scan error")
	}
}

func TestFormatCheck(t *testing.T) {
	module := testModule(t)
	cases := []struct {
		name   string
		result exec.Result
		want   core.Status
	}{
		{"clean", exec.Result{}, core.StatusPass},
		{"rewrote files", exec.Result{Stdout: "a.go\n"}, core.StatusFail},
		{"command failed", exec.Result{ExitCode: 1}, core.StatusFail},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			runner := &fakeRunner{results: map[string]exec.Result{"go fmt": c.result}}
			check := Format(module, runner)
			report, err := check.Run(context.Background())
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if report.Status != c.want {
				t.Errorf("status = %v, want %v", report.Status, c.want)
			}
			if check.Name() != "format:cli" || check.Describe() == "" {
				t.Errorf("metadata = %q", check.Name())
			}
		})
	}
}

func TestBuildCheck(t *testing.T) {
	module := testModule(t)
	runner := &fakeRunner{results: map[string]exec.Result{"go build": {ExitCode: 2, Stderr: "boom\n"}}}
	report, err := Build(module, runner).Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report.Status != core.StatusFail || report.Summary != "build failed" || len(report.Detail) != 1 {
		t.Fatalf("report = %+v", report)
	}

	ok := &fakeRunner{results: map[string]exec.Result{"go build": {}}}
	report, err = Build(module, ok).Run(context.Background())
	if err != nil || report.Status != core.StatusPass || report.Summary != "compiles" {
		t.Fatalf("report = %+v err = %v", report, err)
	}
}

func TestCommandCheckPropagatesRunnerError(t *testing.T) {
	runner := &fakeRunner{errs: map[string]error{"go": errors.New("no toolchain")}}
	if _, err := Build(testModule(t), runner).Run(context.Background()); err == nil {
		t.Fatal("want runner error")
	}
}

func toolOptions(t *testing.T, module workspace.Module, runner exec.Runner) Options {
	t.Helper()
	tools := t.TempDir()
	bin := filepath.Join(t.TempDir(), "bin")
	return Options{
		Workspace: workspace.Workspace{Root: t.TempDir(), Modules: []workspace.Module{module}},
		Runner:    runner,
		Policy:    policy.Default(),
		Builder:   toolbin.Builder{Runner: runner, ToolsDir: tools, BinDir: bin},
	}
}

func TestToolChecks(t *testing.T) {
	module := testModule(t)
	cases := map[string]func(Options, workspace.Module) core.Check{"lint": Lint, "vuln": Vulnerabilities}
	for name, build := range cases {
		t.Run(name+" clean", func(t *testing.T) {
			runner := &fakeRunner{buildsTools: true}
			check := build(toolOptions(t, module, runner), module)
			report, err := check.Run(context.Background())
			if err != nil || report.Status != core.StatusPass {
				t.Fatalf("report = %+v err = %v", report, err)
			}
			if check.Name() != name+":cli" || check.Describe() == "" {
				t.Errorf("metadata = %q", check.Name())
			}
		})
		t.Run(name+" findings", func(t *testing.T) {
			runner := &fakeRunner{buildsTools: true, toolExit: 1, toolOutput: "a.go:1: issue"}
			report, err := build(toolOptions(t, module, runner), module).Run(context.Background())
			if err != nil || report.Status != core.StatusFail || len(report.Detail) != 1 {
				t.Fatalf("report = %+v err = %v", report, err)
			}
		})
		t.Run(name+" cannot be built", func(t *testing.T) {
			runner := &fakeRunner{}
			report, err := build(toolOptions(t, module, runner), module).Run(context.Background())
			if err != nil {
				t.Fatalf("a tool that cannot be built is a finding, not a crash: %v", err)
			}
			if report.Status != core.StatusFail || len(report.Detail) == 0 {
				t.Fatalf("report = %+v", report)
			}
		})
		t.Run(name+" cannot be run", func(t *testing.T) {
			runner := &fakeRunner{buildsTools: true, toolErr: errors.New("permission denied")}
			if _, err := build(toolOptions(t, module, runner), module).Run(context.Background()); err == nil {
				t.Fatal("want an error")
			}
		})
	}
}

func coverageOptions(t *testing.T, module workspace.Module, runner exec.Runner, min float64) Options {
	t.Helper()
	return Options{
		Workspace:   workspace.Workspace{Root: t.TempDir(), Modules: []workspace.Module{module}},
		Runner:      runner,
		Policy:      policy.Policy{Coverage: policy.Coverage{Total: min}},
		CoverageDir: filepath.Join(t.TempDir(), "coverage"),
	}
}

func TestCoverageCheckPasses(t *testing.T) {
	module := testModule(t)
	runner := &fakeRunner{results: map[string]exec.Result{"go test": {}}, profile: profileAllCovered}
	opts := coverageOptions(t, module, runner, 95)
	check := Coverage(opts, module)

	report, err := check.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report.Status != core.StatusPass {
		t.Fatalf("report = %+v", report)
	}
	if !strings.Contains(report.Summary, "100.0%") {
		t.Errorf("summary = %q", report.Summary)
	}
	if _, err := os.Stat(ProfilePath(opts.CoverageDir, module)); err != nil {
		t.Errorf("coverage profile missing: %v", err)
	}
	if check.Name() != "cover:cli" || !strings.Contains(check.Describe(), "95") {
		t.Errorf("metadata = %q %q", check.Name(), check.Describe())
	}
}

func TestCoverageCheckFailsBelowGate(t *testing.T) {
	module := testModule(t)
	runner := &fakeRunner{results: map[string]exec.Result{"go test": {}}, profile: profileHalfCovered}
	report, err := Coverage(coverageOptions(t, module, runner, 95), module).Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report.Status != core.StatusFail {
		t.Fatalf("report = %+v", report)
	}
	if len(report.Detail) != 1 || !strings.Contains(report.Detail[0], "below") {
		t.Errorf("detail = %v", report.Detail)
	}
}

func TestCoverageCheckFailsWhenTestsFail(t *testing.T) {
	module := testModule(t)
	runner := &fakeRunner{results: map[string]exec.Result{"go test": {ExitCode: 1, Stdout: "FAIL\n"}}}
	report, err := Coverage(coverageOptions(t, module, runner, 95), module).Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report.Status != core.StatusFail || report.Summary != "tests failed" {
		t.Fatalf("report = %+v", report)
	}
}

func TestCoverageCheckErrors(t *testing.T) {
	module := testModule(t)
	t.Run("runner error", func(t *testing.T) {
		runner := &fakeRunner{errs: map[string]error{"go": errors.New("nope")}}
		if _, err := Coverage(coverageOptions(t, module, runner, 95), module).Run(context.Background()); err == nil {
			t.Fatal("want runner error")
		}
	})
	t.Run("missing profile", func(t *testing.T) {
		runner := &fakeRunner{results: map[string]exec.Result{"go test": {}}}
		if _, err := Coverage(coverageOptions(t, module, runner, 95), module).Run(context.Background()); err == nil {
			t.Fatal("want profile error")
		}
	})
	t.Run("coverage directory blocked", func(t *testing.T) {
		blocked := filepath.Join(t.TempDir(), "blocked")
		if err := os.WriteFile(blocked, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		opts := coverageOptions(t, module, &fakeRunner{}, 95)
		opts.CoverageDir = blocked
		if _, err := Coverage(opts, module).Run(context.Background()); err == nil {
			t.Fatal("want directory error")
		}
	})
}

func TestSuiteCoversEveryModule(t *testing.T) {
	space := workspace.Workspace{
		Root: t.TempDir(),
		Modules: []workspace.Module{
			{Name: "cli", Path: "example.com/m/src/cli", Dir: t.TempDir()},
			{Name: "tools", Path: "example.com/m/src/tools", Dir: t.TempDir()},
		},
	}
	suite := Suite(Options{Workspace: space, Runner: &fakeRunner{}, Policy: policy.Default(), CoverageDir: t.TempDir()})
	want := []string{
		"comments", "workflows",
		"format:cli", "build:cli", "format:tools", "build:tools",
		"cover:cli", "cover:tools",
		"lint:cli", "vuln:cli", "lint:tools", "vuln:tools",
	}
	got := suite.Names()
	if len(got) != len(want) {
		t.Fatalf("suite = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("suite[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestSuiteAddsTheRaceChecksOnlyWhenAsked(t *testing.T) {
	space := workspace.Workspace{
		Root: t.TempDir(),
		Modules: []workspace.Module{
			{Name: "cli", Path: "example.com/m/src/cli", Dir: t.TempDir()},
			{Name: "tools", Path: "example.com/m/src/tools", Dir: t.TempDir()},
		},
	}
	opts := Options{Workspace: space, Runner: &fakeRunner{}, Policy: policy.Default(), CoverageDir: t.TempDir(), Race: true}
	names := Suite(opts).Names()
	for _, want := range []string{"race:cli", "race:tools"} {
		if !contains(names, want) {
			t.Errorf("suite = %v, missing %q", names, want)
		}
	}
	opts.Race = false
	for _, got := range Suite(opts).Names() {
		if strings.HasPrefix(got, "race") {
			t.Errorf("suite carries %q without Race", got)
		}
	}
}

func TestRaceRunsTheDetector(t *testing.T) {
	module := testModule(t)
	runner := &fakeRunner{}
	check := Race(module, runner)
	if check.Name() != "race:cli" || check.Describe() == "" {
		t.Fatalf("metadata = %q", check.Name())
	}
	report, err := check.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != core.StatusPass || report.Summary != "no races" {
		t.Fatalf("report = %+v", report)
	}
	if !strings.Contains(runner.calls[0], "-race") {
		t.Errorf("race check ran %q", runner.calls[0])
	}
}

func TestRaceReportsFailures(t *testing.T) {
	runner := &fakeRunner{results: map[string]exec.Result{"go test": {ExitCode: 1, Stdout: "DATA RACE"}}}
	report, err := Race(testModule(t), runner).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != core.StatusFail || report.Summary != "races or failures" || len(report.Detail) == 0 {
		t.Fatalf("report = %+v", report)
	}
}

func TestWorkflowsRunsAtTheWorkspaceRoot(t *testing.T) {
	root := t.TempDir()
	runner := &fakeRunner{buildsTools: true}
	opts := toolOptions(t, testModule(t), runner)
	opts.Workspace.Root = root
	check := Workflows(opts)
	if check.Name() != "workflows" || check.Describe() == "" {
		t.Fatalf("Name() = %q, want a check with no module suffix", check.Name())
	}
	report, err := check.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != core.StatusPass {
		t.Fatalf("report = %+v", report)
	}
	last := len(runner.calls) - 1
	if runner.dirs[last] != root {
		t.Errorf("actionlint ran in %q, want the workspace root %q", runner.dirs[last], root)
	}
	for _, want := range []string{"-shellcheck=", "-pyflakes="} {
		if !strings.Contains(runner.calls[last], want) {
			t.Errorf("actionlint ran %q, missing %q", runner.calls[last], want)
		}
	}
}

func TestWorkflowsReportsFindings(t *testing.T) {
	runner := &fakeRunner{buildsTools: true, toolExit: 1, toolOutput: "ci.yml:1:1: bad"}
	report, err := Workflows(toolOptions(t, testModule(t), runner)).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != core.StatusFail || report.Summary != "workflow findings" {
		t.Fatalf("report = %+v", report)
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestGeneratedFilterExcludesGeneratedPackages(t *testing.T) {
	root := t.TempDir()
	moduleDir := filepath.Join(root, "src", "cli")
	space := workspace.Workspace{
		Root:    root,
		Modules: []workspace.Module{{Name: "cli", Path: "example.com/m/src/cli", Dir: moduleDir}},
	}
	markedDir := filepath.Join(moduleDir, "marked")
	if err := os.MkdirAll(markedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(markedDir, "m.go"), []byte("// Code generated by x. DO NOT EDIT.\npackage marked\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	summary := cover.Summary{Files: []cover.File{
		{Path: "example.com/m/src/cli/internal/parser/generated/postgresql/parser.go"},
		{Path: "example.com/m/src/cli/internal/parser/generated/postgresql/parser_base.go"},
		{Path: "example.com/m/src/cli/marked/m.go"},
		{Path: "example.com/m/src/cli/internal/ui/theme.go"},
		{Path: "other.com/x/y.go"},
	}}
	excluded := GeneratedFilter(space, summary)

	if !excluded("example.com/m/src/cli/internal/parser/generated/postgresql") {
		t.Error("vendored generated parsers must be excluded, including their base classes")
	}
	if !excluded("example.com/m/src/cli/marked") {
		t.Error("files carrying the generated marker must be excluded")
	}
	if excluded("example.com/m/src/cli/internal/ui") {
		t.Error("hand written packages must be measured")
	}
	if excluded("other.com/x") {
		t.Error("unresolvable packages must be measured")
	}
}
