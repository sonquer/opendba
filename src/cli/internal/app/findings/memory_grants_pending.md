## What this is

Statements that have been compiled, have been told how much memory they will
need to sort or hash, and are waiting in a queue for that memory to be free.

## When it is high

Anything above zero means statements are queuing for memory rather than
running. It is not a slow query; it is a query that has not started.

The usual cause is a small number of statements asking for enormous grants,
which happens when the optimiser expects far more rows than really exist, which
in turn happens when the statistics are out of date.

## What to do

The statistics reading on this screen is the first thing to check. After that,
look for sorts and hash joins over row counts the plan expected but the table
does not have; the plan screen shows both the estimate and, on a timed run,
what really came back.
