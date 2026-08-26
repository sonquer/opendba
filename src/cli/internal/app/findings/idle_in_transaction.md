## What this is

Connections that opened a transaction and then stopped doing anything with it.

The client is still there and the transaction is still open. It holds every
lock it took, and it holds the oldest row version every cleaner has to read
past, so nothing behind it can be tidied away for as long as it sits there.

## When it is high

One of these for a few seconds is normal: it is a client between two
statements. One that has been idle for minutes is a bug, an application that
forgot to commit, or a person who opened a transaction in a console and went to
lunch.

The damage is not the connection. It is that vacuum cannot remove any row that
was alive when the transaction started, anywhere in the database, so dead rows
pile up in tables that have nothing to do with it.

## What to do

Find it in the list of sessions on this screen and look at what it last ran.
`idle_in_transaction_session_timeout` closes these automatically and is worth
setting on any server that has applications connecting to it.
