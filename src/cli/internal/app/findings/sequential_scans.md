## What this is

The share of reads that walked a whole table instead of jumping straight to the
rows they wanted through an index.

A full scan is not always wrong. On a small table it is the fastest thing to do,
and the planner knows it. On a large one it means the database read every row to
find a few.

## When it is high

Above one read in five, something is missing an index or an index is being
ignored because a query wraps the column in a function, compares it to the wrong
type, or asks for a pattern that cannot use it.

## What to do

Find the tables doing it: `pg_stat_user_tables` ranks them by `seq_scan` and
`seq_tup_read`. Run the query with `EXPLAIN` and look for `Seq Scan` on a table
with more than a few thousand rows.
