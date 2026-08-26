## What this is

Pages inside the file that are no longer used and are waiting to be used again.

When a row is deleted, SQLite does not shrink the file. It marks the page free
and keeps it for the next insert. So a file can be far larger than the data in
it, and stay that way.

## When it is high

A file that is mostly free pages was much bigger once. That is not a problem in
itself: the space is reused, and reusing it is faster than growing the file.

It matters when the file is copied, backed up or shipped somewhere, because all
of that moves the free pages too.

## What to do

`VACUUM` rewrites the file without them, which needs room for a second copy
while it runs and an exclusive lock for the duration.

`PRAGMA auto_vacuum = INCREMENTAL` keeps it in check from then on, but it has
to be set before the tables are created to take effect on an existing file.
