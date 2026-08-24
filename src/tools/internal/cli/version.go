package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sonquer/opendba/src/tools/internal/workspace"
	"github.com/sonquer/opendba/src/tools/pkg/semver"
)

const FileName = "VERSION"

const shortCommit = 7

type versionOptions struct {
	tag       bool
	moduleTag bool
	snapshot  bool
	exact     bool
	commit    string
	check     string
	set       string
	github    string
}

func (a App) version(ctx context.Context, args []string) int {
	opts, err := a.parseVersion(args)
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
	path := filepath.Join(space.Root, FileName)
	current, err := readVersion(path)
	if err != nil {
		fmt.Fprintln(a.Stderr, err)
		return ExitFailure
	}
	if opts.check != "" {
		return a.check(current, opts.check)
	}
	value, err := a.resolve(ctx, space.Root, path, current, opts)
	if err != nil {
		fmt.Fprintln(a.Stderr, err)
		return ExitFailure
	}
	if opts.github != "" {
		if err := appendOutputs(opts.github, value); err != nil {
			fmt.Fprintln(a.Stderr, err)
			return ExitFailure
		}
	}
	fmt.Fprintln(a.Stdout, name(value, opts))
	return ExitOK
}

func (a App) resolve(ctx context.Context, root, path string, current semver.Version, opts versionOptions) (semver.Version, error) {
	value := current
	if opts.set != "" {
		next, err := bumped(current, opts.set)
		if err != nil {
			return semver.Version{}, err
		}
		if err := writeVersion(path, next); err != nil {
			return semver.Version{}, err
		}
		value = next
	}
	if !opts.snapshot {
		return value, nil
	}
	commit, err := a.commitOf(ctx, root, opts.commit)
	if err != nil {
		return semver.Version{}, err
	}
	day := a.now().UTC().Format("20060102")
	if opts.exact {
		value.Pre = "nightly." + day + "." + commit
		return value, nil
	}
	return value.Snapshot(day, commit), nil
}

func (a App) check(current semver.Version, tag string) int {
	if strings.TrimSpace(tag) == current.Tag() {
		fmt.Fprintln(a.Stdout, current.Tag()+" matches "+FileName)
		return ExitOK
	}
	fmt.Fprintf(a.Stderr, "tag %s does not match %s (%s)\n", tag, FileName, current)
	return ExitFailure
}

func (a App) commitOf(ctx context.Context, root, explicit string) (string, error) {
	if explicit != "" {
		return abbreviate(explicit), nil
	}
	if sha := os.Getenv("GITHUB_SHA"); sha != "" {
		return abbreviate(sha), nil
	}
	result, err := a.Runner.Run(ctx, root, "git", "rev-parse", "--short="+fmt.Sprint(shortCommit), "HEAD")
	if err != nil {
		return "", fmt.Errorf("read the current commit: %w", err)
	}
	sha := abbreviate(result.Output())
	if !result.OK() || sha == "" {
		return "", fmt.Errorf("read the current commit: git reported nothing")
	}
	return sha, nil
}

func abbreviate(sha string) string {
	trimmed := strings.TrimSpace(sha)
	if len(trimmed) > shortCommit {
		return trimmed[:shortCommit]
	}
	return trimmed
}

func name(value semver.Version, opts versionOptions) string {
	if opts.moduleTag {
		return value.ModuleTag()
	}
	if opts.tag {
		return value.Tag()
	}
	return value.String()
}

func bumped(current semver.Version, request string) (semver.Version, error) {
	kind, exact, err := semver.ParseKind(request)
	if err != nil {
		return semver.Version{}, err
	}
	if kind == semver.Exact {
		return exact, nil
	}
	return current.Bump(kind), nil
}

func readVersion(path string) (semver.Version, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return semver.Version{}, err
	}
	parsed, err := semver.Parse(string(data))
	if err != nil {
		return semver.Version{}, fmt.Errorf("%s: %w", path, err)
	}
	return parsed, nil
}

func writeVersion(path string, value semver.Version) error {
	return os.WriteFile(path, []byte(value.String()+"\n"), 0o644)
}

func appendOutputs(path string, value semver.Version) error {
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = fmt.Fprintf(file, "version=%s\ntag=%s\nmodule_tag=%s\nbranch=%s\n",
		value, value.Tag(), value.ModuleTag(), value.Branch())
	return err
}

func (a App) parseVersion(args []string) (versionOptions, error) {
	var opts versionOptions
	set := flag.NewFlagSet(a.name()+" version", flag.ContinueOnError)
	set.SetOutput(a.Stderr)
	set.BoolVar(&opts.tag, "tag", false, "print the release tag instead of the bare version")
	set.BoolVar(&opts.moduleTag, "module-tag", false, "print the nested module tag go install resolves")
	set.BoolVar(&opts.snapshot, "snapshot", false, "print what an unreleased build of this commit calls itself")
	set.BoolVar(&opts.exact, "exact", false, "leave the patch alone when building a snapshot version")
	set.StringVar(&opts.commit, "commit", "", "the commit a snapshot names, default the one checked out")
	set.StringVar(&opts.check, "check", "", "fail unless this tag matches the version file")
	set.StringVar(&opts.set, "set", "", "rewrite the version file: patch, minor, major, or X.Y.Z")
	set.StringVar(&opts.github, "github", "", "append version, tag, module_tag and branch to this file")
	if err := set.Parse(args); err != nil {
		return versionOptions{}, err
	}
	return opts, nil
}
