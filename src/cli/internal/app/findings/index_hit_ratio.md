## What this is

The share of index lookups that were answered from memory rather than the disk.

A table can be in memory while the indexes used to find rows in it are not. The
server then reads an index page off the disk before it has even found the row,
which costs exactly what the index was supposed to save.

## When it is low

Below 99% some lookups are going to disk. Below 90% most of them are, and the
index is doing more harm than a plain read of a small table would.

Two things usually cause it: the indexes together are larger than the memory
the server was given for caching, or a report ran overnight and pushed
everything anyone actually uses out of the cache.

## What to do

Compare the total size of the indexes with `shared_buffers`. If they do not fit
and cannot be made to fit, the answer is fewer indexes rather than more memory:
look at the idle indexes reading first, because an index nothing reads still
takes its share of the cache.
