# Generated parsers

This directory holds vendored SQL grammars and the Go parsers generated from
them. **Nothing here is written by hand and nothing here should be edited.**

| Directory | Grammar | Upstream |
|---|---|---|
| `postgresql/` | `PostgreSQLLexer.g4`, `PostgreSQLParser.g4` | [antlr/grammars-v4](https://github.com/antlr/grammars-v4/tree/master/sql/postgresql) |
| `sqlite/` | `SQLiteLexer.g4`, `SQLiteParser.g4` | [antlr/grammars-v4](https://github.com/antlr/grammars-v4/tree/master/sql/sqlite) |

The `.g4` files are vendored byte for byte from upstream, including their
copyright headers, so that updating them is a readable diff. The Go target needs
`this.` rewritten to the generated receiver; that rewrite happens in memory
during generation and is never written back to the vendored grammar.

Regenerate with:

```bash
go run ./src/tools/cmd/grammar
```

That downloads the ANTLR tool into a cache directory and needs a JDK. The
generated code is committed, so building and testing this repository never does.

Both grammars keep their upstream licences, stated in the headers of the `.g4`
files. Everything else in this repository is dual-licensed MIT or Apache-2.0.

These packages are exempt from the repository's comment and coverage gates: they
are not our code, and the classification built on top of them is tested in
`src/cli/pkg/sqldialect` and `src/cli/pkg/sqlguard` instead.
