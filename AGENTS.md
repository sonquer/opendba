# AGENTS.md

Operating rules for anyone, human or agent, writing code in this repository.
These are not suggestions. Every one of them is enforced by `dev check`, and CI
runs `dev check`.

[ARCHITECTURE.md](ARCHITECTURE.md) is how the program is put together and where
things live. This file is only what is enforced.

## Rule 1: Names carry the meaning, not comments

**No comment may explain code.** Nothing inside a function body, no trailing
comment on a line, no commented-out code, no `TODO`. If a piece of code needs
prose to be understood, the code is wrong: rename it, or extract a function
whose name says what the comment would have said.

**Documentation comments are allowed on declarations**, and are the Go
convention: one sentence starting with the name being documented, a second only
if it carries a fact the name cannot. Rationale for a decision belongs in the
commit that made it, not on every read of the file.

Compiler directives (`//go:build`, `//go:generate`, `//go:embed`) are
instructions to the toolchain, not prose, and are always allowed.

```bash
go run ./src/tools/cmd/dev comments
```

The scan parses every file and compares each comment against the declaration it
is attached to. A comment anywhere else fails the build, on the first one it
finds. There is no allowlist and no `nolint` escape.

## Rule 2: 95% test coverage, both modules

Statement coverage must be at least **95%** in `src/cli` and in `src/tools`.

```bash
go run ./src/tools/cmd/dev cover
```

Thresholds live in [`dev.toml`](dev.toml): a total for the workspace, an
override per module, a floor per package, and a list of paths that are not
measured at all. A package sits below the workspace gate only when the remaining
branches cannot be reached without writing fakes for failures the standard
library cannot produce, and the file says so.

Write the test with the code, not after it. Coverage is a floor, not a goal: a
covered line that no assertion depends on is worse than an uncovered one,
because it lies.

The report is written as a single `coverage.html` in the repository root,
printed as a table in the terminal, and appended to the CI job summary. It is
never committed.

## Rule 3: Tests never need anything installed

Tests must run on a clean machine with nothing but a Go toolchain:

- **no Docker**, no `docker compose`,
- **no downloading or installing a database server**,
- no network access,
- no global state, no fixed ports, no writing outside `t.TempDir()`.

**SQLite is the test database.** It is a real driver in the product, not a test
double, it runs in-process via `modernc.org/sqlite` with no cgo, and an
in-memory instance costs nothing to create. Driver contract tests run against
it.

The PostgreSQL and SQL Server drivers are structured so their logic is testable
without a server: SQL construction, row mapping, DSN handling, session pinning,
showplan parsing and health thresholds are pure functions, and everything
touching the wire goes through a fake driver — `pgxmock` for pgx, `go-sqlmock`
for `database/sql`. Tests that genuinely need a live server read
`OPENDBA_TEST_DSN` or `OPENDBA_TEST_MSSQL_DSN` and **skip** when it is unset.

### The one exception

`src/cli/internal/ai/providers/local/llama` binds llama.cpp through `purego`,
which reaches libffi. On macOS and Windows the libffi binding writes a copy of
itself into the user's cache directory the first time any binary that links it
starts, including a test binary. That is the only write outside `t.TempDir()` in
the repository.

It is allowed because hiding it behind a build tag would mean the shipped
program and the tested program are not the same program. The write is
idempotent, it needs nothing installed, and no test depends on it. The same
package is the only entry in `dev.toml`'s exempt list that is ours rather than
generated: every statement in it is a call across a foreign function interface,
and covering it needs the native library and a model file. Everything the
adapter *decides* rather than delegates lives one directory up, in
`internal/ai/providers/local`, and is measured.

## Rule 4: No Makefile, no shell scripts

All tooling is Go, in `src/tools`, and it has a terminal interface.

```bash
go run ./src/tools/cmd/dev            # interactive dashboard
go run ./src/tools/cmd/dev check      # what CI runs
go run ./src/tools/cmd/dev cover      # coverage with the gate and the report
go run ./src/tools/cmd/dev comments   # the comment gate
go run ./src/tools/cmd/dev vuln       # govulncheck over both modules
go run ./src/tools/cmd/dev lint       # golangci-lint, pinned in src/tools/go.mod
go run ./src/tools/cmd/dev workflows  # actionlint over .github/workflows
go run ./src/tools/cmd/dev race       # the tests under the race detector, needs cgo
go run ./src/tools/cmd/dev version    # read or rewrite VERSION
go run ./src/tools/cmd/dev run        # start opendba with the values from .env
go run ./src/tools/cmd/grammar        # regenerate the vendored parsers
```

External tools are pinned as `tool` dependencies of `src/tools` and built into
`.local/bin` on demand. Nothing is installed globally, and `@latest` never
decides which version of a linter judges a pull request.

`actionlint` is the one exception, and it names its version in
`internal/toolbin` instead. It needs `go.yaml.in/yaml/v4` at rc.3, and `gosec`,
which arrives with `golangci-lint`, needs rc.6; the two cannot share a module.
It is built from a throwaway module outside the workspace, from a version that
is still written down in one place, so the pinning promise holds even though the
mechanism differs. `race` is not in `dev check`: the detector needs cgo, and
every other check runs with `CGO_ENABLED=0`.

`.env` is a convenience of the tooling, never of the product. A program that
reads configuration from the directory it happens to run in can be redirected by
anyone who can drop a file there.

Every subcommand accepts `--ci` for plain output and a meaningful exit code.

## Rule 5: Safety code is not refactorable on a hunch

`src/cli/pkg/sqlguard` decides whether a statement reaches the database, and
`src/cli/pkg/sqldialect` tells it what a statement does. The golden tables in
`sqlguard` are the specification. Changing classification without adding cases
to them is not allowed, and loosening an existing expectation requires the
commit message to say why.

The four layers are in [ARCHITECTURE.md](ARCHITECTURE.md#safety). None of them
may be quietly weakened. Multi-statement input is rejected in every access mode.

The vendored grammars under `src/cli/internal/parser/generated` are exempt from
the comment and coverage gates; the code built on top of them is not.

## Rule 6: Secrets

Passwords never appear in `profiles.toml`, in logs, in the query history
database, in `--json` output, or in a rendered frame. Profiles store a
*reference* to a secret backend, never a secret. Any code path that renders a
DSN goes through the redaction helper, and there is a test that greps rendered
output for a canary password.

## Rule 7: Drivers stay behind the interface

No `if driver == "postgres"` outside `internal/driver/postgres`. Screens ask the
driver for its `Capabilities` and degrade gracefully; they never branch on a
driver name. That includes the setup wizard: whether a profile wants a file or a
host, and which port it offers, are `Capabilities.FileBased` and
`Capabilities.DefaultPort`. Adding a driver means adding a package, registering it in
`cli.Registry`, and adding its dialect to `sqldialect`, not touching the UI.

A driver that cannot measure something reports a negative number, never zero.
Zero means measured and empty; SQLite genuinely cannot report index size, and
the interface prints `n/a` rather than a comfortable lie. A driver also
normalises its own types before they leave it, so no screen and no exporter has
to know what a `pgtype.Numeric` is.

## Versioning and releases

The version lives in `VERSION` and follows SemVer, and `dev version` is the only
thing that reads or rewrites it. Nothing about a version is parsed in YAML or in
a shell script.

A release starts with the `release-prep` workflow, run by hand with `patch`,
`minor`, `major`, or an exact `X.Y.Z`. It rewrites `VERSION`, opens a pull
request, and stops. Merging that pull request lands a change to `VERSION` on
`main`, which starts `tag.yml`: it creates `vX.Y.Z` and `src/cli/vX.Y.Z` at the
merge commit and dispatches `release.yml`. The second tag is what `go install`
resolves, because the product is a nested module.

`release.yml` refuses to publish when the tag and `VERSION` disagree, and it
runs every gate again before it builds anything.

`tag.yml` starts the release with an explicit `workflow_dispatch` rather than
relying on the tag push, because a tag pushed with `GITHUB_TOKEN` does not start
a workflow. The alternative is a GitHub App token; the dispatch needs no secret
at all, so that is what is used.

Nightlies are built from `main` every night into one rolling prerelease under
the tag `nightly`, deleted and recreated each time so that exactly one exists.
Their version is `X.Y.Z-nightly.<date>.<commit>`, with the patch bumped first so
the build sorts after the release it was cut from rather than claiming to
precede it. `git.ignore_tags` in `.goreleaser.yml` keeps both `nightly` and the
nested-module tags out of the release tooling's view of history; without it
`git describe` on `main` finds `nightly`.

Release notes are generated from the commit log, which is why the subject line
of a commit is written for somebody reading it in a release.

The `go` directive in both modules names the oldest patched release the project
supports, not the newest release available.

## Style

- Names carry the meaning that comments would have carried.
- Errors are wrapped with `fmt.Errorf("...: %w", err)` and are lowercase, no punctuation.
- No package-level mutable state outside a registry populated at init.
- `gofmt -s` clean; `golangci-lint` clean.
- Table-driven tests with named cases.
- Commit messages: Conventional Commits, imperative subject, explain *why* in the body.

## License

Dual `MIT OR Apache-2.0`. Contributions are accepted under both.
