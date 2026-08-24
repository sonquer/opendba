# Security policy

## Reporting a vulnerability

Please report security issues privately through
[GitHub security advisories](https://github.com/sonquer/opendba/security/advisories/new)
rather than in a public issue.

Include what you did, what happened, and what you expected. A proof of concept
helps. You will get an acknowledgement within a few days, and an assessment with
a fix or a rejection once the report has been reproduced.

## Supported versions

Until the first stable release, only the latest commit on `main` receives fixes.

## What this project treats as a vulnerability

OPENDBA stands between a person and their production database, so the bar is:

- **A statement reaching the database that the safety layer should have stopped.**
  The classifier is default-deny over a real parse tree; anything that gets a write
  past it in READ ONLY mode is a vulnerability, even if the database role would
  have rejected it anyway.
- **A secret leaving its backend.** Passwords must never appear in `profiles.toml`,
  in the query history, in `--json` output, in a rendered frame, or in a log line.
- **Configuration trusted too readily.** Config files that other users can write,
  or values from them that reach a shell, are vulnerabilities.
- **Anything executed on the user's behalf without an explicit confirmation**,
  including SQL produced by the optional AI features.

## What it does not

- A database role with write privileges doing what its privileges allow.
  OPENDBA documents the read-only role and warns when the connected role can write,
  but it cannot be the boundary.
- Findings that require an attacker to already control the user's account.

## Defence in depth

Client-side classification is one of four layers. The others are the database
role, session pinning (`default_transaction_read_only`, statement and lock
timeouts), and a read-only transaction around every read. A report that defeats
one layer is still worth sending.
