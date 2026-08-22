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

## Elsewhere

`ctrl+d` lists the databases this server lets you open and the schemas of the
one you are in. Choosing either is remembered in the profile, so the next run
starts where you left off.

`ctrl+p` lists your connections, adds one, or removes one along with its
password.
