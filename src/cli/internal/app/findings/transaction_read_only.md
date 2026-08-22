## What this is

Whether this session can change anything, asked of the server rather than of
the profile.

The profile says which mode was chosen. This says what the server actually did
with it, which is the more useful of the two: it is the answer after the
connection was made, the role's own settings were applied, and the session was
configured.

## When it says read / write

Nothing is wrong. It means statements that change data will reach the server if
you run them, and the classifier will ask before they do.

It is worth noticing when you did not expect it. A profile set to READ ONLY
that reports read / write here means the session setting did not take, and the
only thing standing between you and a write is the classifier.

## What to do

Nothing, unless it disagrees with the mode in the header. If it does, that is
worth understanding before you type anything.
