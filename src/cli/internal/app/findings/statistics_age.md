## What this is

When the optimiser last learned what is actually in these tables.

Every plan is chosen from an estimate of how many rows each step will return,
and that estimate comes from the statistics. Out of date statistics do not make
a statement wrong; they make the server choose the wrong way to run it.

## When it is high

Automatic updates are triggered by how much of a table has changed, so a large
table can go a long time without one. A month is old. Statistics that have
never been built at all are worse: the optimiser is guessing.

## What to do

`UPDATE STATISTICS` on the tables that matter, or a maintenance job that walks
them. Note that opendba will not run it for you on a read only profile, and
that rebuilding an index rebuilds its statistics as a side effect.
