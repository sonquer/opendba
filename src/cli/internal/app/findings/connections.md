## What this is

How many connections the server is holding against the most it will allow.

Each connection is a process with its own memory. They are not free, and running
out of slots means the next client cannot connect at all.

## When it is high

Above 90% of the limit, one restart of an application is enough to lock everyone
out.

Watch for sessions sitting in `idle in transaction`: they hold locks and pin the
vacuum without doing any work.

## What to do

Put a pooler in front of the server rather than raising the limit. If the count
climbs without the traffic climbing, something is leaking connections.
