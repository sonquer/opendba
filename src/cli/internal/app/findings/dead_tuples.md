## What this is

Rows that were deleted or updated and are still on disk, waiting for the vacuum
to clear them.

PostgreSQL does not overwrite a row when you change it. It writes a new version
and leaves the old one behind until nothing can still see it. The vacuum removes
those leftovers.

## When it is high

Dead rows are read along with the live ones, so a table that is a third dead is
a third slower to scan, and its indexes grow to cover rows that no longer exist.

It rises when the table changes faster than autovacuum is allowed to keep up, or
when a long transaction keeps old versions visible and nothing can be removed.

## What to do

Look for a transaction that has been open for hours: nothing can be vacuumed
past it. Then consider a lower `autovacuum_vacuum_scale_factor` on the busiest
tables, so the vacuum starts earlier rather than in one long sweep.
