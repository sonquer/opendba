## What this is

Replication slots that no replica is currently consuming.

A slot is a promise. It tells the server to keep every write ahead log segment
a replica has not read yet, however long that takes, so the replica can catch
up when it comes back.

## Why it is critical rather than a warning

An inactive slot keeps that promise to a replica that may never return. The
log grows without limit, and the disk fills without a single table getting
bigger. It also holds the oldest row version, so vacuum stops making progress
across the whole server.

This is the most common way a PostgreSQL disk fills up unexpectedly.

## What to do

Find out whether the replica is coming back. If it is, this is a temporary
state and the log is doing its job. If it is not, drop the slot:
`SELECT pg_drop_replication_slot('name')`.

The wal reading on this screen shows how much log the promise is holding.
