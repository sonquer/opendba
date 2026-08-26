package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/sonquer/opendba/src/tools/internal/core"
	"github.com/sonquer/opendba/src/tools/internal/exec"
	"github.com/sonquer/opendba/src/tools/internal/render"
	"github.com/sonquer/opendba/src/tools/internal/workspace"
)

type fakeRunner struct {
	profile string
	fail    map[string]bool
	missing map[string]bool
}

func (f fakeRunner) Run(_ context.Context, _ string, name string, args ...string) (exec.Result, error) {
	if f.missing[name] {
		return exec.Result{}, errors.New("binary not found")
	}
	for _, arg := range args {
		if strings.HasPrefix(arg, "-coverprofile=") && f.profile != "" {
			if err := os.WriteFile(strings.TrimPrefix(arg, "-coverprofile="), []byte(f.profile), 0o600); err != nil {
				return exec.Result{}, err
			}
		}
	}
	result := exec.Result{Command: exec.Format(name, args...)}
	if f.fail[name] {
		result.ExitCode = 1
		result.Stdout = name + " failed"
	}
	return result, nil
}

type harness struct {
	app    App
	root   string
	stdout *bytes.Buffer
	stderr *bytes.Buffer
}

func (h harness) out() string { return ansi.Strip(h.stdout.String()) }

func (h harness) err() string { return ansi.Strip(h.stderr.String()) }

func newHarness(t *testing.T, runner exec.Runner) *harness {
	t.Helper()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.work"), "go 1.27.0\n\nuse (\n\t./src/cli\n\t./src/tools\n)\n")
	writeFile(t, filepath.Join(root, "src", "cli", "go.mod"), "module example.com/m/src/cli\n\ngo 1.27.0\n")
	writeFile(t, filepath.Join(root, "src", "cli", "a.go"), "package a\n\nfunc A() {}\n")
	writeFile(t, filepath.Join(root, "src", "tools", "go.mod"), "module example.com/m/src/tools\n\ngo 1.27.0\n")
	writeFile(t, filepath.Join(root, "src", "tools", "b.go"), "package b\n\nfunc B() {}\n")

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	return &harness{
		root:   root,
		stdout: stdout,
		stderr: stderr,
		app: App{
			Dir:    root,
			Runner: runner,
			Stdout: stdout,
			Stderr: stderr,
			Name:   "dev",
		},
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

const fullProfile = `mode: atomic
example.com/m/src/cli/a.go:3.10,3.20 1 1
example.com/m/src/tools/b.go:3.10,3.20 1 1
`

const thinProfile = `mode: atomic
example.com/m/src/cli/a.go:3.10,3.20 1 0
example.com/m/src/tools/b.go:3.10,3.20 1 1
`

func TestCheckCommandPasses(t *testing.T) {
	h := newHarness(t, fakeRunner{profile: fullProfile, missing: map[string]bool{"golangci-lint": true}})
	code := h.app.Run(context.Background(), []string{"check", "--coverage-dir", filepath.Join(t.TempDir(), "cov")})
	if code != ExitOK {
		t.Fatalf("exit = %d\n%s\n%s", code, h.out(), h.err())
	}
	out := h.out()
	for _, want := range []string{"comments", "format:cli", "build:tools", "cover:cli", "lint:cli", "all checks passed"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestCheckCommandFailsOnCoverageGate(t *testing.T) {
	h := newHarness(t, fakeRunner{profile: thinProfile, missing: map[string]bool{"golangci-lint": true}})
	code := h.app.Run(context.Background(), []string{"check", "--coverage-dir", filepath.Join(t.TempDir(), "cov")})
	if code != ExitFailure {
		t.Fatalf("exit = %d, want failure\n%s", code, h.out())
	}
	if !strings.Contains(h.out(), "below the 95.0% gate") {
		t.Errorf("missing gate message:\n%s", h.out())
	}
}

func TestCoverCommandWritesOneReportInTheRepositoryRoot(t *testing.T) {
	h := newHarness(t, fakeRunner{profile: fullProfile})
	dir := filepath.Join(t.TempDir(), "cov")
	code := h.app.Run(context.Background(), []string{"cover", "--coverage-dir", dir})
	if code != ExitOK {
		t.Fatalf("exit = %d\n%s", code, h.out())
	}
	report := filepath.Join(h.root, "coverage.html")
	data, err := os.ReadFile(report)
	if err != nil {
		t.Fatalf("coverage report missing: %v", err)
	}
	html := string(data)
	for _, want := range []string{"<!doctype html>", "example.com/m/src/cli", "example.com/m/src/tools", "cli, tools"} {
		if !strings.Contains(html, want) {
			t.Errorf("report missing %q", want)
		}
	}
	if !strings.Contains(h.out(), "coverage report: "+report) {
		t.Errorf("the report path must be printed:\n%s", h.out())
	}
	if strings.Contains(h.out(), "format:") {
		t.Error("cover must not run formatting checks")
	}
}

func TestCoverCommandCanSkipHTML(t *testing.T) {
	h := newHarness(t, fakeRunner{profile: fullProfile})
	dir := filepath.Join(t.TempDir(), "cov")
	if code := h.app.Run(context.Background(), []string{"cover", "--html=false", "--coverage-dir", dir}); code != ExitOK {
		t.Fatalf("exit = %d", code)
	}
	if _, err := os.Stat(filepath.Join(h.root, "coverage.html")); err == nil {
		t.Error("the html report must not be written")
	}
}

func TestCoverCommandRespectsCustomGate(t *testing.T) {
	h := newHarness(t, fakeRunner{profile: thinProfile})
	code := h.app.Run(context.Background(), []string{"cover", "--min", "40", "--html=false", "--coverage-dir", filepath.Join(t.TempDir(), "cov")})
	if code != ExitOK {
		t.Fatalf("exit = %d, want pass at a 40%% gate\n%s", code, h.out())
	}
}

func TestModuleSelection(t *testing.T) {
	h := newHarness(t, fakeRunner{profile: fullProfile})
	code := h.app.Run(context.Background(), []string{"build", "--module", "cli"})
	if code != ExitOK {
		t.Fatalf("exit = %d\n%s", code, h.out())
	}
	out := h.out()
	if !strings.Contains(out, "build:cli") || strings.Contains(out, "build:tools") {
		t.Errorf("module filter ignored:\n%s", out)
	}
}

func TestUnknownModuleIsUsageError(t *testing.T) {
	h := newHarness(t, fakeRunner{})
	if code := h.app.Run(context.Background(), []string{"build", "--module", "nope"}); code != ExitUsage {
		t.Fatalf("exit = %d, want usage error", code)
	}
	if !strings.Contains(h.err(), "unknown module") {
		t.Errorf("stderr = %q", h.err())
	}
}

func TestCommentsCommandFailsOnComment(t *testing.T) {
	h := newHarness(t, fakeRunner{})
	writeFile(t, filepath.Join(h.root, "src", "cli", "commented.go"), "package a\n\nfunc C() {\n\t// nope\n}\n")
	if code := h.app.Run(context.Background(), []string{"comments"}); code != ExitFailure {
		t.Fatalf("exit = %d, want failure", code)
	}
	if !strings.Contains(h.out(), "commented.go:4") {
		t.Errorf("output = %s", h.out())
	}
}

func TestFormatFailureIsReported(t *testing.T) {
	h := newHarness(t, fakeRunner{fail: map[string]bool{"go": true}})
	if code := h.app.Run(context.Background(), []string{"format"}); code != ExitFailure {
		t.Fatalf("exit = %d, want failure", code)
	}
	if !strings.Contains(h.out(), "needs formatting") {
		t.Errorf("output = %s", h.out())
	}
}

func TestCheckReportsRunnerErrors(t *testing.T) {
	h := newHarness(t, fakeRunner{missing: map[string]bool{"go": true}})
	if code := h.app.Run(context.Background(), []string{"build"}); code != ExitFailure {
		t.Fatalf("exit = %d, want failure", code)
	}
	if !strings.Contains(h.out(), "binary not found") {
		t.Errorf("output = %s", h.out())
	}
}

func TestInteractiveLaunch(t *testing.T) {
	h := newHarness(t, fakeRunner{profile: fullProfile})
	var launched core.Suite
	var title string
	h.app.Launch = func(suite core.Suite, _ render.Theme, t string) ([]core.Report, error) {
		launched, title = suite, t
		return []core.Report{{Check: "x", Status: core.StatusPass}}, nil
	}
	if code := h.app.Run(context.Background(), nil); code != ExitOK {
		t.Fatalf("exit = %d", code)
	}
	if len(launched) == 0 {
		t.Fatal("suite was not handed to the launcher")
	}
	if !strings.HasPrefix(title, "opendba dev · ") {
		t.Errorf("title = %q", title)
	}
}

func TestInteractiveLaunchFailure(t *testing.T) {
	h := newHarness(t, fakeRunner{})
	h.app.Launch = func(core.Suite, render.Theme, string) ([]core.Report, error) {
		return nil, errors.New("no terminal")
	}
	if code := h.app.Run(context.Background(), nil); code != ExitFailure {
		t.Fatalf("exit = %d, want failure", code)
	}
	if !strings.Contains(h.err(), "no terminal") {
		t.Errorf("stderr = %q", h.err())
	}
}

func TestInteractiveLaunchReportsFailure(t *testing.T) {
	h := newHarness(t, fakeRunner{})
	h.app.Launch = func(core.Suite, render.Theme, string) ([]core.Report, error) {
		return []core.Report{{Check: "x", Status: core.StatusFail}}, nil
	}
	if code := h.app.Run(context.Background(), nil); code != ExitFailure {
		t.Fatalf("exit = %d, want failure", code)
	}
}

func TestCIFlagSkipsLauncher(t *testing.T) {
	h := newHarness(t, fakeRunner{profile: fullProfile, missing: map[string]bool{"golangci-lint": true}})
	h.app.Launch = func(core.Suite, render.Theme, string) ([]core.Report, error) {
		t.Fatal("launcher must not run with --ci")
		return nil, nil
	}
	if code := h.app.Run(context.Background(), []string{"--ci", "--coverage-dir", filepath.Join(t.TempDir(), "cov")}); code != ExitOK {
		t.Fatalf("exit = %d\n%s", code, h.out())
	}
}

func TestDefaultCoverageDirectoryIsInsideWorkspace(t *testing.T) {
	h := newHarness(t, fakeRunner{profile: fullProfile})
	if code := h.app.Run(context.Background(), []string{"cover", "--html=false"}); code != ExitOK {
		t.Fatalf("exit = %d", code)
	}
	if _, err := os.Stat(filepath.Join(h.root, "coverage", "cli.out")); err != nil {
		t.Errorf("coverage profile not written to the workspace: %v", err)
	}
}

func TestHelpAndUsageErrors(t *testing.T) {
	h := newHarness(t, fakeRunner{})
	if code := h.app.Run(context.Background(), []string{"help"}); code != ExitOK {
		t.Fatalf("help exit = %d", code)
	}
	if !strings.Contains(h.out(), "opendba development tooling") {
		t.Errorf("usage missing:\n%s", h.out())
	}

	h = newHarness(t, fakeRunner{})
	if code := h.app.Run(context.Background(), []string{"nonsense"}); code != ExitUsage {
		t.Fatalf("unknown command exit = %d", code)
	}
	if !strings.Contains(h.err(), `unknown command "nonsense"`) {
		t.Errorf("stderr = %q", h.err())
	}

	h = newHarness(t, fakeRunner{})
	if code := h.app.Run(context.Background(), []string{"cover", "--nope"}); code != ExitUsage {
		t.Fatalf("bad flag exit = %d", code)
	}

	h = newHarness(t, fakeRunner{})
	if code := h.app.Run(context.Background(), []string{"cover", "-h"}); code != ExitOK {
		t.Fatalf("flag help exit = %d", code)
	}
}

func TestMissingWorkspaceIsFailure(t *testing.T) {
	h := newHarness(t, fakeRunner{})
	h.app.Dir = t.TempDir()
	if code := h.app.Run(context.Background(), []string{"check"}); code != ExitFailure {
		t.Fatalf("exit = %d, want failure", code)
	}
	if !strings.Contains(h.err(), "no go.work") {
		t.Errorf("stderr = %q", h.err())
	}
}

func TestDefaultAppNameIsDev(t *testing.T) {
	if (App{}).name() != "dev" {
		t.Error("default name must be dev")
	}
}

func TestMainRejectsUnknownCommand(t *testing.T) {
	if code := Main("dev", []string{"nonsense"}); code != ExitUsage {
		t.Fatalf("Main exit = %d", code)
	}
}

func TestVersionCommand(t *testing.T) {
	h := newHarness(t, fakeRunner{})
	writeFile(t, filepath.Join(h.root, "VERSION"), "1.2.3\n")
	if code := h.app.Run(context.Background(), []string{"version"}); code != ExitOK {
		t.Fatalf("exit = %d", code)
	}
	if strings.TrimSpace(h.out()) != "1.2.3" {
		t.Errorf("output = %q", h.out())
	}
}

func TestVersionCommandFailures(t *testing.T) {
	h := newHarness(t, fakeRunner{})
	if code := h.app.Run(context.Background(), []string{"version"}); code != ExitFailure {
		t.Fatalf("a missing VERSION file must fail, got %d", code)
	}

	outside := newHarness(t, fakeRunner{})
	outside.app.Dir = t.TempDir()
	if code := outside.app.Run(context.Background(), []string{"version"}); code != ExitFailure {
		t.Fatalf("no workspace must fail, got %d", code)
	}
}

func TestSummaryIsAppendedForCI(t *testing.T) {
	h := newHarness(t, fakeRunner{profile: fullProfile})
	summary := filepath.Join(t.TempDir(), "summary.md")
	code := h.app.Run(context.Background(), []string{"cover", "--html=false", "--summary", summary, "--coverage-dir", filepath.Join(t.TempDir(), "cov")})
	if code != ExitOK {
		t.Fatalf("exit = %d\n%s", code, h.out())
	}
	data, err := os.ReadFile(summary)
	if err != nil {
		t.Fatalf("summary missing: %v", err)
	}
	for _, want := range []string{"## opendba coverage", "| File |", "cli/", "tools/"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("summary missing %q:\n%s", want, data)
		}
	}
}

func TestSummaryFailureIsReported(t *testing.T) {
	h := newHarness(t, fakeRunner{profile: fullProfile})
	blocked := filepath.Join(t.TempDir(), "dir")
	if err := os.Mkdir(blocked, 0o755); err != nil {
		t.Fatal(err)
	}
	code := h.app.Run(context.Background(), []string{"cover", "--html=false", "--summary", blocked, "--coverage-dir", filepath.Join(t.TempDir(), "cov")})
	if code != ExitOK {
		t.Fatalf("a summary that cannot be written must not fail the run, got %d", code)
	}
	if h.err() == "" {
		t.Error("the failure must be reported on stderr")
	}
}

func TestCoverageReportFailureIsReported(t *testing.T) {
	h := newHarness(t, fakeRunner{profile: fullProfile})
	if err := os.Mkdir(filepath.Join(h.root, "coverage.html"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(h.root, "coverage.html", "keep"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	code := h.app.Run(context.Background(), []string{"cover", "--coverage-dir", filepath.Join(t.TempDir(), "cov")})
	if code != ExitOK {
		t.Fatalf("a report that cannot be written must not fail the run, got %d", code)
	}
	if h.err() == "" {
		t.Error("the failure must be reported on stderr")
	}
}

func TestCoverageReportStampsTheTime(t *testing.T) {
	h := newHarness(t, fakeRunner{profile: fullProfile})
	h.app.Now = func() time.Time { return time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC) }
	if code := h.app.Run(context.Background(), []string{"cover", "--coverage-dir", filepath.Join(t.TempDir(), "cov")}); code != ExitOK {
		t.Fatalf("exit = %d", code)
	}
	data, err := os.ReadFile(filepath.Join(h.root, "coverage.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "2026-08-21T12:00:00Z") {
		t.Error("the report must carry the time it was generated")
	}
}

func TestDefaultClockIsUsed(t *testing.T) {
	if (App{}).now().IsZero() {
		t.Error("the default clock must return a real time")
	}
}

func TestShortenerLeavesForeignPathsAlone(t *testing.T) {
	space := workspace.Workspace{Modules: []workspace.Module{{Name: "cli", Path: "example.com/m/src/cli", Dir: "/tmp"}}}
	shorten := shortener(space)
	if got := shorten("example.com/m/src/cli/internal/ui"); got != "cli/internal/ui" {
		t.Errorf("shorten() = %q", got)
	}
	if got := shorten("example.com/m/src/cli"); got != "cli/." {
		t.Errorf("shorten() = %q", got)
	}
	if got := shorten("other.com/x"); got != "other.com/x" {
		t.Errorf("shorten() = %q", got)
	}
}

func TestPolicyFileIsUsed(t *testing.T) {
	h := newHarness(t, fakeRunner{profile: thinProfile})
	writeFile(t, filepath.Join(h.root, "dev.toml"), "[coverage]\ntotal = 40\n")
	code := h.app.Run(context.Background(), []string{"cover", "--html=false", "--coverage-dir", filepath.Join(t.TempDir(), "cov")})
	if code != ExitOK {
		t.Fatalf("the gate from dev.toml must be used, got exit %d\n%s", code, h.out())
	}
}

func TestBrokenPolicyFileIsAFailure(t *testing.T) {
	h := newHarness(t, fakeRunner{})
	writeFile(t, filepath.Join(h.root, "dev.toml"), "[coverage]\ntotal = 150\n")
	if code := h.app.Run(context.Background(), []string{"cover"}); code != ExitFailure {
		t.Fatalf("exit = %d, want failure", code)
	}
	if !strings.Contains(h.err(), "between 0 and 100") {
		t.Errorf("stderr = %q", h.err())
	}
}

func TestMinFlagOverridesThePolicy(t *testing.T) {
	h := newHarness(t, fakeRunner{profile: thinProfile})
	writeFile(t, filepath.Join(h.root, "dev.toml"), "[coverage]\ntotal = 95\n\n[coverage.modules]\ncli = 99\n")
	code := h.app.Run(context.Background(), []string{"cover", "--min", "10", "--html=false", "--coverage-dir", filepath.Join(t.TempDir(), "cov")})
	if code != ExitOK {
		t.Fatalf("--min must override every module gate, got exit %d\n%s", code, h.out())
	}
}

func TestRunProductPassesTheEnvironmentThrough(t *testing.T) {
	h := newHarness(t, fakeRunner{})
	writeFile(t, filepath.Join(h.root, ".env"), "XDG_CONFIG_HOME=.local/config\n")
	writeFile(t, filepath.Join(h.root, "src", "cli", "cmd", "opendba", "main.go"),
		"package main\n\nimport \"fmt\"\n\nfunc main() { fmt.Println(\"started\") }\n")

	code := h.app.Run(context.Background(), []string{"run", "version"})
	if code != 0 && code != 1 {
		t.Fatalf("exit = %d\n%s", code, h.err())
	}
	if !strings.Contains(h.err(), "using .env") {
		t.Errorf("the values in use must be shown: %q", h.err())
	}
	if !strings.Contains(h.err(), filepath.Join(h.root, ".local/config")) {
		t.Errorf("relative paths must be resolved against the root: %q", h.err())
	}
}

func TestRunProductReportsABrokenEnvFile(t *testing.T) {
	h := newHarness(t, fakeRunner{})
	writeFile(t, filepath.Join(h.root, ".env"), "NOT AN ASSIGNMENT\n")
	if code := h.app.Run(context.Background(), []string{"run"}); code != ExitFailure {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(h.err(), ".env") {
		t.Errorf("stderr = %q", h.err())
	}
}

func TestRunProductNeedsAWorkspace(t *testing.T) {
	h := newHarness(t, fakeRunner{})
	h.app.Dir = t.TempDir()
	if code := h.app.Run(context.Background(), []string{"run"}); code != ExitFailure {
		t.Fatalf("exit = %d", code)
	}
}

func TestBuilderPointsAtTheToolsModule(t *testing.T) {
	h := newHarness(t, fakeRunner{})
	space, err := workspace.Discover(h.root)
	if err != nil {
		t.Fatal(err)
	}
	builder := h.app.builder(space)
	if !strings.HasSuffix(builder.ToolsDir, filepath.Join("src", "tools")) {
		t.Errorf("tools directory = %q", builder.ToolsDir)
	}
	if !strings.HasPrefix(builder.BinDir, h.root) {
		t.Errorf("the tools must land inside the repository: %q", builder.BinDir)
	}

	single := workspace.Workspace{Root: h.root, Modules: []workspace.Module{{Name: "cli", Dir: h.root}}}
	if h.app.builder(single).ToolsDir != h.root {
		t.Error("a workspace without a tools module must fall back to the last one")
	}
}
