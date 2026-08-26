## What this is

The memory PostgreSQL keeps table and index pages in, set by `shared_buffers`.

It is not the memory the machine has. It is the slice the server was told it
may use for caching, and every read that does not find its page here goes to
the disk.

## When it matters

The usual starting point is a quarter of the machine's memory. Much less than
that on a busy server shows up as a falling cache reading rather than here,
which is why this row is a fact and not a check.

`work_mem` is the other half of the answer: it is per sort and per hash, not
per connection, so a single query can use several times it. When that is not
enough the query spills to disk, which the spilled reading counts.

## What to do

Read this beside the cache and index cache readings. Alone it says nothing;
together with a cache ratio under 99% it says the cache is too small for what
this database is being asked to do.
