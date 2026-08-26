## What this is

The share of checkpoints the server was forced into rather than ones it had
planned.

A checkpoint writes everything that changed since the last one out to disk.
PostgreSQL spreads planned checkpoints over time. A forced one happens when the
write ahead log fills up first, and it writes as fast as it can.

## When it is high

Forced checkpoints arrive in bursts, and everything else waits for the disk
while they run. Above a third of them, writes are arriving faster than the
configuration expects.

## What to do

Raise `max_wal_size` so the log has room between planned checkpoints. If they
also take a long time, `checkpoint_completion_target` spreads the writing out.
