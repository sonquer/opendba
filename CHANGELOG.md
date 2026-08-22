# Changelog

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

Work towards the first release: a terminal database workbench for PostgreSQL and
SQLite, read only by default. The interface fills the terminal, scrolls, and is
driven by a command palette on `ctrl+k` as well as by keys, which are shown the
way a Mac keyboard prints them. The editor sits beside the schema it is written
against, results fill their pane and zoom to the window, and leaving asks
first. Connections are set up, switched and removed
inside the interface, passwords go to the keychain, and every statement is
classified against the real grammar of the target database before it is sent.

[Unreleased]: https://github.com/sonquer/tui4db/commits/main
