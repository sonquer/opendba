## What this is

Indexes the optimiser wished existed while it was planning a statement, and the
improvement it estimated the best of them would have made.

## How to read it

This is a wish list, not a plan. The server records what would have helped one
statement without knowing what else is on the table, so the suggestions
overlap, ignore indexes that nearly fit, and never suggest dropping anything.
Creating all of them is a reliable way to make writes slow.

The impact figure is the optimiser's own estimate of how much cheaper that one
statement would have been. It says nothing about how often the statement runs.

## What to do

Treat a high impact as a reason to look at the statement, not as an instruction.
The list is also reset when the server restarts, so a small number on a server
that rebooted this morning means nothing yet.
