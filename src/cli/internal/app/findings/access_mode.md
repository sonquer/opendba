## What this is

Whether this connection will send a statement that changes data.

## Why it is a finding here

On PostgreSQL, a read only profile is also pinned read only in the session, so
the server itself refuses a write even if this program somehow sent one. SQL
Server has no equivalent: there is no session setting that refuses a write, and
its driver rejects a read only transaction outright.

So on this server a read only profile rests on two things rather than three:
opendba refusing the statement before it is sent, and the permissions of the
login you connected as.

## What to do

Connect as a login that cannot write. That is the boundary the server enforces,
and it is the one that holds when this program is wrong.
