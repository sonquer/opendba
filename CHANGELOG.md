# Changelog

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

Work towards the first release: a terminal database workbench for PostgreSQL and
SQLite, read only by default. The interface fills the terminal, scrolls, and is
driven by a command palette on `ctrl+k` as well as by keys, which are shown the
way a Mac keyboard prints them. The editor sits beside the schema it is written
against, results fill their pane and zoom to the window, and leaving asks
first. Connections are set up, switched and removed
inside the interface, passwords go to the keychain, and every statement is
classified against the real grammar of the target database before it is sent.

### The editor

The editor has tabs, on the row between the header and the rule under it, so
they cost the screen nothing. Each one holds its own statement, its own result
and its own split, and the one being worked in is a block of colour. The key
that reaches each tab is printed on it, on the same cap the footer prints keys
on.
`ctrl+1` to `ctrl+9` reach a tab by its place; `ctrl+n`, `ctrl+w`, `ctrl+pgup`
and `ctrl+pgdown` open, close and walk. Every tab is also a command, so a tab is found by name in the
palette rather than by counting along, and clicking one moves to it.

`enter` on a table in the schema tree opens its rows in a tab of its own. The
statement that read them is written into that tab rather than sent behind it: it
meets the classifier like anything else, it is on screen, and it is the
statement to add a WHERE to. The name in it is quoted the way the server quotes
one, so a table with a capital letter opens.

A buffer holding several statements separated by semicolons is a script now, and
the run key sends the one the cursor is in rather than refusing the lot. The
guard still refuses more than one statement in a request: it is handed one
rather than talked round. The classification line says which statement of how
many is about to run.

`f6` asks the server what it would do with the statement and draws the plan as
the tree it is, with what each step costs beside it. `enter` on the plan times
it, which means running it, and asks first. Both drivers had this and nothing
called it.

`ctrl+g` opens what you have run: the statement, when, how many rows and how
long. `enter` puts one back in a tab of its own so nothing being written is
lost, `space` keeps one past the point the rest are trimmed away, and typing
searches. `history.db` was a package, a path and a schema that nothing imported;
it is written now, and a history that cannot be opened is a sentence on the
screen rather than a reason not to open the database.

The line between the statement and its result can be dragged with the pointer,
and a shifted backspace deletes a character the way an unshifted one does —
terminals send `shift+backspace` when shift is held for anything else, and the
editor was ignoring it.

The statement is coloured as it is typed rather than only once the cursor has
left it, and what could finish the word is drawn after the cursor in grey, the
way an editor suggests something: `tab` takes it, `↑↓` walk the other ways to
finish it. The list that used to cover the text is gone.

A result is written to a file with `ctrl+e`: CSV, XLSX, JSON, XML or markdown.
The dialog asks for the format, the file and how much of the result, and then
says what it is about to do before it does it. The default is everything the
statement returns rather than the rows on screen, so the statement runs again
with the row cap lifted and, on PostgreSQL, with the session's statement
timeout lifted for that transaction; the rows go to the file as they arrive, so
the size of a result is the size of the file and not also of the memory. A
statement that changes data is never run a second time: the dialog says so and
writes what is already in memory. Values are written as the server gave them
rather than as the table drew them.

The clipboard: `y` copies the value under the cursor, `Y` the row, and the
command list copies the whole result as CSV, JSON or markdown. Dragging over a
result selects rows and letting go copies them. It goes out over OSC 52, which
some terminals and tmux refuse unless told otherwise, so what is said is what
was copied rather than that it arrived.

The mouse can be handed back to the terminal, from the command list or with
`mouse = "off"` under `[appearance]`. A terminal reporting the mouse to a
program is a terminal that cannot select text with it, and that trade is now
somebody's to make.

Scrolling a large result no longer slows down with the size of it: the table is
given the rows on screen rather than all of them, and a column keeps the width
it was measured at instead of changing as the rows under it change. The schema
beside the editor scrolls again — it had stopped whenever the cursor was in
another pane, because the code that found the cursor searched the drawn text for
a marker that is only drawn while that pane has the focus.

Closing a tab asks first, and says what closing it costs: nothing, or the
statement, or the statement and the rows it returned, or a statement that is
still running.

Section headings are drawn quietly now rather than in the blue that made them
the brightest thing on the screen. A heading labels what is under it; the eye
should go to the rows, not to the word TABLES.

The wheel now moves whatever the pointer is over on that screen — the schema on
the left, the result on the right — rather than a body that has nowhere to go.

A statement that is still running says so. The spinner turns beside how long it
has been out, `esc` gives up on it, and the footer offers that key for exactly
as long as it is true. A cancelled statement is cancelled on the server as well:
the PostgreSQL pool now sends a real cancel request rather than dropping the
socket and leaving the backend to finish. There is no deadline of the program's
own any more, because the profile's statement timeout is the one that was
configured, and an answer that arrives after it was given up on is discarded
rather than drawn over the next one.

### The assistant

`a` opens a conversation about the database in front of you, and carries any
page opened with `enter` into it. The assistant has read only tools and uses
them rather than guessing, every statement it wants to run is classified by the
same guard a typed statement meets, and everything a tool returns is data rather
than instructions however it is worded.

Six back-ends behind one interface: a model running inside this process through
llama.cpp, the hosted ones from Anthropic, OpenAI and Google, an Ollama daemon,
and anything else that answers chat completions.

Nothing has to be configured before the first question. Pressing `a` with an
empty `[ai]` section opens a list of everything that could answer, grouped by
provider with the models that run on this machine at the top. Choosing a model
downloads it and starts using it; choosing a provider asks for a key and keeps
it in the keychain, writing only a reference to it. A key already in the
environment is offered as it stands. `ctrl+o` opens the same list from inside a
conversation.

A statement the assistant writes is put on the screen before it runs, and runs
when the person says so. It has already met the same classifier a typed
statement meets, so the question is whether to read that rather than whether it
is safe, and it is what lets the assistant answer anything it can write a query
for.

Before the first request of a turn that would leave this machine, the screen
says what would be sent and waits. It says it by class — your question, the
shape of the database, rows out of the tables — and asks again when a turn would
add a class you have not allowed. A model running here is not asked about,
because nothing leaves.

Local models are downloaded from Hugging Face with the download resumed if it is
interrupted and the weights checked before they are given a name. While one
arrives the list gives way to it, and leaving asks before it is given up on.

llama.cpp is not downloaded: this program carries the build it was written
against for every platform it is published for, and writes it out the first time
a conversation is opened. Nothing about the inference library is fetched at run
time.

[Unreleased]: https://github.com/sonquer/tui4db/commits/main
