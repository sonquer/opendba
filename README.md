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
| `a` | ask, and on any page: ask about what is on it |
| `ctrl+o` | change what answers |
| `ctrl+t` | show the working a model did before answering |
| `s` `i` | tables, indexes |
| `r` | read everything again |
| `ctrl+p` | connections |
| `ctrl+d` | databases and schemas of the connection in use |
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

`ctrl+p` is where connections live. `ctrl+d` goes one level in, to the databases
and schemas of the one you are on:

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

## Ask

`a` opens a conversation about the database in front of you. It has tools and
uses them: it reads the schema before describing it, and the readings before
saying what they mean.

```text
  ASK                                                    claude · working

    you
    which table is the biggest, and why is it slow?

    claude
    · list_tables(schema: main)
    · describe_table(schema: main, table: orders)

    orders, at 4 MiB. It has no index on `customer`, which is what the
    dashboard is calling a sequential scan.
  ─────────────────────────────────────────────────────────────────────
  ┃ ask about this database
```

Every statement it wants to run goes through the same classifier a statement you
type goes through, in the same access mode. It cannot write in any mode, and a
refusal comes back to it as a fact to report rather than an obstacle to word
differently. Everything a tool hands back is data: table names and column
comments are never instructions, however they are worded.

`enter` sends and `esc` stops an answer that is arriving, which stops a local
model computing rather than leaving it to finish something nobody will read. A
line ending in `\` is continued rather than sent.

On any page opened with `enter` — a reading, a table, an index, a row — `a`
carries it into the conversation. The question is put in the box, not sent, so
you read what would leave before it does.

### Setting it up

There is nothing to set up before pressing `a`. The first time, it opens a list
of everything that could answer, and every row in it leads somewhere:

```text
  › _

  ON THIS MACHINE                                library b10587 · included
    Gemma 4 E4B                       3.9 GiB · Apache-2.0 · fits
    Gemma 4 E2B                       2.4 GiB · Apache-2.0 · fits
    Gemma 4 12B                       6.3 GiB · Apache-2.0 · 8 GB short

  ANTHROPIC                                                    needs a key
    add a key

  OLLAMA                                                  not running here
    use this daemon
```

Choosing a model downloads it and starts answering with it. While it arrives the
list gives way to the download: what is coming, a bar, and how far it has got.
Trying to leave asks first, and what has arrived is kept, so choosing the same
model again carries on from where it stopped. Choosing a hosted provider asks for a key, keeps it in your
keychain, and writes only a reference to it in `settings.toml`. A key already in
`ANTHROPIC_API_KEY` or its siblings is offered as it stands, with nothing to
type and nothing copied anywhere.

`ctrl+o` opens the same list again from inside a conversation, so switching is
one key. Typing filters it. What is answering is written under the box you type
in, which is where you are already looking.

Nothing here asks you to edit a file. `settings.toml` is where the choices end
up, and it can be written by hand, but it does not have to be.

### Where it runs

Six back-ends, all reachable from that list:

| kind | what it is |
|---|---|
| `local` | a model running inside this process, through llama.cpp |
| `anthropic` `openai` `gemini` | the hosted ones |
| `ollama` | a daemon you already run |
| `compatible` | anything that answers chat completions, at an address you give |

```toml
[ai]
  enabled = true
  active  = "here"

[[ai.instance]]
  name  = "here"
  kind  = "local"
  model = "gemma-4-e4b-qat"

[[ai.instance]]
  name  = "claude"
  kind  = "anthropic"
  model = "claude-sonnet-5"
  key   = "keyring:ai-claude"
```

A key is a reference to a secret, exactly as a database password is. A key
written into the file is refused when the file is read, because a secret that
has been on disk in the clear cannot be untold. The keychain is the only place
the interface will put one: the encrypted vault needs a passphrase, and asking
for one from inside a full screen program would mean drawing over it and reading
keys it is already reading. A machine with no keychain is told so and pointed at
an environment variable.

### Nothing leaves without you saying so

Before the first request of a turn that would go to somebody else's machine, the
screen says what would be sent, by class rather than by byte: your question, the
shape of the database, rows out of the tables. `enter` sends it, `esc` does not.
A class you have already allowed is not asked about again in the same
conversation; one that has not appeared before stops the turn and asks.

A model running on this machine sends nothing anywhere and is not asked about.

### Models

Everything offered is Apache-2.0 or MIT and needs no account:

| model | size | fits |
|---|---|---|
| Gemma 4 E2B | 2.4 GiB | about 4 GB of memory |
| Gemma 4 E4B | 3.9 GiB | about 6 GB — the one to start with |
| Gemma 4 12B | 6.3 GiB | about 8 GB |
| GPT-OSS 20B | 11.3 GiB | about 16 GB |
| GPT-OSS 120B | 59.0 GiB | a workstation |

They are quantisation-aware trained, so there is one file each rather than a
ladder of precisions to pick from. Weights are downloaded to
`$XDG_DATA_HOME/tui4db/models`, resumed if interrupted, and checked against the
checksum the Hub reports before they are given a name.

llama.cpp itself is not downloaded at all: this program carries the build it was
written against, for all six platforms it is published for, and writes it out
the first time you open the conversation. purego opens a library by path and
cannot open one out of memory, which is the only reason the bytes reach the disk
at all. Nothing about it is fetched, so a release replaced under the tag it was
published on, a machine with no network, and an air-gapped one are all the same
here.

```toml
[ai]
  token = "env:HF_TOKEN"   # for repositories that want one
```

Set `YZMA_LIB` to use a build of your own instead.

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
  settings.toml     0600   theme, bar style, safety defaults, assistants
  secrets.age       0600   only when the encrypted vault is used

$XDG_DATA_HOME/tui4db/       (or ~/.local/share/tui4db/)
  models/                  weights, and a manifest saying what they are
  lib/                     llama.cpp, written out of this program

$XDG_STATE_HOME/tui4db/      (or ~/.local/state/tui4db/)
  history.db               statements you have run
  engine.log               what llama.cpp said this run
  crash-<when>.log         an account of a failure that ended the program
```

Both files are refused if another user can read them, and the directory is
forced to `0700`. None of this is enforced on Windows.

A password is never in `profiles.toml`, in `--json`, or on any screen. The
profile holds a reference to it: the keyring, an age encrypted vault, an
environment variable, a command to run, `~/.pgpass`, or a prompt.

`settings.toml` is written only when the assistant screen changes which instance
answers. The setting worth knowing about is the shape of a bar, since how well a
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
- **There is no settings screen for anything but the assistant**, so the rest of
  `settings.toml` is edited by hand.
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
