## What this is

How close this database is to the point where PostgreSQL stops accepting writes
to protect itself.

Every transaction gets a number, and the numbers wrap around. The database keeps
the oldest one it still needs; when that gets too far behind, the server refuses
writes until a vacuum catches up. This is the one number on the dashboard that
ends in an outage rather than a slowdown.

## When it is high

Above 70% the freeze cycle is falling behind. Above 90% treat it as urgent.

## What to do

Find the table holding it back and vacuum it. A long open transaction, an
abandoned replication slot or a stuck prepared transaction can all pin the
oldest number in place, so look for those first.
