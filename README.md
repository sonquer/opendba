<h1 align="center">TUI4DB</h1>

<p align="center">
  <b>A terminal workbench for databases.</b><br>
  <i>Know where you are. Know what you're running.</i>
</p>

<p align="center">
  <a href="#license"><img alt="license" src="https://img.shields.io/badge/license-MIT%20OR%20Apache--2.0-blue"></a>
  <img alt="go" src="https://img.shields.io/badge/go-1.26%2B-00ADD8">
  <img alt="coverage" src="https://img.shields.io/badge/coverage-95%25%20gate-brightgreen">
  <img alt="comments" src="https://img.shields.io/badge/comments-docs%20only-black">
  <img alt="status" src="https://img.shields.io/badge/status-in%20development-orange">
</p>

---

## Spot the write

One of these four statements modifies your database. You have five seconds.

```sql
-- A
SELECT * FROM users FOR UPDATE;

-- B
WITH removed AS (DELETE FROM users WHERE last_login < now() - interval '1 year' RETURNING *)
SELECT count(*) FROM removed;

-- C
EXPLAIN ANALYZE DELETE FROM sessions WHERE expired;

-- D
/* SELECT */ SELECT * INTO backup FROM orders;
```

The answer is **all four**. `A` takes row locks and blocks your writers. `B` hides a
`DELETE` inside a CTE. `C` runs the delete, because that is what `ANALYZE` means. `D`
creates a table, and the comment is there to fool the reader.

Every SQL guard built on keyword matching gets at least one of these wrong.
TUI4DB gets all four right, because it does not match keywords. It parses the
statement with PostgreSQL's own grammar and walks the tree:

```text
→ ~ tui4db query

  WITH removed AS (DELETE FROM users … RETURNING *) SELECT count(*) FROM removed

  ────────────────────────────────────────────────────────────────────────────

  ✗ blocked      SELECT · the statement contains DELETE

  This connection is READ ONLY. Nothing was sent to the server.

  [e] edit   [m] switch to READ / WRITE   [esc] cancel
```

And if the parser is ever wrong, three more layers are still standing.

## Production is red. Always.

```text
████████████████████████████████████████████████████████████████████████████████
  ● production-eu · PostgreSQL 16.3 · READ ONLY
████████████████████████████████████████████████████████████████████████████████

  connected · production-eu · postgres 16.3 · read-only · 6h window

    cache hit      [████████████████████]  99.2%   ok
    connections    [████████░░░░░░░░░░░░]  42/100  ok
    lock wait      [████████████░░░░░░░░]  61.0%   query 4f2a
    idle indexes   [████░░░░░░░░░░░░░░░░]  43 GiB  review

  tables 284 · schemas 12 · indexes 1,284 · size 1.8 TB

  ────────────────────────────────────────────────────────────────────────────

  [q] query   [s] schema   [i] indexes   [h] health   [,] settings   [?] help
```

The environment bar spans the full width of the terminal on every screen. Switch
connections and the whole interface repaints in the new colour. You will not run
the staging query against production because you looked away for a second.

## Why

- **Read only is real, not a checkbox.** Four layers, none of which trust the
  other three: a default-deny classifier over a real parse tree, a database role
  with no write grants, a pinned session, and a read-only transaction around
  every read.
- **Nothing is configured by hand.** Connections, settings and secrets are managed
  inside the TUI. Passwords never touch a config file. The profile stores a
  reference to your OS keychain, to an `age`-encrypted vault, or to your existing
  secret manager.
- **Keyboard first, Unix shaped.** `psql` + `btop` + `k9s` + `lazygit`, findings
  first, colour only where it carries information.
- **Pretty for humans, stable for machines.** The interface is for you; a
  versioned `--json` contract is for your scripts.

## Status

Early, and honest about it: PostgreSQL and SQLite work, the safety layer is
finished, and the interface covers the dashboard, the health report, the table
list and the query editor. Relations, EXPLAIN and the settings screen are next.

## Quickstart

```bash
go run ./src/cli/cmd/tui4db
```

With no connections configured, TUI4DB opens the setup wizard: pick a driver,
name it, give it a database file or a host, confirm the access mode (READ ONLY
is preselected), pick an environment colour. The connection is tested before it
is saved, and any password goes straight to your keychain. Once it is saved the
wizard hands over to the interface, already connected.

Inside the interface, `ctrl+p` lists your connections: `enter` switches to
another one, `n` opens the same wizard without leaving the program, and `d`
removes a connection along with its password once you have typed its name back.

### Keys

`ctrl+k` opens the command palette, which reaches every screen without knowing
a single shortcut. The rest:

| key | does |
|---|---|
| `e` | query editor |
| `s` `i` | tables, indexes |
| `h` | health report |
| `r` | read everything again |
| `ctrl+p` | connections |
| `ctrl+d` | databases and schemas on this server |
| `ctrl+k` | commands |
| `?` | keys and the safety rule |
| `esc` | back |
| `q` `ctrl+c` | quit |

In the editor, `ctrl+r` runs the statement and typing offers what could finish
the word: the tables of the current schema, the columns of the tables the
statement already names, and SQL keywords. `tab` accepts, `↑↓` picks, `esc`
closes the list. Columns are read from the server the moment a table is named,
once per table.

A connection with no schema of its own reads every schema the database holds,
and each table says which one it came from. `ctrl+d` lists the databases the
server lets you connect to and the schemas of the one you are in. Choosing a database reconnects with the same profile and the
same access mode; choosing a schema just reloads. Neither writes anything to
your configuration.

 Terminals that speak the Kitty
keyboard protocol (Ghostty, kitty, WezTerm) also send `ctrl+enter` and
`cmd+enter`, and the footer switches to `⌃⏎` once the terminal says it can tell
those apart. On macOS the keys are shown the way the keyboard prints them:
`⌃R`, `⌃K`, `⎋`, `⏎`.

It also works without the interface:

```bash
go run ./src/cli/cmd/tui4db connections
go run ./src/cli/cmd/tui4db inspect
go run ./src/cli/cmd/tui4db schema --json
go run ./src/cli/cmd/tui4db query "SELECT count(*) FROM orders"
```

Once released, installation will be:

```bash
go install github.com/sonquer/tui4db/src/cli/cmd/tui4db@latest
```

## The safety model

| Layer | Where | What it does |
|---|---|---|
| 0 | client | A default-deny classifier over the real parse tree. Unknown statement, unknown verdict, blocked. |
| 1 | role | The database role holds no write grants. **This is the only real boundary.** |
| 2 | session | `default_transaction_read_only`, `statement_timeout`, `lock_timeout`, `idle_in_transaction_session_timeout`. |
| 3 | transaction | Every read runs inside `BEGIN … READ ONLY`. |

Layers 0, 2 and 3 are ours. Layer 1 is yours:

```sql
CREATE ROLE tui4db LOGIN PASSWORD 'change-me';
GRANT pg_monitor TO tui4db;
GRANT CONNECT ON DATABASE app TO tui4db;
GRANT USAGE ON SCHEMA public TO tui4db;
GRANT SELECT ON ALL TABLES IN SCHEMA public TO tui4db;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT ON TABLES TO tui4db;
ALTER ROLE tui4db SET default_transaction_read_only = on;
```

If the connected role can write, the setup screen says so out loud rather than
letting a READ ONLY badge imply a guarantee it cannot make.

Two rules hold in every mode, including READ / WRITE: multi-statement input is
always rejected, and nothing is executed before you have seen how it was
classified.

The classifier is not hand-written keyword sniffing. The PostgreSQL and SQLite
grammars from [antlr/grammars-v4](https://github.com/antlr/grammars-v4) are
compiled into parsers, and classification is a walk over the parse tree with an
allow-list of node types. A statement whose shape is not recognised is blocked,
not guessed at.

## Development

Everything is a Go program. There is no Makefile, no shell script, and nothing to
install beyond a Go toolchain.

```bash
go run ./src/tools/cmd/dev
```

That opens the dashboard: pick a check, press enter, or `a` to run all of them.

The linters are pinned in `src/tools/go.mod` as tool dependencies and built into
`.local/bin` the first time they are needed, so everyone runs the same versions
and nothing is installed into your `GOPATH`.

<table>
<tr><td>

```bash
go run ./src/tools/cmd/dev check --ci
```

</td><td>every gate, plain output, the exact command CI runs</td></tr>
<tr><td>

```bash
go run ./src/tools/cmd/dev cover
```

</td><td>tests, the coverage gate, an Istanbul-style table, and <code>coverage.html</code></td></tr>
<tr><td>

```bash
go run ./src/tools/cmd/dev comments
```

</td><td>fails on any comment in Go source</td></tr>
<tr><td>

```bash
go run ./src/tools/cmd/dev vuln
```

</td><td><code>govulncheck</code> over both modules</td></tr>
<tr><td>

```bash
go run ./src/tools/cmd/grammar
```

</td><td>regenerates the vendored parsers from the grammars (needs a JDK)</td></tr>
<tr><td>

```bash
go run ./src/tools/cmd/dev run inspect
```

</td><td>starts tui4db with the values from <code>.env</code>, passing the rest through</td></tr>
</table>

Copy `.env.example` to `.env` to keep connection profiles, settings and history
inside the repository rather than in your home directory:

```bash
cp .env.example .env && go run ./src/tools/cmd/dev run
```

Both `.env` and the `.local` directory it points at are ignored by git. TUI4DB
itself never reads `.env`: a program that picks up configuration from whatever
directory you happen to stand in is a surprise, and a security relevant one, so
the development tooling reads the file and hands the values to the program it
starts.

Coverage prints like this, and writes a single self-contained `coverage.html` in
the repository root:

```text
╭──────────────────────────────────────────────────────────────────────╮
│  file                        stmts    funcs    lines   uncovered     │
├──────────────────────────────────────────────────────────────────────┤
│  ALL FILES                   97.41    98.20    97.03                 │
│    cli/pkg/sqlguard         100.00   100.00   100.00                 │
│    cli/pkg/sqldialect        98.45   100.00    98.11   142-143       │
│    cli/internal/config       96.59    97.14    96.02   88-89,131     │
╰──────────────────────────────────────────────────────────────────────╯
```

Thresholds live in [`dev.toml`](dev.toml): a total for the workspace, a value per
module, and a floor per package:

```toml
[coverage]
total = 95

[coverage.modules]
cli = 95

[package]
"cli/pkg/sqlguard" = 100
```

Two rules make the difference between this repository and most. **No comment may
explain code**: nothing inside a function body, nothing trailing a line. If code
needs prose to be understood it gets renamed or split, and the gate parses every
file to enforce it. Doc comments on declarations are expected instead. And
**95% coverage**, enforced, in both modules. See [AGENTS.md](AGENTS.md) for the
reasoning and [CONTRIBUTING.md](CONTRIBUTING.md) for the workflow.

## Where things are stored

```text
$XDG_CONFIG_HOME/tui4db/      (dir 0700)
  profiles.toml               0600   connection metadata, never a password
  settings.toml               0600   theme, safety defaults, AI
  secrets.age                 0600   only when the encrypted vault is used

$XDG_STATE_HOME/tui4db/
  history.db                  0600   query history and timings (SQLite)
```

TUI4DB refuses to start if any of them is readable by another user.

## Roadmap

- **Now**: PostgreSQL and SQLite, with connections, schema, relations, indexes,
  query, EXPLAIN and health.
- **Next**: MySQL, MariaDB and SQL Server, behind the same driver interface.
- **Later**: optional AI, where a question becomes SQL and a plan becomes an explanation.
  Generated SQL lands in the editor and goes through the same classifier as
  anything you typed. It is never executed on its own.

Not planned: a web UI, migrations, backup and restore, or anything that writes to
your database without you typing it first.

## Built with

[Bubble Tea](https://github.com/charmbracelet/bubbletea),
[Bubbles](https://github.com/charmbracelet/bubbles) and
[Lip Gloss](https://github.com/charmbracelet/lipgloss) for the interface,
[ANTLR](https://www.antlr.org) for the SQL grammars,
[pgx](https://github.com/jackc/pgx) for PostgreSQL and
[modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite) for SQLite. The last
of those is a cgo-free port, which is why every build is a static binary.

## License

Dual-licensed under either of

- Apache License, Version 2.0 ([LICENSE-APACHE](LICENSE-APACHE))
- MIT license ([LICENSE-MIT](LICENSE-MIT))

at your option. `SPDX-License-Identifier: MIT OR Apache-2.0`
