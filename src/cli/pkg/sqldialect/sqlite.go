package sqldialect

import (
	"github.com/antlr4-go/antlr/v4"

	"github.com/sonquer/opendba/src/cli/internal/parser/generated/sqlite"
)

func SQLite() Dialect {
	return grammar{
		name:          "sqlite",
		statementRule: "sql_stmt",
		explainToken:  "EXPLAIN",
		explainSafe:   true,
		rules:         sqliteRules,
		prefixes:      sqlitePrefixes,
		parse:         parseSQLite,
	}
}

func parseSQLite(input antlr.CharStream, listener antlr.ErrorListener) (antlr.Tree, []string) {
	lexer := sqlite.NewSQLiteLexer(input)
	lexer.RemoveErrorListeners()
	lexer.AddErrorListener(listener)

	parser := sqlite.NewSQLiteParser(antlr.NewCommonTokenStream(lexer, antlr.TokenDefaultChannel))
	parser.RemoveErrorListeners()
	parser.AddErrorListener(listener)
	return parser.Parse(), parser.RuleNames
}

var sqliteRules = map[string]semantics{
	"select_stmt":  {kind: KindSelect},
	"explain_stmt": {kind: KindExplain},

	"insert_stmt":         {kind: KindInsert, mutating: true},
	"update_stmt":         {kind: KindUpdate, mutating: true},
	"delete_stmt":         {kind: KindDelete, mutating: true},
	"update_stmt_limited": {kind: KindUpdate, mutating: true},
	"delete_stmt_limited": {kind: KindDelete, mutating: true},

	"begin_stmt":     {kind: KindTransaction, refusal: "opendba owns transaction boundaries"},
	"commit_stmt":    {kind: KindTransaction, refusal: "opendba owns transaction boundaries"},
	"rollback_stmt":  {kind: KindTransaction, refusal: "opendba owns transaction boundaries"},
	"savepoint_stmt": {kind: KindTransaction, refusal: "opendba owns transaction boundaries"},
	"release_stmt":   {kind: KindTransaction, refusal: "opendba owns transaction boundaries"},
	"attach_stmt":    {kind: KindSession, refusal: "ATTACH opens another database file"},
	"detach_stmt":    {kind: KindSession, refusal: "DETACH closes a database file"},
	"pragma_stmt":    {kind: KindPragma, refusal: "pragmas change how the database behaves"},
	"vacuum_stmt":    {kind: KindMaintenance, refusal: "VACUUM rewrites the database file"},
	"analyze_stmt":   {kind: KindMaintenance, refusal: "ANALYZE writes statistics tables"},
	"reindex_stmt":   {kind: KindMaintenance, refusal: "REINDEX rewrites indexes"},
}

var sqlitePrefixes = []prefixRule{
	{prefix: "create_", rule: semantics{kind: KindCreate, mutating: true}},
	{prefix: "alter_", rule: semantics{kind: KindAlter, mutating: true}},
	{prefix: "drop_", rule: semantics{kind: KindDrop, mutating: true}},
}
