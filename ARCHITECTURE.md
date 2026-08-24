# Architecture

## Two modules

```
src/cli/     the product        module github.com/sonquer/tui4db/src/cli
src/tools/   the dev tooling    module github.com/sonquer/tui4db/src/tools
```

Two modules on purpose: dependencies of the dev tooling can never leak into the
dependency graph of the shipped binary. `go.work` ties them together.

## Layout

```
src/cli/
  cmd/tui4db/                  binary entry point
  cmd/screens/                 renders the interface against a seeded database
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
  internal/ai/                 the assistant: agent, tools, providers
  internal/chats/              conversations
  internal/history/            query history
  internal/export/             a result written to a file
  internal/sqlfiles/           the .sql files kept beside a connection
  internal/report/             the report behind --json
  internal/ui/                 theme, widgets, tables, layout
  internal/parser/generated/   vendored grammars and their generated parsers

src/tools/
  cmd/dev/                     every gate, with a terminal interface
  cmd/cover/ cmd/comments/     the same gates on their own
  cmd/grammar/                 regenerates the vendored parsers
  pkg/cover/ pkg/gate/         coverage reporting and the comment gate
```

## The path of a statement

The interface is one Bubble Tea `Model` (`internal/app`). Everything that
touches the outside world is a `tea.Cmd` returning a message, so `Update` stays
a pure function of a model and a message, and a test drives the whole program by
feeding it keys.

1. A keypress reaches `Model.Update`.
2. `script.chosen` picks the statement the cursor is in, out of the buffer.
3. `pkg/sqldialect` parses it against the grammar of the connected database and
   reports what it is.
4. `pkg/sqlguard` decides whether it may be sent, from that report and the
   access mode of the profile.
5. `driver.Conn.Query` runs it inside a transaction, read-only on a read-only
   profile.
6. A `driver.ResultSet` is drained into `[][]any`, and `ui.Cell` draws each
   value into the grid.

## Safety

Four layers, and none of them may be quietly weakened:

0. the client-side classifier,
1. the database role — documented, user-owned, the only real boundary,
2. session pinning (`default_transaction_read_only`, timeouts),
3. a read-only transaction around every read.

Multi-statement input is refused in every access mode. The golden tables in
`pkg/sqlguard` are the specification of what is refused; the grammars under
`internal/parser/generated` are vendored from `antlr/grammars-v4` byte for byte,
and the parsers built from them are committed so no contributor needs a JDK.

## Drivers

`internal/driver` is an interface and a registry. Screens ask a driver for its
`Capabilities` and degrade gracefully; they never branch on a driver name. A
driver that cannot measure something reports a negative number, never zero —
zero means measured and empty.

Adding one means adding a package, registering it in `cli.Registry`, and adding
its dialect to `pkg/sqldialect`. It does not mean touching the interface.

## The assistant

`internal/ai` is a provider-agnostic agent. The app talks to a `Talk` interface
and never to a provider. `ai/agent` runs the loop, `ai/tool` is what a model may
call — reading the schema, drafting a statement — and each provider under
`ai/providers` turns that into its own wire format. The local provider runs
llama.cpp through `purego`, so there is no cgo and no network.

Nothing is sent anywhere until you say so.

## The rules the code is held to

[AGENTS.md](AGENTS.md), all of them enforced by `go run ./src/tools/cmd/dev check`.
