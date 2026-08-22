<h1 align="center">TUI4DB</h1>

<p align="center">
  <b>A terminal workbench for PostgreSQL and SQLite.</b><br>
  <i>Know where you are. Know what you are running.</i>
</p>

<p align="center">
  <a href="#licence"><img alt="licence" src="https://img.shields.io/badge/licence-MIT%20OR%20Apache--2.0-blue"></a>
  <img alt="go" src="https://img.shields.io/badge/go-1.26.7-00ADD8">
  <img alt="status" src="https://img.shields.io/badge/status-in%20development-orange">
</p>

---

TUI4DB connects to a database and shows you what it is doing: the health of the
server, what is running on it right now, what it holds, and an editor to ask it
questions. Every statement is parsed before it is sent, and on a read only
profile anything that would change data is refused rather than warned about.

It is in development. The interface works and the safety model works; the rough
edges are named at the bottom of this file rather than left for you to find.

## Install

```bash
go install github.com/sonquer/tui4db/src/cli/cmd/tui4db@latest
```

There are no binary releases yet.

The first run has no connection to open, so it asks for one: a driver, then the
details, then an access mode and a colour for the environment. The connection
is tested before it is saved, and the password goes to your keychain rather
than into the profile.

## The dashboard

Every screen in this file is printed by `go run ./src/cli/cmd/screens`, which
renders the real program against a seeded SQLite file. They are captured, not
drawn, so they cannot show something that does not exist.

```text
  ▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀
  → ~ screens › sqlite 3.53.3  READ ONLY
  ─────────────────────────────────────────────────────────────────────────────

   OK   nothing needs attention                       6 checks, all of them fine

  MEMORY

    free pages     [||||||||||||||||||||]           0   ok

  LOAD

    access                                  read only   ok

  SCANS

    foreign keys                                    0   ok

  STORAGE

  ▌ integrity                                      ok   ok
    journal                                    delete   note
    size                                     20.0 KiB   note
```

The line across the top is the environment colour, so a production connection
looks different from a local one before you have read a single word. The
headline is the state of the server in one word, what it is about, and how much
came back fine. Underneath, the readings are grouped by what they are about:
memory, load, scans, storage.

`↑↓` walks the readings and `enter` opens the one under the cursor: what the
number is, what it means when it moves, and what to do about it. Every reading
has a page, on both drivers.

On PostgreSQL the dashboard also lists what the server is running right now,
with the statement, who is running it and for how long. `c` cancels a session,
`x` closes it. The sessions refresh every three seconds, the health of the
server every fifteen, and the catalogue not at all, because the shape of a
database is not weather.

## Tables and indexes

```text
  TABLES                                          every schema · 2 tables · 0 B
  ▔▔▔▔▔▔

    table ↑       rows   size   indexes   read from memory
  ─────────────────────────────────────────────────────────────────────────────
    main.orders      0    0 B       0 B                     never read  n/a
    main.users       0    0 B       0 B                     never read  n/a
```

A table shows how many rows it holds, what it and its indexes take on disk, and
a bar for the share of its reads the server answered from memory. An index
shows what it costs, how often it was read, its share of the reads on its
table, and what it is there for: a primary key, a uniqueness rule, or plain
lookups that nothing is doing.

`f` searches, `o` moves the sort to the next column, `O` turns it round, and
the heading says which column the list is in the order of. `enter` opens the
row: its columns, the counters the server keeps about it, and a page in plain
words saying what those numbers mean.

## The editor

```text
  TABLES                    │ ┃   1 SELECT ...
                            │ ┃
   main                     │ ┃
  ├── orders                │ ┃
  └── users                 │ ┃
                            │ ┃
                            │ ───────────────────────────────────────────────
                            │ nothing has run yet
```

The schema on the left, the statement and its result on the right. `tab` walks
the three panes, `enter` on a table opens its definition, `i` writes its name
into the statement, `ctrl+b` puts the schema away. `ctrl+up` and `ctrl+down`
resize the editor, `z` gives a result the whole window.

Typing offers what could finish the word: the tables of this database, the
columns of the tables the statement already names, and SQL keywords. A dot is
part of the grammar, so `catalog.` offers that schema's tables and `products.`
offers that table's columns. `tab` accepts, `↑↓` picks, `esc` closes the list.

`ctrl+r` runs the statement. A wide result moves sideways with `←→` and `enter`
opens one row as a list of keys and values, which is the only readable way to
look at a table with thirty columns.

## The safety model

Four layers. None of them trusts the other three.

| layer | what it does |
|---|---|
| the classifier | parses the statement with the real grammar for the dialect and decides from the parse tree |
| the role | whatever you granted the database user, which is yours to set |
| the session | `default_transaction_read_only`, statement and lock timeouts, `PRAGMA query_only` on SQLite |
| the transaction | every statement runs in a transaction of its own, opened read only when the profile is |

The classifier is the one that makes the difference. It is not keyword
matching: `WITH t AS (DELETE FROM users RETURNING *) SELECT * FROM t` starts
with `WITH` and is a delete, and a parse tree knows that. It refuses anything
it cannot parse, anything empty, and anything with two statements in it. It
also refuses a read that calls a function with a side effect, of which it knows
eighteen, including `pg_terminate_backend`, `setval` and `dblink_exec`.

Three verdicts:

- **allow**, a read, which runs.
- **warn**, a write, which runs in READ / WRITE once you have been shown the
  statement and said yes. `confirm_queries = false` in `settings.toml` skips
  the question, and on the command line the answer is `--yes`.
- **block**, which does not run in any mode, and says why.

READ ONLY means every write is a block. It does not mean the program cannot
change the server at all: cancelling or closing a session goes round the
classifier, so it is a dialog with a warning and a box to tick rather than a
refusal. That is deliberate, and it is the only path of its kind.

The grammars are ANTLR grammars from
[antlr/grammars-v4](https://github.com/antlr/grammars-v4), vendored and
committed, so a build cannot quietly change how a statement is read.

## Keys

`/` opens the command palette, which reaches every screen without knowing a
single shortcut. `ctrl+k` does the same and is the one to use while typing.

| key | does |
|---|---|
| `e` | query editor |
| `s` `i` | tables, indexes |
| `r` | read everything again |
| `ctrl+p` `ctrl+d` | connections |
| `ctrl+b` | show or hide the schema beside the editor |
| `ctrl+r` | run the statement |
| `ctrl+up` `ctrl+down` | resize the editor |
| `z` | zoom a result to the whole window |
| `f` `o` `O` | search a list, sort it, turn the sort round |
| `c` `x` | cancel a session, close it |
| `/` `ctrl+k` | commands |
| `?` | keys and the safety rule |
| `↑↓` `jk` | up and down |
| `enter` | open what the cursor is on |
| `esc` | back |
| `q` `ctrl+c` | quit, and again to confirm |

Terminals that speak the Kitty keyboard protocol (Ghostty, kitty, WezTerm) also
send `ctrl+enter` and `cmd+enter` to run a statement, and the footer says so
once the terminal has said it can tell them apart. On macOS the modifiers are
drawn the way the keyboard prints them, and every named key is spelled out:
`enter`, `esc`, `tab`, `space`.

## Connections

`ctrl+p` and `ctrl+d` open the same screen, because there is one place where
connections live:

```text
  ▌ localhost ·                    postgres · read only · localhost:5432/bullet

  ↑ up  ↓ down  enter open  e edit  n new  d remove  esc dashboard
```

`enter` opens a connection. On the one already in use there is nothing to open,
so it goes a level in instead, to the database and the schemas that connection
is reading: one database, any number of schemas, `space` picks, `enter` saves.
No schema ticked means every schema.

`e` opens the profile in the form that made it, so a host, a port or an access
mode can be changed without removing the connection and starting again. The
password field left empty keeps the one that is stored.

## Without the interface

```bash
tui4db inspect --connection production
tui4db schema --json
tui4db indexes --schema catalog
tui4db query "SELECT count(*) FROM orders" --limit 100
tui4db connections
```

Eight commands: `tui` (the default), `inspect`, `schema`, `indexes`, `query`,
`connections`, `version`, `help`. Five flags: `--json`, `--connection`,
`--schema`, `--limit`, and `--yes` for a statement that changes data.

Four exit codes: `0` fine, `1` a failure, `2` a usage error, `3` blocked by the
classifier. `inspect` also exits `1` when the report is unhealthy, which is
what makes it usable as a check in CI.

`inspect`, `schema` and `indexes` with `--json` emit a document against
[`src/cli/schema/tui4db.report.v1.json`](src/cli/schema/tui4db.report.v1.json).
`query --json` emits its own shape, which that schema does not cover.

## Where things are stored

```text
$XDG_CONFIG_HOME/tui4db/      (or ~/.config/tui4db/)
  profiles.toml     0600   connection metadata, never a password
  settings.toml     0600   theme, bar style, safety defaults
  secrets.age       0600   only when the encrypted vault is used
```

Both files are refused if another user can read them, and the directory is
forced to `0700`. None of this is enforced on Windows.

A password is never in `profiles.toml`, in `--json`, or on any screen. The
profile holds a reference to it: the keyring, an age encrypted vault, an
environment variable, a command to run, `~/.pgpass`, or a prompt.

`settings.toml` is read and never written, because there is no settings screen
yet. The setting worth knowing about is the shape of a bar, since how well a
glyph draws is the font's business and a terminal program cannot choose the
font it is drawn in:

```toml
[appearance]
  bar = "pipes"      # [||||] ticks, the empty ones in grey; nothing but ASCII
  # bar = "smooth"   # █▌ solid, with eighths for what falls between two cells
  # bar = "shade"    # █░ the classic block and shading pair
  # bar = "rail"     # ━─ heavy and light lines, the quietest of them
  # bar = "segments" # ▰▱ separate segments rather than one run
  # bar = "braille"  # █⣿ dots with real gaps, for fonts that blur ░
  # bar = "ascii"    # #- for terminals with no block glyphs at all
```

To see them in your own font before choosing:

```bash
go run ./src/cli/cmd/screens --bars
```

## What is not there yet

Named here rather than left to be discovered:

- **Relations and EXPLAIN** are on the driver interface and both drivers
  implement them, but nothing calls either yet.
- **Query history** has a package, a file path and a schema, and is not wired
  into the program. `history.db` is never written.
- **The `[ai]` section** of `settings.toml` is parsed and has no code behind
  it.
- **There is no settings screen**, so `settings.toml` is edited by hand.
- **MySQL, MariaDB and SQL Server** are planned behind the same driver
  interface. Only PostgreSQL and SQLite exist.

Not planned: a web interface, migrations, backup and restore, or anything that
writes to your database without you typing it first.

## Contributing

[CONTRIBUTING.md](CONTRIBUTING.md) is how to build and test it.
[AGENTS.md](AGENTS.md) is the rules the code is held to: names instead of
comments, 95% coverage in both modules, tests that need nothing installed, no
Makefile and no shell scripts.

```bash
go run ./src/tools/cmd/dev check --ci
```

## Built with

[Bubble Tea](https://charm.land), [Bubbles](https://charm.land),
[Lip Gloss](https://charm.land) and [Glamour](https://charm.land) for the
interface, [pgx](https://github.com/jackc/pgx) for PostgreSQL,
[modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite) for SQLite with no
cgo, [ANTLR](https://www.antlr.org) for the grammars, and
[go-keyring](https://github.com/zalando/go-keyring) and
[age](https://github.com/FiloSottile/age) for the secrets.

## Licence

MIT or Apache 2.0, at your option.
