## What this is

SQLite reading its own pages back and checking that they still make sense: that
every index matches the table it indexes, that no page is referenced twice, and
that nothing is missing.

## When it fails

The file is damaged. Not slow, not misconfigured: damaged. The usual causes are
a disk that lied about writing, a file copied while the database was open, or
storage that lost power mid-write without honest flushing.

A damaged file can read correctly for months before anyone notices, because the
damage is in a page nothing has asked for yet.

## What to do

Stop writing to it. Recover what you can with `.dump` into a new file, and
prefer a backup taken before the failure over anything recovered from the
damaged one.

If it keeps happening on the same machine, the database is the messenger.
