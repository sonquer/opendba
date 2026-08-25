## What this is

How full the transaction log is.

Every change is written to the log before it is written to the table. Space in
the log is reused once the change is safely elsewhere; until then it is held.

## When it is high

A log at ninety per cent will grow, and a log that cannot grow stops every
write in the database with error 9002.

A log filling up is almost never caused by the writes themselves. Something is
stopping the space from being reused, and the reading below this one names it.

## What to do

Read the log reuse reading first. Growing the file buys time and fixes nothing
if a transaction has been open since Tuesday.
