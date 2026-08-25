## What this is

Everything this one database holds: its tables, their indexes, and the parts of
large values stored beside them.

## What it cannot tell you

How much room is left. No built-in function on either server reports free disk space
or the size of the filesystem, and the ones that come closest are for
superusers and answer about files rather than about the disk they sit on.

The server reading on this screen is the closest available answer: every
database on this instance added together.

## What to do

Watch it move rather than reading it once. Growth is the useful signal, and a
size that changes overnight without anyone loading anything usually means dead
rows rather than data: the dead rows and vacuum readings say so.

For the free space itself, `df -h` on the machine.
