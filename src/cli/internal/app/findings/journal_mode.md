## What this is

How SQLite makes a change durable: what it writes, and where, so that a crash
mid-write leaves the file readable.

`delete` is the default and the oldest: the old pages are copied to a separate
journal file, then the change is made, then the journal is removed.

`wal` writes the change to a log first and folds it back into the file later.
It is the one to want: readers do not block the writer and the writer does not
block readers.

## When it matters

`off` and `memory` mean there is no journal to recover from. A crash then does
not corrupt one row, it corrupts the file.

`wal` needs the file to be on local storage. It uses shared memory to
coordinate, and that does not work across a network filesystem.

## What to do

`PRAGMA journal_mode = WAL` on local storage, and leave it at `delete` on
anything shared. Never `off` on a file that matters.
