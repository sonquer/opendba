## What this is

The share of transactions that ended in a rollback rather than a commit.

## When it is high

A few per cent is normal: a unique constraint doing its job, a retry after a
deadlock, a client that disconnected.

Above five per cent something is failing repeatedly and being retried. That
work still costs the server everything except the write: the parse, the plan,
the locks and the rows read all happened before the rollback threw them away.

## What to do

This counter is cumulative since the statistics were reset, so a high number on
a long lived server may be old news. Reset it or compare two readings before
acting on it.

Then look for the cause in the application logs rather than here. The server
knows the transaction failed; only the client knows why.
