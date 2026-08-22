## What this is

Indexes that nothing has read since the statistics were last reset.

An index that is never read still costs: every insert and update maintains it,
and it takes space in memory and on disk.

## When it is high

The counter is per node. An index unused here may be the one a read replica
depends on, so a zero is a question, not an answer.

## What to do

Check the same counter on every replica before dropping anything, and be sure
the statistics have been collecting long enough to cover a monthly report.
