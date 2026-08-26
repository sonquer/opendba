package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sonquer/opendba/src/tools/internal/checks"
	"github.com/sonquer/opendba/src/tools/internal/core"
	"github.com/sonquer/opendba/src/tools/internal/envfile"
	"github.com/sonquer/opendba/src/tools/internal/exec"
	"github.com/sonquer/opendba/src/tools/internal/policy"
	"github.com/sonquer/opendba/src/tools/internal/render"
	"github.com/sonquer/opendba/src/tools/internal/toolbin"
	"github.com/sonquer/opendba/src/tools/internal/tui"
	"github.com/sonquer/opendba/src/tools/internal/workspace"
	"github.com/sonquer/opendba/src/tools/pkg/cover"
)

const (
	ExitOK      = 0
	ExitFailure = 1
	ExitUsage   = 2
)

const DefaultMinCoverage = 95

type Launcher func(suite core.Suite, theme render.Theme, title string) ([]core.Report, error)

type App struct {
	Dir    string
	Runner exec.Runner
	Stdout io.Writer
	Stderr io.Writer
	Launch Launcher
	Name   string
	Now    func() time.Time
}

type options struct {
	ci          bool
	html        bool
	min         float64
	minSet      bool
	coverageDir string
	module      string
	summary     string
	out         string
	only        string
	size        string
	update      bool
	jobs        int
	binary      string
	policy      policy.Policy
}

func (a App) Run(ctx context.Context, args []string) int {
	command, rest := split(args)
	if command == "help" {
		a.usage()
		return ExitOK
	}
	if command == "version" {
		return a.version(ctx, rest)
	}
	if command == "run" {
		return a.runProduct(rest)
	}
	if command == "e2e" {
		opts, err := a.parse(command, rest)
		if err != nil {
			if err == flag.ErrHelp {
				return ExitOK
			}
			fmt.Fprintln(a.Stderr, err)
			return ExitUsage
		}
		return a.e2e(ctx, opts)
	}
	if !known(command) {
		fmt.Fprintf(a.Stderr, "unknown command %q\n\n", command)
		a.usage()
		return ExitUsage
	}

	opts, err := a.parse(command, rest)
	if err != nil {
		if err == flag.ErrHelp {
			return ExitOK
		}
		fmt.Fprintln(a.Stderr, err)
		return ExitUsage
	}

	space, err := workspace.Discover(a.Dir)
	if err != nil {
		fmt.Fprintln(a.Stderr, err)
		return ExitFailure
	}

	loaded, err := policy.Load(space.Root)
	if err != nil {
		fmt.Fprintln(a.Stderr, err)
		return ExitFailure
	}
	opts.policy = loaded
	if opts.minSet {
		opts.policy = loaded.WithTotal(opts.min)
	}
	opts.min = opts.policy.Coverage.Total

	suite, err := a.suite(space, command, opts)
	if err != nil {
		fmt.Fprintln(a.Stderr, err)
		return ExitUsage
	}

	if a.interactive(command, opts) {
		reports, err := a.Launch(suite, render.DefaultTheme(), a.title(space))
		if err != nil {
			fmt.Fprintln(a.Stderr, err)
			return ExitFailure
		}
		a.report(space, suite, opts)
		return verdict(reports)
	}
	code := a.headless(ctx, suite)
	a.report(space, suite, opts)
	return code
}

func (a App) runProduct(args []string) int {
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
	if len(values) > 0 {
		fmt.Fprintln(a.Stderr, "using "+envfile.FileName+": "+values.Describe())
	}
	command := append([]string{"run", "./src/cli/cmd/opendba"}, args...)
	code, err := exec.Passthrough(context.Background(), space.Root, values.Environment(os.Environ()), "go", command...)
	if err != nil {
		fmt.Fprintln(a.Stderr, err)
		return ExitFailure
	}
	return code
}

func (a App) interactive(command string, opts options) bool {
	return command == "dev" && !opts.ci && a.Launch != nil
}

func (a App) headless(ctx context.Context, suite core.Suite) int {
	theme := render.DefaultTheme()
	reports := make([]core.Report, 0, len(suite))
	for _, check := range suite {
		report, err := check.Run(ctx)
		if err != nil {
			report = core.Report{Check: check.Name(), Status: core.StatusFail, Summary: err.Error()}
		}
		reports = append(reports, report)
	}
	fmt.Fprint(a.Stdout, theme.Reports(reports))
	return verdict(reports)
}

func (a App) report(space workspace.Workspace, suite core.Suite, opts options) {
	var (
		summaries []cover.Summary
		sections  []string
	)
	for _, module := range space.Modules {
		if _, ok := suite.Find("cover:" + module.Name); !ok {
			continue
		}
		parsed, err := cover.ParseFile(checks.ProfilePath(a.coverageDir(space, opts), module))
		if err != nil {
			continue
		}
		filtered := parsed.Without(checks.GeneratedFilter(space, parsed))
		summaries = append(summaries, filtered.Without(exempt(opts.policy, module)))
		sections = append(sections, module.Name)
	}
	if len(summaries) == 0 {
		return
	}
	merged := cover.Merge(summaries...)
	rows := cover.Rows(merged, space.Resolve, shortener(space))
	fmt.Fprint(a.Stdout, render.DefaultTheme().CoverageTable(rows, opts.min))
	a.writeSummary(rows, opts)

	if !opts.html {
		return
	}
	path := filepath.Join(space.Root, "coverage.html")
	err := cover.WriteReportFile(path, merged, cover.ReportOptions{
		Title:     "opendba",
		Min:       opts.min,
		Resolve:   space.Resolve,
		Sections:  sections,
		Generated: a.now().Format(time.RFC3339),
	})
	if err != nil {
		fmt.Fprintln(a.Stderr, err)
		return
	}
	fmt.Fprintln(a.Stdout, "coverage report: "+path)
}

func (a App) writeSummary(rows []cover.TableRow, opts options) {
	if opts.summary == "" {
		return
	}
	file, err := os.OpenFile(opts.summary, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		fmt.Fprintln(a.Stderr, err)
		return
	}
	defer file.Close()
	if err := cover.WriteMarkdown(file, rows, opts.min, "opendba"); err != nil {
		fmt.Fprintln(a.Stderr, err)
	}
}

func exempt(rules policy.Policy, module workspace.Module) func(string) bool {
	return func(importPath string) bool {
		short := cover.ShortPath(importPath, module.Path)
		if short == "." {
			short = module.Name
		} else {
			short = module.Name + "/" + short
		}
		return rules.Exempt(short)
	}
}

func shortener(space workspace.Workspace) func(string) string {
	return func(importPath string) string {
		for _, module := range space.Modules {
			if short := cover.ShortPath(importPath, module.Path); short != importPath {
				return module.Name + "/" + strings.TrimPrefix(short, "./")
			}
		}
		return importPath
	}
}

func (a App) now() time.Time {
	if a.Now == nil {
		return time.Now()
	}
	return a.Now()
}

func (a App) suite(space workspace.Workspace, command string, opts options) (core.Suite, error) {
	settings := checks.Options{
		Workspace:   space,
		Runner:      a.Runner,
		Policy:      opts.policy,
		CoverageDir: a.coverageDir(space, opts),
		Builder:     a.builder(space),
		Race:        command == "race",
	}
	full := checks.Suite(settings)
	filtered, err := filter(full, prefixFor(command), opts.module, space)
	if err != nil {
		return nil, err
	}
	return filtered, nil
}

func (a App) builder(space workspace.Workspace) toolbin.Builder {
	tools, ok := space.Find("tools")
	if !ok {
		tools = space.Modules[len(space.Modules)-1]
	}
	return toolbin.Builder{
		Runner:   a.Runner,
		ToolsDir: tools.Dir,
		BinDir:   filepath.Join(space.Root, toolbin.DirName),
	}
}

func (a App) coverageDir(space workspace.Workspace, opts options) string {
	if opts.coverageDir != "" {
		return opts.coverageDir
	}
	return filepath.Join(space.Root, "coverage")
}

func (a App) title(space workspace.Workspace) string {
	return fmt.Sprintf("opendba dev · %s", filepath.Base(space.Root))
}

func (a App) parse(command string, args []string) (options, error) {
	opts := options{min: DefaultMinCoverage, html: true}
	set := flag.NewFlagSet(a.name()+" "+command, flag.ContinueOnError)
	set.SetOutput(a.Stderr)
	set.BoolVar(&opts.ci, "ci", false, "plain output, no interactive interface")
	set.BoolVar(&opts.html, "html", true, "write the HTML coverage report")
	set.Float64Var(&opts.min, "min", DefaultMinCoverage, "override the total coverage gate from dev.toml")
	set.StringVar(&opts.coverageDir, "coverage-dir", "", "directory for coverage output")
	set.StringVar(&opts.module, "module", "", "limit the run to a single module")
	set.StringVar(&opts.summary, "summary", "", "append a markdown coverage summary to this file")
	set.StringVar(&opts.out, "out", "", "directory to write the screens into")
	set.StringVar(&opts.only, "only", "", "run a single scenario")
	set.StringVar(&opts.size, "size", "", "run at a single terminal size")
	set.BoolVar(&opts.update, "update", false, "keep the screens that were drawn as the ones to compare against")
	set.IntVar(&opts.jobs, "jobs", 0, "how many scenarios run at once")
	set.StringVar(&opts.binary, "binary", "", "run against a program built with the pinned key style")
	if err := set.Parse(args); err != nil {
		return options{}, err
	}
	set.Visit(func(f *flag.Flag) {
		if f.Name == "min" {
			opts.minSet = true
		}
	})
	return opts, nil
}

func (a App) name() string {
	if a.Name == "" {
		return "dev"
	}
	return a.Name
}

func (a App) usage() {
	fmt.Fprintf(a.Stdout, `%s, opendba development tooling

usage: %s <command> [flags]

commands:
  dev        run every check (interactive unless --ci)
  check      run every check without the interactive interface
  cover      run tests and enforce the coverage gate
  comments   fail on any comment in Go source
  format     verify formatting
  build      compile every module
  lint       run golangci-lint, built from the version pinned in src/tools
  vuln       run govulncheck, built the same way
  workflows  run actionlint over the GitHub Actions workflows
  race       run the tests under the race detector, which needs cgo
  run        start opendba with the values from .env, passing the rest through
  e2e        walk every screen of the interface through a real terminal
  version    read or rewrite the VERSION file
  help       show this message

flags:
  --ci                 plain output, no interactive interface
  --min <percent>      coverage gate, default %.0f
  --html               write coverage.html in the repository root, default true
  --coverage-dir <dir> coverage output directory, default <root>/coverage
  --module <name>      limit the run to a single module
  --summary <file>     append a markdown coverage summary, for CI job summaries

e2e flags:
  --out <dir>          where a failed screen is written, default <root>/.e2e
  --only <scenario>    run a single scenario
  --size <WxH>         run at a single terminal size
  --update             keep the screens that were drawn as the ones to compare against
  --jobs <n>           how many scenarios run at once
  --binary <path>      run against a program built with the pinned key style

version flags:
  --tag                print v<version> instead of the bare version
  --module-tag         print src/cli/v<version>, the tag go install resolves
  --snapshot           print what an unreleased build of this commit calls itself
  --exact              leave the patch alone when building a snapshot version
  --commit <sha>       the commit a snapshot names, default the one checked out
  --check <tag>        fail unless the tag matches the VERSION file
  --set <bump>         rewrite VERSION: patch, minor, major, or X.Y.Z
  --github <file>      append version, tag, module_tag and branch to this file
`, a.name(), a.name(), float64(DefaultMinCoverage))
}

func split(args []string) (string, []string) {
	if len(args) == 0 {
		return "dev", nil
	}
	if strings.HasPrefix(args[0], "-") {
		return "dev", args
	}
	return args[0], args[1:]
}

var commands = map[string]string{
	"dev":       "",
	"check":     "",
	"cover":     "cover",
	"comments":  "comments",
	"format":    "format",
	"build":     "build",
	"lint":      "lint",
	"vuln":      "vuln",
	"race":      "race",
	"workflows": "workflows",
	"version":   "",
	"run":       "",
	"e2e":       "",
}

func known(command string) bool {
	_, ok := commands[command]
	return ok
}

func prefixFor(command string) string { return commands[command] }

func filter(suite core.Suite, prefix, module string, space workspace.Workspace) (core.Suite, error) {
	if module != "" {
		if _, ok := space.Find(module); !ok {
			return nil, fmt.Errorf("unknown module %q, have %s", module, strings.Join(space.Names(), ", "))
		}
	}
	var selected core.Suite
	for _, check := range suite {
		name := check.Name()
		if prefix != "" && !strings.HasPrefix(name, prefix) {
			continue
		}
		if module != "" && strings.Contains(name, ":") && !strings.HasSuffix(name, ":"+module) {
			continue
		}
		selected = append(selected, check)
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("no checks match the selection")
	}
	return selected, nil
}

func verdict(reports []core.Report) int {
	if core.Aggregate(reports) == core.StatusFail {
		return ExitFailure
	}
	return ExitOK
}

func Main(name string, args []string) int {
	dir, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return ExitFailure
	}
	app := App{
		Dir:    dir,
		Runner: exec.OS{},
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		Launch: launch,
		Name:   name,
	}
	return app.Run(context.Background(), args)
}

func launch(suite core.Suite, theme render.Theme, title string) ([]core.Report, error) {
	return tui.Run(suite, theme, title)
}
