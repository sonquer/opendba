## What this is

How much write ahead log is on disk.

Every change is written to the log before it is written to the table. The log
is then recycled once a checkpoint has flushed the change and once every
replica has consumed it. What is on disk is what has not yet been recycled.

## When it is high

The log routinely holds more than `max_wal_size`, which is a target rather than
a limit. Several times that is a signal.

Two things cause it: checkpoints are not keeping up with the rate of writes, or
a replication slot has stopped being consumed and the server is keeping the log
for a replica that is never coming back.

## What to do

The checkpoints reading on this screen answers the first. The replication
reading answers the second: an inactive slot holds the log indefinitely and is
the usual way a disk fills up without any table getting bigger.

Reading this at all needs the `pg_monitor` role. Without it the row says so
rather than guessing.
