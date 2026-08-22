package sqldialect

import (
	"github.com/antlr4-go/antlr/v4"

	"github.com/sonquer/tui4db/src/cli/internal/parser/generated/postgresql"
)

func PostgreSQL() Dialect {
	return grammar{
		name:          "postgres",
		statementRule: "stmt",
		analyzeRule:   "analyze_keyword",
		rules:         postgresRules,
		prefixes:      postgresPrefixes,
		parse:         parsePostgres,
	}
}

func parsePostgres(input antlr.CharStream, listener antlr.ErrorListener) (antlr.Tree, []string) {
	lexer := postgresql.NewPostgreSQLLexer(input)
	lexer.RemoveErrorListeners()
	lexer.AddErrorListener(listener)

	parser := postgresql.NewPostgreSQLParser(antlr.NewCommonTokenStream(lexer, antlr.TokenDefaultChannel))
	parser.RemoveErrorListeners()
	parser.AddErrorListener(listener)
	return parser.Root(), parser.RuleNames
}

var postgresRules = map[string]semantics{
	"selectstmt":       {kind: KindSelect},
	"variableshowstmt": {kind: KindShow},
	"explainstmt":      {kind: KindExplain},

	"insertstmt":              {kind: KindInsert, mutating: true},
	"updatestmt":              {kind: KindUpdate, mutating: true},
	"deletestmt":              {kind: KindDelete, mutating: true},
	"mergestmt":               {kind: KindMerge, mutating: true},
	"truncatestmt":            {kind: KindTruncate, mutating: true},
	"grantstmt":               {kind: KindGrant, mutating: true},
	"grantrolestmt":           {kind: KindGrant, mutating: true},
	"revokestmt":              {kind: KindRevoke, mutating: true},
	"revokerolestmt":          {kind: KindRevoke, mutating: true},
	"commentstmt":             {kind: KindAlter, mutating: true},
	"seclabelstmt":            {kind: KindAlter, mutating: true},
	"definestmt":              {kind: KindCreate, mutating: true},
	"rulestmt":                {kind: KindCreate, mutating: true},
	"indexstmt":               {kind: KindCreate, mutating: true},
	"viewstmt":                {kind: KindCreate, mutating: true},
	"refreshmatviewstmt":      {kind: KindCreate, mutating: true},
	"importforeignschemastmt": {kind: KindCreate, mutating: true},
	"reassignownedstmt":       {kind: KindAlter, mutating: true},
	"dropownedstmt":           {kind: KindDrop, mutating: true},

	"into_clause":        {materializes: true},
	"createtableasstmt":  {kind: KindCreate, mutating: true, materializes: true},
	"for_locking_clause": {locking: true},

	"copystmt":           {kind: KindCopy, refusal: "COPY moves data between the server and files"},
	"variablesetstmt":    {kind: KindSession, refusal: "session settings are managed by tui4db"},
	"constraintssetstmt": {kind: KindSession, refusal: "session settings are managed by tui4db"},
	"discardstmt":        {kind: KindSession, refusal: "session settings are managed by tui4db"},
	"transactionstmt":    {kind: KindTransaction, refusal: "tui4db owns transaction boundaries"},
	"lockstmt":           {kind: KindMaintenance, refusal: "LOCK blocks other sessions"},
	"dostmt":             {kind: KindCall, refusal: "anonymous code blocks can do anything"},
	"callstmt":           {kind: KindCall, refusal: "procedures can do anything"},
	"vacuumstmt":         {kind: KindMaintenance, refusal: "VACUUM is a maintenance operation"},
	"clusterstmt":        {kind: KindMaintenance, refusal: "CLUSTER rewrites tables"},
	"reindexstmt":        {kind: KindMaintenance, refusal: "REINDEX rewrites indexes"},
	"checkpointstmt":     {kind: KindMaintenance, refusal: "CHECKPOINT is a maintenance operation"},
	"loadstmt":           {kind: KindMaintenance, refusal: "LOAD loads shared libraries"},
	"declarecursorstmt":  {kind: KindCursor, refusal: "cursors outlive a single request"},
	"fetchstmt":          {kind: KindCursor, refusal: "cursors outlive a single request"},
	"closeportalstmt":    {kind: KindCursor, refusal: "cursors outlive a single request"},
	"preparestmt":        {kind: KindPrepared, refusal: "prepared statements outlive a single request"},
	"executestmt":        {kind: KindPrepared, refusal: "prepared statements outlive a single request"},
	"deallocatestmt":     {kind: KindPrepared, refusal: "prepared statements outlive a single request"},
	"listenstmt":         {kind: KindSession, refusal: "notification channels outlive a single request"},
	"unlistenstmt":       {kind: KindSession, refusal: "notification channels outlive a single request"},
	"notifystmt":         {kind: KindSession, refusal: "notifications reach other sessions"},
}

var postgresPrefixes = []prefixRule{
	{prefix: "create", rule: semantics{kind: KindCreate, mutating: true}},
	{prefix: "alter", rule: semantics{kind: KindAlter, mutating: true}},
	{prefix: "drop", rule: semantics{kind: KindDrop, mutating: true}},
	{prefix: "rename", rule: semantics{kind: KindAlter, mutating: true}},
}
