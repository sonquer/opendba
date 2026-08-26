package sqldialect

import (
	"github.com/antlr4-go/antlr/v4"

	"github.com/sonquer/opendba/src/cli/internal/parser/generated/postgresql"
)

func PostgreSQL() Dialect {
	return grammar{
		name:        "postgres",
		statements:  []string{"stmt"},
		suffix:      "stmt",
		analyzeRule: "analyze_keyword",
		rules:       postgresRules,
		prefixes:    postgresPrefixes,
		refinements: postgresRefinements,
		parse:       parsePostgres,
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
	"createasstmt":       {kind: KindCreate, mutating: true, materializes: true},
	"for_locking_clause": {locking: true},

	"copystmt":           {kind: KindCopy, refusal: "COPY moves data between the server and files"},
	"variablesetstmt":    {kind: KindSession, refusal: "session settings are managed by opendba"},
	"constraintssetstmt": {kind: KindSession, refusal: "session settings are managed by opendba"},
	"discardstmt":        {kind: KindSession, refusal: "session settings are managed by opendba"},
	"transactionstmt":    {kind: KindTransaction, refusal: "opendba owns transaction boundaries"},
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

	"createdbstmt":         {kind: KindCreate, mutating: true, refusal: outsideTransaction("CREATE DATABASE")},
	"dropdbstmt":           {kind: KindDrop, mutating: true, refusal: outsideTransaction("DROP DATABASE")},
	"createtablespacestmt": {kind: KindCreate, mutating: true, refusal: outsideTransaction("CREATE TABLESPACE")},
	"droptablespacestmt":   {kind: KindDrop, mutating: true, refusal: outsideTransaction("DROP TABLESPACE")},
	"altersystemstmt":      {kind: KindAlter, mutating: true, refusal: outsideTransaction("ALTER SYSTEM")},

	"createsubscriptionstmt": {kind: KindCreate, mutating: true, refusal: replication},
	"altersubscriptionstmt":  {kind: KindAlter, mutating: true, refusal: replication},
	"dropsubscriptionstmt":   {kind: KindDrop, mutating: true, refusal: replication},
}

// outsideTransaction is the refusal for a statement a server only runs on its
// own, which opendba has nowhere to put.
func outsideTransaction(name string) string {
	return name + " cannot run inside a transaction, and opendba runs every statement in one"
}

// replication is the refusal for the subscription statements, only some of whose
// forms are the ones a transaction will not hold.
const replication = "subscriptions manage replication between servers"

var postgresRefinements = map[string]refinement{
	"indexstmt":         concurrentIndex,
	"dropstmt":          concurrentIndex,
	"alterdatabasestmt": movedDatabase,
}

// concurrentIndex refuses an index built or dropped without a lock, which CREATE
// INDEX spells as a rule of its own and DROP INDEX as a bare token.
func concurrentIndex(ctx antlr.ParserRuleContext) semantics {
	for _, child := range ctx.GetChildren() {
		if _, ok := child.(*postgresql.Concurrently_Context); ok {
			return semantics{refusal: outsideTransaction("CONCURRENTLY")}
		}
		if keyword(child, postgresql.PostgreSQLParserCONCURRENTLY) {
			return semantics{refusal: outsideTransaction("CONCURRENTLY")}
		}
	}
	return semantics{}
}

// movedDatabase refuses the one form of ALTER DATABASE that moves files, which
// the grammar folds in with the forms that only set options.
func movedDatabase(ctx antlr.ParserRuleContext) semantics {
	children := ctx.GetChildren()
	for i := 0; i+1 < len(children); i++ {
		if keyword(children[i], postgresql.PostgreSQLParserSET) &&
			keyword(children[i+1], postgresql.PostgreSQLParserTABLESPACE) {
			return semantics{refusal: outsideTransaction("ALTER DATABASE SET TABLESPACE")}
		}
	}
	return semantics{}
}

// keyword reports whether a child of a parse tree node is one particular
// keyword token.
func keyword(tree antlr.Tree, kind int) bool {
	node, ok := tree.(antlr.TerminalNode)
	return ok && node.GetSymbol().GetTokenType() == kind
}

var postgresPrefixes = []prefixRule{
	{prefix: "create", rule: semantics{kind: KindCreate, mutating: true}},
	{prefix: "alter", rule: semantics{kind: KindAlter, mutating: true}},
	{prefix: "drop", rule: semantics{kind: KindDrop, mutating: true}},
	{prefix: "rename", rule: semantics{kind: KindAlter, mutating: true}},
}
