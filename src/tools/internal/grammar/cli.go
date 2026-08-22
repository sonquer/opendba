package grammar

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/sonquer/tui4db/src/tools/internal/exec"
	"github.com/sonquer/tui4db/src/tools/internal/workspace"
)

const (
	ExitOK      = 0
	ExitFailure = 1
	ExitUsage   = 2
)

type App struct {
	Dir     string
	Runner  exec.Runner
	Stdout  io.Writer
	Stderr  io.Writer
	Home    string
	Fetcher func(url string) JarFetcher
}

func (a App) Run(ctx context.Context, args []string) int {
	set := flag.NewFlagSet("grammar", flag.ContinueOnError)
	set.SetOutput(a.Stderr)
	version := set.String("antlr", DefaultVersion, "antlr version to generate with")
	module := set.String("module", "cli", "workspace module holding the generated parsers")
	dialect := set.String("dialect", "", "regenerate a single dialect")
	if err := set.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return ExitOK
		}
		return ExitUsage
	}

	space, err := workspace.Discover(a.Dir)
	if err != nil {
		fmt.Fprintln(a.Stderr, err)
		return ExitFailure
	}
	target, ok := space.Find(*module)
	if !ok {
		fmt.Fprintf(a.Stderr, "unknown module %q\n", *module)
		return ExitUsage
	}

	jar := filepath.Join(CacheDir(a.home()), JarName(*version))
	if err := a.fetcher(JarURL(*version)).Ensure(ctx, jar); err != nil {
		fmt.Fprintln(a.Stderr, err)
		return ExitFailure
	}

	generator := Generator{Runner: a.Runner, Jar: jar}
	for _, spec := range Specs(target.Dir) {
		if *dialect != "" && spec.Dialect != *dialect {
			continue
		}
		written, err := generator.Generate(ctx, spec)
		if err != nil {
			fmt.Fprintln(a.Stderr, err)
			return ExitFailure
		}
		fmt.Fprintf(a.Stdout, "%s: %d files generated in %s\n", spec.Dialect, len(written), spec.Dir)
	}
	return ExitOK
}

func (a App) home() string {
	if a.Home != "" {
		return a.Home
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return home
}

func (a App) fetcher(url string) JarFetcher {
	if a.Fetcher != nil {
		return a.Fetcher(url)
	}
	return JarFetcher{Client: http.DefaultClient, URL: url}
}

func Main(args []string) int {
	dir, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return ExitFailure
	}
	app := App{Dir: dir, Runner: exec.OS{}, Stdout: os.Stdout, Stderr: os.Stderr}
	return app.Run(context.Background(), args)
}
