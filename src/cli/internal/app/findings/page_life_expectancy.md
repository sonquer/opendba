## What this is

How long SQL Server expects a page to stay in the buffer pool before something
else needs the memory and pushes it out.

It is the most direct reading of whether the server has enough memory for the
work it is being given. A cache hit ratio stays high even on a server that is
thrashing, because a page read a moment ago is still counted as a hit. This
number falls instead.

## When it is low

Under fifteen minutes the server is reading from disk far more than it should.
Under five minutes it is thrashing.

The old advice of three hundred seconds came from machines with four gigabytes
of memory. On a modern server, three hundred seconds is a fire.

## What to do

Find what is reading whole tables. A single query with no useful index can
empty the buffer pool for everything else, and the sequential scans and missing
indexes readings on this screen are where that shows up. Adding memory hides
the symptom; fixing the read removes it.

On a server with more than one NUMA node this is the average across nodes, and
one starved node can be hidden by the others.
