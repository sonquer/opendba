## What this is

How much a query had to write to disk because it did not fit in memory.

Sorting, grouping and hashing all get a slice of memory to work in. When the
data is bigger than the slice, the work spills into temporary files, and a step
that would have been instant becomes disk bound.

## When it is high

Any spill is worth a look. A steady stream of them means the queries and the
memory setting disagree about how much data they handle.

## What to do

`work_mem` is per sort, per connection, so raising it globally multiplies. Raise
it for the session or the role that runs the heavy reports instead. If one
statement spills every time, it is usually a sort that an index could have
provided for free.
