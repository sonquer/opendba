## What this is

The share of reads that were answered from memory instead of the disk. A
database keeps recently used pages in a cache; every read it finds there is
thousands of times faster than one it has to fetch from storage.

## When it is low

Below 99% the server is going to disk more often than a warm database should.
Below 90% it is going to disk most of the time, and every query feels it.

Two things cause it: the cache is smaller than the working set, or a large
one-off scan pushed everything useful out of it.

## What to do

Check `shared_buffers` against the size of the tables you read all day. The
usual starting point is a quarter of the machine's memory. If the number
dropped suddenly rather than drifting, look for a report or a migration that
read a whole table at once.
