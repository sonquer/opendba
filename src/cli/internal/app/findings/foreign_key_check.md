## What this is

Rows that point at rows which are not there.

SQLite only enforces foreign keys when `PRAGMA foreign_keys` is on, and it is
off by default in every connection unless something turns it on. A file written
by a program that never did will happily hold references to rows that were
deleted years ago.

## When it is high

Every one of these is a row some query will one day join to nothing and quietly
skip, or join to and crash on. Reports come out short and nobody can say why.

## What to do

`PRAGMA foreign_key_check` lists them: which table, which row, which constraint.
Decide per table whether the child rows should be deleted or the parents
restored, then turn `PRAGMA foreign_keys = ON` on in the application so it
cannot happen again.
