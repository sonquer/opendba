## What this is

How long ago the least recently cleaned table was cleaned.

Every update and delete leaves the old version of the row on disk. Vacuum is
what removes them. Until it has been round, those rows take space, get read
past by every scan, and hold the table's statistics out of date.

## When it is high

Autovacuum normally keeps this within hours. A table that has not been cleaned
for a week is either not being written to, which is fine, or something is
stopping the cleaner, which is not.

What stops it: a transaction left open and idle, a replication slot nothing is
consuming, autovacuum turned off for the table, or a server too busy to reach
it.

## What to do

Check the idle in transaction reading and the replication reading on this
screen first, because both of them stop vacuum everywhere at once. If they are
clean, the dead rows reading says which tables are actually suffering for it.
