## Safety

Everything you type is classified against the real grammar of the database
before it is sent. In **READ ONLY** mode a statement that changes data never
leaves this program, and multi statement input is refused in every mode.

A profile marked READ ONLY is enforced four times over: the classifier here, the
privileges of the role you connect as, a read only session, and a read only
transaction around every statement.

## The editor

The schema of the current database sits beside the editor. `enter` on a table
writes its qualified name into the statement, `space` opens its columns.

Typing offers what could finish the word: tables, the columns of the tables the
statement already names, and SQL keywords.

## Asking

`a` opens a conversation about this database. It reads the schema and the
readings with tools rather than guessing, and every statement it wants to run
meets the same classifier yours does, in the same access mode. It cannot write
in any mode.

On a page opened with `enter` — a reading, a table, an index, a row — `a` puts
that page into the box as a question. It is not sent until you send it.

`enter` sends, `esc` stops an answer that is still arriving, and a line ending
in a backslash is continued rather than sent. `ctrl+c` closes the program, and
asks first.

Before anything goes to a machine that is not this one, the screen says what
would be sent and waits for you. A model running here sends nothing anywhere.

`ctrl+o` opens the list of everything that could answer: the models that run on
this machine, and the providers that need a key. Choosing a model downloads it
and starts using it; choosing a provider asks for a key and keeps it in your
keychain. The first time you press `a` with nothing set up, that list opens by
itself. While a model is arriving the list gives way to the download, and
leaving asks before it gives up on it; what has arrived is kept either way.

What is answering is written under the box you type in. `pgup` and `pgdown` walk
back through a long conversation, and `ctrl+t` opens out the working a model
showed before its answer.

A model that runs here is read into memory when you choose it, and the box is
shut while that happens. `u` gives that memory back; the next question reads it
in again.

## Elsewhere

`ctrl+d` lists the databases this server lets you open and the schemas of the
one you are in. Choosing either is remembered in the profile, so the next run
starts where you left off.

`ctrl+p` lists your connections, adds one, or removes one along with its
password.
