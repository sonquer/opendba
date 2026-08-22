package checks

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sonquer/tui4db/src/tools/internal/core"
	"github.com/sonquer/tui4db/src/tools/internal/exec"
	"github.com/sonquer/tui4db/src/tools/internal/policy"
	"github.com/sonquer/tui4db/src/tools/internal/toolbin"
	"github.com/sonquer/tui4db/src/tools/internal/workspace"
	"github.com/sonquer/tui4db/src/tools/pkg/cover"
	"github.com/sonquer/tui4db/src/tools/pkg/gate"
)

type Options struct {
	Workspace   workspace.Workspace
	Runner      exec.Runner
	Policy      policy.Policy
	CoverageDir string
	Builder     toolbin.Builder
}

func ProfilePath(coverageDir string, module workspace.Module) string {
	return filepath.Join(coverageDir, module.Name+".out")
}

const generatedSegment = "/generated/"

func GeneratedFilter(space workspace.Workspace, summary cover.Summary) func(string) bool {
	generated := map[string]bool{}
	for _, file := range summary.Files {
		if strings.Contains(file.Path, generatedSegment) {
			generated[file.Package()] = true
			continue
		}
		path, ok := space.Resolve(file.Path)
		if !ok {
			continue
		}
		source, err := os.ReadFile(path)
		if err == nil && gate.IsGenerated(source) {
			generated[file.Package()] = true
		}
	}
	return func(importPath string) bool { return generated[importPath] }
}

func Suite(opts Options) core.Suite {
	suite := core.Suite{Comments(opts.Workspace.Root)}
	for _, module := range opts.Workspace.Modules {
		suite = append(suite, Format(module, opts.Runner), Build(module, opts.Runner))
	}
	for _, module := range opts.Workspace.Modules {
		suite = append(suite, Coverage(opts, module))
	}
	for _, module := range opts.Workspace.Modules {
		suite = append(suite, Lint(opts, module), Vulnerabilities(opts, module))
	}
	return suite
}

type commentCheck struct{ root string }

func Comments(root string) core.Check { return commentCheck{root: root} }

func (c commentCheck) Name() string { return "comments" }

func (c commentCheck) Describe() string { return "no comments in Go source" }

func (c commentCheck) Run(context.Context) (core.Report, error) {
	started := time.Now()
	findings, err := gate.Scan(c.root)
	if err != nil {
		return core.Report{}, err
	}
	report := core.Report{Check: c.Name(), Status: core.StatusPass, Duration: time.Since(started)}
	if len(findings) == 0 {
		report.Summary = "no comments found"
		return report, nil
	}
	report.Status = core.StatusFail
	report.Summary = fmt.Sprintf("%d comment(s) found", len(findings))
	for _, f := range findings {
		report.Detail = append(report.Detail, f.String())
	}
	return report, nil
}

type commandCheck struct {
	name     string
	describe string
	module   workspace.Module
	runner   exec.Runner
	args     []string
	failOn   func(exec.Result) bool
	summary  func(exec.Result) string
}

func (c commandCheck) Name() string { return c.name + ":" + c.module.Name }

func (c commandCheck) Describe() string { return c.describe }

func (c commandCheck) Run(ctx context.Context) (core.Report, error) {
	result, err := c.runner.Run(ctx, c.module.Dir, "go", c.args...)
	if err != nil {
		return core.Report{}, err
	}
	report := core.Report{
		Check:    c.Name(),
		Status:   core.StatusPass,
		Summary:  c.summary(result),
		Duration: result.Duration,
	}
	if c.failOn(result) {
		report.Status = core.StatusFail
		report.Detail = result.Lines()
	}
	return report, nil
}

func Format(module workspace.Module, runner exec.Runner) core.Check {
	return commandCheck{
		name:     "format",
		describe: "gofmt -s -l reports nothing",
		module:   module,
		runner:   runner,
		args:     []string{"fmt", "./..."},
		failOn:   func(r exec.Result) bool { return !r.OK() || r.Output() != "" },
		summary: func(r exec.Result) string {
			if r.OK() && r.Output() == "" {
				return "formatted"
			}
			return "needs formatting"
		},
	}
}

func Build(module workspace.Module, runner exec.Runner) core.Check {
	return commandCheck{
		name:     "build",
		describe: "module compiles",
		module:   module,
		runner:   runner,
		args:     []string{"build", "./..."},
		failOn:   func(r exec.Result) bool { return !r.OK() },
		summary: func(r exec.Result) string {
			if r.OK() {
				return "compiles"
			}
			return "build failed"
		},
	}
}

type toolCheck struct {
	name     string
	describe string
	tool     toolbin.Tool
	args     []string
	failed   string
	module   workspace.Module
	runner   exec.Runner
	builder  toolbin.Builder
}

func (c toolCheck) Name() string { return c.name + ":" + c.module.Name }

func (c toolCheck) Describe() string { return c.describe }

func (c toolCheck) Run(ctx context.Context) (core.Report, error) {
	binary, err := c.builder.Ensure(ctx, c.tool)
	if err != nil {
		return core.Report{
			Check:   c.Name(),
			Status:  core.StatusFail,
			Summary: "the tool could not be built",
			Detail:  []string{err.Error()},
		}, nil
	}
	result, err := c.runner.Run(ctx, c.module.Dir, binary, c.args...)
	if err != nil {
		return core.Report{}, fmt.Errorf("run %s: %w", c.tool.Name, err)
	}
	report := core.Report{Check: c.Name(), Status: core.StatusPass, Summary: "clean", Duration: result.Duration}
	if !result.OK() {
		report.Status = core.StatusFail
		report.Summary = c.failed
		report.Detail = result.Lines()
	}
	return report, nil
}

func Lint(opts Options, module workspace.Module) core.Check {
	return toolCheck{
		name:     "lint",
		describe: "golangci-lint run, at the version pinned in src/tools",
		tool:     toolbin.Lint,
		args:     []string{"run"},
		failed:   "lint findings",
		module:   module,
		runner:   opts.Runner,
		builder:  opts.Builder,
	}
}

func Vulnerabilities(opts Options, module workspace.Module) core.Check {
	return toolCheck{
		name:     "vuln",
		describe: "govulncheck reports no known vulnerabilities",
		tool:     toolbin.Vuln,
		args:     []string{"./..."},
		failed:   "known vulnerabilities",
		module:   module,
		runner:   opts.Runner,
		builder:  opts.Builder,
	}
}

type coverageCheck struct {
	module      workspace.Module
	space       workspace.Workspace
	runner      exec.Runner
	policy      policy.Policy
	min         float64
	coverageDir string
}

func Coverage(opts Options, module workspace.Module) core.Check {
	return coverageCheck{
		module:      module,
		space:       opts.Workspace,
		runner:      opts.Runner,
		policy:      opts.Policy,
		min:         opts.Policy.ModuleThreshold(module.Name),
		coverageDir: opts.CoverageDir,
	}
}

func (c coverageCheck) Name() string { return "cover:" + c.module.Name }

func (c coverageCheck) Describe() string {
	return fmt.Sprintf("tests pass and coverage is at least %.0f%%", c.min)
}

func (c coverageCheck) profilePath() string { return ProfilePath(c.coverageDir, c.module) }

func (c coverageCheck) Run(ctx context.Context) (core.Report, error) {
	profile := c.profilePath()
	if err := ensureDir(filepath.Dir(profile)); err != nil {
		return core.Report{}, err
	}
	result, err := c.runner.Run(ctx, c.module.Dir, "go",
		"test", "./...", "-covermode=atomic", "-coverpkg=./...", "-coverprofile="+profile)
	if err != nil {
		return core.Report{}, err
	}
	report := core.Report{Check: c.Name(), Status: core.StatusPass, Duration: result.Duration}
	if !result.OK() {
		report.Status = core.StatusFail
		report.Summary = "tests failed"
		report.Detail = result.Lines()
		return report, nil
	}

	parsed, err := cover.ParseFile(profile)
	if err != nil {
		return core.Report{}, err
	}
	summary := parsed.Without(GeneratedFilter(c.space, parsed)).Without(c.exempt)
	report.Summary = fmt.Sprintf("%.1f%% of %d statements", summary.Percent(), summary.Statements)
	if !summary.Meets(c.min) {
		report.Status = core.StatusFail
		report.Detail = append(report.Detail,
			fmt.Sprintf("total coverage %.1f%% is below the %.1f%% gate", summary.Percent(), c.min))
	}
	for _, failure := range c.packageFailures(summary) {
		report.Status = core.StatusFail
		report.Detail = append(report.Detail, failure)
	}
	return report, nil
}

func (c coverageCheck) shortPath(importPath string) string {
	short := cover.ShortPath(importPath, c.module.Path)
	if short == "." {
		return c.module.Name
	}
	return c.module.Name + "/" + short
}

func (c coverageCheck) exempt(importPath string) bool {
	return c.policy.Exempt(c.shortPath(importPath))
}

func (c coverageCheck) packageFailures(summary cover.Summary) []string {
	var failures []string
	for _, pkg := range summary.Packages {
		path := c.shortPath(pkg.ImportPath)
		threshold := c.policy.PackageThreshold(path)
		if threshold <= 0 || pkg.Statements == 0 || pkg.Percent() >= threshold {
			continue
		}
		failures = append(failures,
			fmt.Sprintf("%s is at %.1f%%, below its %.1f%% gate", path, pkg.Percent(), threshold))
	}
	return failures
}
