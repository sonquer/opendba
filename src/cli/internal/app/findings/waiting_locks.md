## What this is

How many statements are stopped, waiting for a lock another transaction holds.

## When it is high

Anything above zero for more than a moment means work is queued behind one
transaction. The one holding the lock is usually not the one you notice: the
queue behind it is.

## What to do

Find the oldest transaction on the sessions list and look at what it is doing.
If it is idle in a transaction, it is holding locks for nothing, and closing it
frees the queue.
