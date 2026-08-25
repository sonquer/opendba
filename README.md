<h1 align="center">OPENDBA</h1>

<p align="center">
  <b>A terminal workbench for PostgreSQL and SQLite.</b><br>
  <i>Know where you are. Know what you are running.</i>
</p>

<p align="center">
  <a href="https://github.com/sonquer/opendba/actions/workflows/ci.yml"><img alt="ci" src="https://github.com/sonquer/opendba/actions/workflows/ci.yml/badge.svg"></a>
  <a href="https://github.com/sonquer/opendba/actions/workflows/nightly.yml"><img alt="nightly" src="https://github.com/sonquer/opendba/actions/workflows/nightly.yml/badge.svg"></a>
  <a href="https://github.com/sonquer/opendba/releases/latest"><img alt="release" src="https://img.shields.io/github/v/release/sonquer/opendba?sort=semver"></a>
  <a href="https://scorecard.dev/viewer/?uri=github.com/sonquer/opendba"><img alt="openssf scorecard" src="https://api.scorecard.dev/projects/github.com/sonquer/opendba/badge"></a>
  <a href="#licence"><img alt="licence" src="https://img.shields.io/badge/licence-MIT%20OR%20Apache--2.0-blue"></a>
  <img alt="go" src="https://img.shields.io/badge/go-1.26-00ADD8">
  <img alt="status" src="https://img.shields.io/badge/status-in%20development-orange">
</p>

```text
  ▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀
  → ~ screens › sqlite 3.53.3  READ ONLY
  ─────────────────────────────────────────────────────────────────────────────

   OK   nothing needs attention                       6 checks, all of them fine

  MEMORY

    free pages     [||||||||||||||||||||]           0   ok

  LOAD

    access                                  read only   ok

  STORAGE

  ▌ integrity                                      ok   ok
    journal                                    delete   note
    size                                     20.0 KiB   note
```

OPENDBA connects to a database and shows you what it is doing: the health of the
server, what is running on it right now, what it holds, and an editor to ask it
questions. Every statement is parsed before it is sent, and on a read-only
profile anything that would change data is refused rather than warned about.

It is in development.

## Install

Every release publishes a signed archive for Linux, macOS and Windows, on both
amd64 and arm64. Pick one from the
[latest release](https://github.com/sonquer/opendba/releases/latest), unpack it,
and put `opendba` on your `PATH`.

```bash
go install github.com/sonquer/opendba/src/cli/cmd/opendba@latest
```

`go install` resolves the `src/cli/vX.Y.Z` tag, which every release pushes
alongside the plain one, because the product is a nested module.

Nothing else is needed at run time. There is no cgo, no libpq, and no SQLite to
install: everything the program talks to a database with is compiled into it.

### Nightly builds

[The `nightly` release](https://github.com/sonquer/opendba/releases/tag/nightly)
is `main` as it stood last night, built for the same six targets and replaced
every night. Its version reads `X.Y.Z-nightly.<date>.<commit>`, so it sorts
after the release it was cut from. It is not supported and carries no
compatibility promise.

### Verifying what you downloaded

Every archive, every SBOM and `checksums.txt` carry build provenance, for
releases and nightlies alike. Check one before you run it:

```bash
gh attestation verify opendba_0.1.0_linux_amd64.tar.gz --repo sonquer/opendba
```

## Quick start

```bash
opendba
```

The first run has no connection to open, so it asks for one: a driver, the
details, an access mode, and a colour for the environment. The connection is
tested before it is saved, and the password goes to your keychain rather than
into the profile.

After that: `e` for the editor, `ctrl+r` to run, `?` for the keys, `ctrl+k` for
everything else.

## Features

- **A dashboard** of server health, sessions, and what the database holds.
- **An editor with tabs**, autocompletion, and the schema beside it. Each tab is
  on a connection of its own, and a statement keeps running when you leave it.
- **SQL files** kept per connection, opened in a tab and saved with `ctrl+s`.
- **Results** that scroll, zoom to the window, and open a row on its own page.
- **`EXPLAIN`** and `EXPLAIN ANALYZE`, drawn as a tree.
- **Export** to CSV, JSON, XLSX, Markdown or XML.
- **History** of what you have run, searchable and forgettable.
- **An assistant** that can read the schema and draft a statement — Anthropic,
  OpenAI, Gemini, Ollama, or a local model with no network at all.
- **Connections** set up, switched and removed inside the interface, several of
  them open at once — including several on one configuration, which is two
  windows onto one server.
- **`--json`** output for scripting.
- **Mouse** support, if you want it.

## Safety

Four layers, and a statement has to pass all of them:

0. the client-side classifier — the statement is parsed against the real grammar
   of the target database and classified before it is sent,
1. the database role — documented, user-owned, and the only real boundary,
2. session pinning — `default_transaction_read_only`, statement and lock timeouts,
3. a read-only transaction around every read.

A READ ONLY profile can send nothing that changes data. A READ / WRITE profile
asks before it sends one, and keeps what it ran: the transaction is committed
unless the statement failed or you gave up on it. A statement PostgreSQL will
not run inside a transaction block at all, such as `DROP DATABASE` or
`CREATE INDEX CONCURRENTLY`, is refused by the classifier rather than sent and
rejected by the server.

Multi-statement input is refused in every access mode. A password is never
written to `profiles.toml`, to `--json`, to the query history, or to any screen:
the profile holds a reference to the keychain, an age-encrypted vault, an
environment variable, a command, `~/.pgpass`, or a prompt.

[SECURITY.md](SECURITY.md) is what to do if you find a hole in that.

## Configuration

```text
$XDG_CONFIG_HOME/opendba/     (or ~/.config/opendba/)
  profiles.toml    0600  connection metadata, never a password
  settings.toml    0600  theme, safety defaults, workspace, assistants
  secrets.age      0600  only when the encrypted vault is used

$XDG_DATA_HOME/opendba/       (or ~/.local/share/opendba/)
  sql/<connection>/      the .sql files the sidebar lists
  models/                weights, and a manifest saying what they are
  lib/                   llama.cpp, written out of this program

$XDG_STATE_HOME/opendba/      (or ~/.local/state/opendba/)
  history.db             statements you have run
  chats.db               conversations with the assistant
  engine.log             what llama.cpp said this run
  crash-<when>.log       an account of a failure that ended the program
```

Both `.toml` files are refused if another user can read them, and the directory
is forced to `0700`. None of that is enforced on Windows.

Everything in `settings.toml` is on the settings screen. Two settings are worth
knowing about from the outside, because they depend on your font and your
terminal rather than on your taste:

```toml
[appearance]
  bar = "pipes"   # pipes smooth shade rail segments braille ascii
  mouse = "on"    # off gives the terminal its mouse back, for selecting text
```

To see the bars in your own font before choosing:

```bash
go run ./src/cli/cmd/screens --bars
```

## Keys

`ctrl+k` opens the command palette, which reaches every screen without knowing a
single shortcut, and `?` prints the rest of this table inside the program.

| key | does |
|---|---|
| `e` `a` | editor, ask |
| `ctrl+r` | run the statement |
| `ctrl+s` | save the tab to a file |
| `ctrl+n` `ctrl+w` | open a tab, close it |
| `ctrl+1`…`ctrl+9` | go to a tab by its place |
| `ctrl+b` | show or hide the schema beside the editor |
| `ctrl+e` | write the result to a file |
| `ctrl+p` | connections: where you are working, and what else you could open |
| `ctrl+d` | the databases and schemas of the connection you are on |
| `ctrl+g` | what you have run |
| `f6` | what the server would do with the statement |
| `z` | zoom a result to the whole window |
| `f7` | give up on the statement this tab is waiting for |
| `esc` | go back, leaving what is running to run |
| `q` | quit, and again to confirm |

Terminals that speak the Kitty keyboard protocol (Ghostty, kitty, WezTerm) also
send `ctrl+enter` to run a statement, and the footer says so once the terminal
has said it can tell them apart. On macOS the modifiers are drawn the way the
keyboard prints them.

## What is not there yet

- **Relations** are on the driver interface and both drivers implement them, but
  nothing calls them yet.
- **MySQL, MariaDB and SQL Server** are planned behind the same driver
  interface. Only PostgreSQL and SQLite exist.

Not planned: a web interface, migrations, backup and restore, or anything that
writes to your database without you typing it first.

## Contributing

[CONTRIBUTING.md](CONTRIBUTING.md) is how to build and test it,
[ARCHITECTURE.md](ARCHITECTURE.md) is how it is put together, and
[AGENTS.md](AGENTS.md) is the rules the code is held to.

```bash
go run ./src/tools/cmd/dev check --ci
```

## Built with

[Bubble Tea, Bubbles, Lip Gloss and Glamour](https://charm.land) for the
interface, [pgx](https://github.com/jackc/pgx) for PostgreSQL,
[modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite) for SQLite with no
cgo, [ANTLR](https://www.antlr.org) for the grammars, and
[go-keyring](https://github.com/zalando/go-keyring) and
[age](https://github.com/FiloSottile/age) for the secrets.

## Licence

MIT or Apache 2.0, at your option.
