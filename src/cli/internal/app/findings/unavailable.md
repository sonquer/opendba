## What this is

A group of readings the role you are connected as is not allowed to see.

The report is built from four separate queries against the server's statistics
views. A role without the right to read one of them gets this row where those
readings would have been, and the rest of the report is unaffected.

## Why it happens

Most of the statistics views need the `pg_monitor` role, or membership of
`pg_read_all_stats`. A purpose-made read only role often has neither, because
the person who created it was thinking about tables rather than about
monitoring.

## What to do

Grant it, if you can and if you want the readings:

```sql
GRANT pg_monitor TO your_role;
```

`pg_monitor` grants reading statistics and nothing else. It does not allow
reading table data the role could not already read, and it does not allow
changing anything.
