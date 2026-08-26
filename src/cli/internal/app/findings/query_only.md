## What this is

Whether this connection can write, asked of SQLite rather than of the profile.

`PRAGMA query_only = ON` makes the connection itself refuse every write, below
the classifier and independently of it. It is the second of the safety layers
and the only one the database enforces.

## When it says writing is allowed

Nothing is wrong, if that is what the profile asked for. The classifier will
still ask before a write reaches the file, and a write it lets through is kept.

It is worth noticing when you did not expect it: a profile set to READ ONLY
that reports writing allowed here means the pragma did not take, and the only
thing standing between you and a change is the classifier.

## What to do

Nothing, unless it disagrees with the mode in the header. If it does, that is
worth understanding before you type anything.
