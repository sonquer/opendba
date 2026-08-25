## What this is

What the server has spent the most time waiting for since it last started,
ignoring the waits that mean a thread was idle.

## How to read it

It names a bottleneck class rather than a problem. `PAGEIOLATCH` waits are
reads from disk, `LCK_M` waits are one session waiting on another's lock,
`CXPACKET` and `CXCONSUMER` are threads of one parallel query waiting for each
other, and `WRITELOG` is the transaction log.

The counters are cumulative since startup, so on a server that has been up for
months this is history rather than news.

## What to do

Use it to decide which of the other readings on this screen to believe first.
A server whose top wait is `PAGEIOLATCH` has a memory or indexing problem; one
whose top wait is `LCK_M` has a concurrency problem.
