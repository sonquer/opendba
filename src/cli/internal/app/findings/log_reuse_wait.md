## What this is

What SQL Server says is stopping the transaction log from reusing its space.

## How to read it

`NOTHING` is the healthy answer.

`LOG_BACKUP` means the database is in full recovery and nobody is backing the
log up, which is the single most common cause of a full disk on a SQL Server.
`ACTIVE_TRANSACTION` means somebody opened a transaction and walked away.
`REPLICATION` and `AVAILABILITY_REPLICA` mean the log is being kept for a
consumer that is behind or gone.

## What to do

Each answer has its own fix, and none of them is growing the file. A database
in full recovery that nobody backs up should either be backed up or be in
simple recovery; being in full recovery without log backups is the worst of
both.
