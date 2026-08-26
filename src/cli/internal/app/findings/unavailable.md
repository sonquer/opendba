## What this is

A group of readings the role you are connected as is not allowed to see.

The report is built from four separate queries against the server's statistics
views. A role without the right to read one of them gets this row where those
readings would have been, and the rest of the report is unaffected.

## Why it happens

A purpose-made read only role is usually created by somebody thinking about
tables rather than about monitoring, and the statistics are a separate
permission on every server that has them.

## What to do

Grant it, if you can and if you want the readings.

On PostgreSQL the statistics views need the `pg_monitor` role, or membership of
`pg_read_all_stats`:

```sql
GRANT pg_monitor TO your_role;
```

`pg_monitor` grants reading statistics and nothing else. It does not allow
reading table data the role could not already read, and it does not allow
changing anything.

On SQL Server the dynamic management views need `VIEW SERVER STATE`, and the
per-database ones need `VIEW DATABASE STATE`:

```sql
GRANT VIEW SERVER STATE TO your_login;
```

The backup reading is separate again: it lives in `msdb`, and a login with no
access to that database gets `n/a` rather than a missing backup.
