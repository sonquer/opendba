## What this is

Everything this server holds, all of its databases added together, not just the
one you are connected to.

## What it cannot tell you

How much room is left on the machine. No built-in PostgreSQL function reports
free disk space or the size of the filesystem. The functions that come closest,
`pg_ls_dir` and `pg_stat_file`, are for superusers and answer about files
rather than about the disk they sit on.

So this number is what the server is using, and the space it has to grow into
has to come from somewhere else.

## What to do

Watch it move rather than reading it once. A number that doubles in a week is
worth more than a number that is large.

For the free space itself: `df -h` on the machine, or whatever collects metrics
from the host. A database is rarely the only thing on a disk.
