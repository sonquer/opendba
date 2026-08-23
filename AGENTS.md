# AGENTS.md

Operating rules for anyone, human or agent, writing code in this repository.
These are not suggestions. Every one of them is enforced by `dev check`, and CI
runs `dev check`.

## Repository layout

```
go.work                        workspace tying the two modules together
dev.toml                       coverage thresholds
VERSION                        the version the release workflow checks against

src/cli/                       the product   module github.com/sonquer/tui4db/src/cli
  cmd/tui4db/                  binary entry point
  schema/                      the versioned --json contract
  pkg/sqlguard/                the safety policy
  pkg/sqldialect/              parse trees turned into facts about a statement
  pkg/secretref/               keychain, encrypted vault, environment, command
  internal/app/                the interface and the setup wizard
  internal/cli/                commands, and the session everything else runs in
  internal/config/             profiles and settings on disk
  internal/driver/             the driver interface and its registry
  internal/driver/postgres/    pgx
  internal/driver/sqlite/      modernc.org/sqlite, also the test database
  internal/history/            query history
  internal/report/             the report behind --json
  internal/ui/                 theme, widgets, tables
  internal/parser/generated/   vendored grammars and their generated parsers

src/tools/                     dev tooling   module github.com/sonquer/tui4db/src/tools
  cmd/dev/                     every gate, with an interface
  cmd/cover/ cmd/comments/     the same gates on their own
  cmd/grammar/                 regenerates the vendored parsers
  pkg/cover/ pkg/gate/         coverage reporting and the comment gate
  internal/                    tooling internals
```

Two modules, on purpose: dependencies of the dev tooling can never leak into the
dependency graph of the shipped binary.

## Rule 1: Names carry the meaning, not comments

**No comment may explain code.** Nothing inside a function body, no trailing
comment on a line, no commented-out code, no `TODO`. If a piece of code needs
prose to be understood, the code is wrong: rename it, or extract a function whose
name says what the comment would have said.

**Documentation comments are allowed on declarations.** A doc comment above a
package clause, a type, a function, a constant, a variable, a struct field or an
interface method is the Go convention, it is what `pkg.go.dev` renders, and a
library nobody can read is not a library. Write them for exported API, in the
standard form that starts with the name being documented.

Compiler directives (`//go:build`, `//go:generate`, `//go:embed`) are instructions
to the toolchain, not prose, and are always allowed.

This is checked mechanically. The scan parses every file and compares each
comment against the declaration it is attached to:

```bash
go run ./src/tools/cmd/dev comments
```

A comment that documents a declaration passes. A comment anywhere else fails the
build, on the first one it finds. There is no allowlist and no `nolint` escape.

## Rule 2: 95% test coverage, both modules

Statement coverage must be at least **95%** in `src/cli` and in `src/tools`.

```bash
go run ./src/tools/cmd/dev cover
```

Thresholds live in [`dev.toml`](dev.toml): a total for the workspace, an override
per module, a floor per package, and a list of paths that are not measured at
all. A package sits below the workspace gate only when the remaining branches
cannot be reached without writing fakes for failures the standard library cannot
produce, and the file says so.

Write the test with the code, not after it. Reaching 95% retroactively is how you
end up with tests that assert what the code does instead of what it should do.
Coverage is a floor, not a goal: a covered line that no assertion depends on is
worse than an uncovered one, because it lies.

The report is written as a single `coverage.html` in the repository root, printed
as a table in the terminal, and appended to the CI job summary as markdown. It is
never committed.

## Rule 3: Tests never need anything installed

Tests must run on a clean machine with nothing but a Go toolchain:

- **no Docker**, no `docker compose`,
- **no downloading or installing a database server**,
- no network access,
- no global state, no fixed ports, no writing outside `t.TempDir()`.

**SQLite is the test database.** It is a real driver in the product (not a test
double), it runs in-process via `modernc.org/sqlite` with no cgo, and an
in-memory instance costs nothing to create. Driver contract tests run against it.

The PostgreSQL driver is structured so its logic is testable without a server:
SQL construction, row mapping, DSN handling, session pinning and health
thresholds are pure functions, and everything touching the wire goes through a
narrow interface that tests fake. Tests that genuinely need a live PostgreSQL
read `TUI4DB_TEST_DSN` and **skip** when it is unset. They never start a server.

### The one exception, and why it is written down

`src/cli/internal/ai/local/llama` binds llama.cpp through `purego`, which reaches
libffi. On macOS and Windows the libffi binding writes a copy of libffi into the
user's cache directory the first time any binary that links it starts, including
a test binary. That is a write outside `t.TempDir()`, and it is the only one in
the repository.

It is allowed because the alternative is worse: the binding is what makes local
inference work without cgo, and hiding it behind a build tag would mean the
shipped program and the tested program are not the same program. The write is
idempotent, it needs nothing installed, and no test depends on it.

The same package is the only entry in `dev.toml`'s exempt list that is ours
rather than generated. Every statement in it is a call across a foreign function
interface: covering it needs the native library and a model file, which a test
suite is not allowed to want. Everything the adapter *decides* rather than
delegates lives one directory up, in `cli/internal/ai/local`, and is measured.

## Rule 4: No Makefile, no shell scripts

All tooling is Go, in `src/tools`, and it has a terminal interface.

```bash
go run ./src/tools/cmd/dev            # interactive dashboard
go run ./src/tools/cmd/dev check      # what CI runs
go run ./src/tools/cmd/dev cover      # coverage with the gate and the report
go run ./src/tools/cmd/dev comments   # the comment gate
go run ./src/tools/cmd/dev vuln       # govulncheck over both modules
go run ./src/tools/cmd/dev lint       # golangci-lint, pinned in src/tools/go.mod
go run ./src/tools/cmd/dev version    # the version from VERSION
go run ./src/tools/cmd/grammar        # regenerate the vendored parsers
go run ./src/tools/cmd/cover          # coverage on its own
go run ./src/tools/cmd/comments       # the comment gate on its own
go run ./src/tools/cmd/dev run       # start tui4db with the values from .env
```

External tools are pinned as `tool` dependencies of `src/tools` and built into
`.local/bin` on demand. Nothing is installed globally, and `@latest` never
decides which version of a linter judges a pull request.

`.env` is a convenience of the tooling, never of the product. A program that
reads configuration from the directory it happens to run in can be redirected by
anyone who can drop a file there.

Every subcommand accepts `--ci` for plain output and a meaningful exit code.

## Rule 5: Safety code is not refactorable on a hunch

`src/cli/pkg/sqlguard` decides whether a statement reaches the database, and
`src/cli/pkg/sqldialect` tells it what a statement does. The golden tables in
`sqlguard` are the specification. Changing classification without adding cases to
them is not allowed, and loosening an existing expectation requires the commit
message to say why.

Classification reads a real parse tree. The grammars under
`src/cli/internal/parser/generated` come from `antlr/grammars-v4`, are vendored
byte for byte, and the Go parsers built from them are committed so that no
contributor needs a JDK. Those packages are exempt from the comment and coverage
gates; the code built on top of them is not.

Four layers protect the user, and none of them may be quietly weakened:

0. the client-side classifier,
1. the database role (documented, user-owned, the only real boundary),
2. session pinning (`default_transaction_read_only`, timeouts),
3. a read-only transaction around every read.

Multi-statement input is rejected in every access mode.

## Rule 6: Secrets

Passwords never appear in `profiles.toml`, in logs, in the query history
database, in `--json` output, or in a rendered frame. Profiles store a
*reference* to a secret backend, never a secret. Any code path that renders a DSN
goes through the redaction helper, and there is a test that greps rendered output
for a canary password.

## Rule 7: Drivers stay behind the interface

No `if driver == "postgres"` outside `internal/driver/postgres`. Screens ask the
driver for its `Capabilities` and degrade gracefully; they never branch on a
driver name. Adding a driver means adding a package, registering it in
`cli.Registry`, and adding its dialect to `sqldialect`, not touching the UI.

A driver that cannot measure something reports a negative number, never zero.
Zero means measured and empty; SQLite genuinely cannot report index size, and
the interface prints `n/a` rather than a comfortable lie.

The PostgreSQL driver is tested with `pgxmock`, so its tests need no server and
assert the SQL that is actually sent.

## Versioning and releases

The version lives in `VERSION` and follows SemVer. A release is a tag `vX.Y.Z`
that matches that file; the release workflow refuses to publish when the two
disagree. The product is a nested module, so `go install` resolves the matching
`src/cli/vX.Y.Z` tag, which is pushed alongside the plain one.

`CHANGELOG.md` follows Keep a Changelog. User visible changes land under
`Unreleased` in the same pull request that makes them.

The `go` directive in both modules names the oldest patched release the project
supports, not the newest release available. Naming an unpatched version makes
`govulncheck` report every standard library advisory fixed after it, and it would
be right to.

## Style

- Names carry the meaning that comments would have carried.
- Errors are wrapped with `fmt.Errorf("...: %w", err)` and are lowercase, no punctuation.
- No package-level mutable state outside a registry populated at init.
- `gofmt -s` clean; `golangci-lint` clean.
- Table-driven tests with named cases.
- Commit messages: Conventional Commits, imperative subject, explain *why* in the body.

## License

Dual `MIT OR Apache-2.0`. Contributions are accepted under both.
