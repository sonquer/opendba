## What this is

Data files that have a maximum size set, out of the data files this database
has.

## When it is high

A file with a maximum will stop growing while there is still disk underneath
it, and the database stops accepting writes at that point rather than when the
disk fills.

That is sometimes deliberate, as a way of keeping one database from eating a
shared volume. It is more often a default nobody revisited.

## What to do

Decide which it is. A deliberate ceiling wants monitoring against the ceiling;
an accidental one wants removing.
