## What this is

How many times two transactions have each waited for a lock the other held, and
the server broke the tie by killing one of them.

## When it is high

Any number above zero is a bug rather than a load problem. A deadlock is not
the server running out of anything: it is two pieces of code taking the same
locks in a different order.

The classic shape is two transactions updating the same two rows, one in each
order. Neither can finish, so PostgreSQL picks one and cancels it.

## What to do

The victim's application gets an error, so the details are in its log, not
here. The fix is almost always to make every transaction touch rows in the same
order, usually by sorting the identifiers before updating.

This counter is cumulative since the statistics were reset.
