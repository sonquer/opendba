## Safety

Everything you type is classified against the real grammar of the database
before it is sent. In **READ ONLY** mode a statement that changes data never
leaves this program, and multi statement input is refused in every mode.

A profile marked READ ONLY is enforced four times over: the classifier here, the
privileges of the role you connect as, a read only session, and a read only
transaction around every statement.

## The editor

The editor has tabs. Each one holds its own statement, its own result and its
own split. `ctrl+1` to `ctrl+9` reach a tab by its place, `ctrl+n` opens one and
`ctrl+w` closes it after asking. Every tab is in this list by name as well.

A statement keeps running when you leave the editor: `esc` goes back and the tab
waits on its own, marked with a `•` in the strip, with the header saying how much
is still out from wherever you are. `f7` gives up on the statement a tab is
waiting for, and closing a tab gives up on its statement after asking.

The schema of the current database sits beside the editor. `enter` on a table
opens its rows in a tab of its own, with the statement that read them written
into it. `i` writes a name into the statement, `space` opens a table's columns.

Under the schema is FILES: the `.sql` files kept beside this connection.
`enter` or a click opens one in a tab of its own, or moves to the tab it is
already in. `ctrl+s` writes the tab back to its file, asking for a name the
first time and asking again before it writes over a name that is taken; a tab
holding something its file does not is marked with a `*`.
`ctrl+x` removes a file after asking. They live in `sql/<connection>/` under the
data directory unless "sql files" in the settings names somewhere else.

The statement is coloured as you type it. What could finish the word is drawn
after the cursor in grey: tables, the columns of the tables the statement
already names, and SQL keywords. `tab` takes it, `↑↓` walk the other ways to
finish it.

A buffer holding several statements separated by semicolons is a script: what
runs is the one the cursor is in, and the line under the editor says which. `f6`
asks what the server would do with it instead of doing it.

`ctrl+g` opens what you have run. `enter` there puts a statement back in a tab
of its own, `space` keeps one, and typing searches.

`ctrl+e` writes the result to a file as CSV, XLSX, JSON, XML or markdown. It
writes everything the statement returns rather than the rows on screen, which
means running the statement again; the dialog says so and asks. A statement that
changes data is never run twice.

`y` copies the value under the cursor and `Y` the row. Clicking one row opens
that record on a page of its own, since a row wider than the screen is a row you
want to read first; dragging across several selects them, and letting go copies
them. Copying uses OSC 52, which tmux ignores unless `set -g set-clipboard on`.

While the program is reading the mouse your terminal cannot select text with it.
"mouse on or off" in this list hands it back.

## Asking

`a` opens a conversation about this database. It reads the schema and the
readings with tools rather than guessing, and every statement it wants to run
meets the same classifier yours does, in the same access mode. It cannot write
in any mode.

On a page opened with `enter` — a reading, a table, an index, a row — `a` puts
that page into the box as a question. It is not sent until you send it.

`ctrl+g` opens the conversations you have had. `enter` carries one on from where
it left off, `ctrl+x` forgets one, `ctrl+n` begins another, and typing searches.
A conversation is kept whole, including the rows a tool read for it, which is
what lets it be carried on and why the settings screen can empty the lot.

`enter` sends, `esc` stops an answer that is still arriving, and a line ending
in a backslash is continued rather than sent. `ctrl+c` closes the program, and
asks first.

Before anything goes to a machine that is not this one, the screen says what
would be sent and waits for you. A model running here sends nothing anywhere.

A statement the assistant writes is shown to you before it runs, and runs only
when you say so. It has already been through the same classifier your own
statements meet, so what you are being asked is whether to read that, now — and
it is what lets the assistant answer anything it can write a query for.

`ctrl+o` opens the list of everything that could answer: the models that run on
this machine, and the providers that need a key. Choosing a model downloads it
and starts using it; choosing a provider asks for a key and keeps it in your
keychain. The first time you press `a` with nothing set up, that list opens by
itself. While a model is arriving the list gives way to the download, and
leaving asks before it gives up on it; what has arrived is kept either way.

What is answering is written under the box you type in. `pgup`, `pgdown` and the
mouse wheel walk back through a long conversation, and `ctrl+t` opens out the working a model
showed before its answer.

A model that runs here is read into memory when you ask your first question, not
before: opening this screen to read what was said earlier costs nothing. The box
is dimmed while that happens, as it is while an answer is arriving, and the
question waits where you typed it. `esc` stops
either one. "release the model" in the command list gives that memory
back, as does `r` on its row in `ctrl+o`; the next question reads it in again.

When something goes wrong badly enough to end the program, what happened is
written to a file under the state directory rather than to a screen that is
about to be cleared: `crash-<when>.log`, and `engine.log` beside it, which is
what the inference library itself was saying at the time.

## Settings

The `settings` command reaches everything in `settings.toml` that is not the
assistant, and empties the query history or the conversations. The row limit,
the timeouts and the access mode reach the server when a connection is opened,
so they apply to the next one.

## Elsewhere

`ctrl+p` opens the connections over whatever you were looking at, from anywhere.
It has two halves. OPEN is where you are working: `enter` goes to a session and a
bare digit goes straight to the one numbered beside it, the way `ctrl+1` reaches
a tab. CONFIGURED is what `profiles.toml` holds: `enter` there opens **another**
session, even on a connection you already have open, so two windows onto one
server are two rows rather than a fight.

A second session on the same database is called `#2`, and the tab strip names
them apart. Each row says how many tabs are on that session, and the one you are
working in says `in use`. What you open lands in the tab in front when it is
empty, and in a tab of its own when it is holding a statement or a result — so a
tab with something in it is never the one that moves.

`x` closes a session — without asking when no tab is on it and nothing is
running, and after saying what goes with it when there is. `n`, `e` and `d`
make, change and remove a profile, and never a session. `f` filters.

`ctrl+d` opens the databases and schemas of the session in front. It is a form:
`space` moves the dot or ticks a box, `enter` applies. Ticking schemas only
re-reads; choosing another database reconnects. Choosing is remembered in the
profile, so the next run starts where you left off.

Each tab belongs to a session: the header, the access mode and the schema tree
all follow the tab in front.
