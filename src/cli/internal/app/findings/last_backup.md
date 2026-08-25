## What this is

When this database was last written somewhere that is not this server.

## When it is high

A day is a day of work that only exists in one place. A week, on a database
anybody depends on, is not a backup strategy.

The recovery model shown beside it changes what the number means: under simple
recovery a full backup is the only restore point there is, so the age is
exactly how much would be lost. Under full recovery the log backups fill the
gap, and this reading is only half the picture.

## What to do

If it says `n/a`, this login cannot read `msdb`, and the answer is unknown
rather than bad. Nothing here checks that the backups can be restored, which is
the only test that counts.
