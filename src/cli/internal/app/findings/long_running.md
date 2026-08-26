## What this is

The longest statement currently running, measured from when it started.

## When it is high

A query that has been going for a minute is either doing real work over a lot
of rows or waiting for something. The waiting reading says which.

Over five minutes on an interactive database is usually a mistake: a missing
`WHERE`, a join that multiplied, or a report that should have been run
somewhere else.

Long statements are not only slow for whoever is waiting on them. They hold
their locks, and while one is inside a transaction nothing older than it can be
vacuumed.

## What to do

The list of sessions on this screen shows it, and `c` cancels the statement
without closing the connection. Cancel before you close: an application usually
recovers from a cancelled query and rarely recovers from a dropped connection.
