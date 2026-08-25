package sqldialect

import (
	"github.com/antlr4-go/antlr/v4"

	"github.com/sonquer/opendba/src/cli/internal/parser/generated/tsql"
)

// MSSQL parses Transact-SQL. Its name is the driver's name, because a session
// looks up its driver and its dialect with the same word.
func MSSQL() Dialect {
	return grammar{
		name:        "mssql",
		statements:  []string{"sql_clauses", "batch_level_statement", "execute_body_batch", "go_statement"},
		rules:       tsqlRules,
		prefixes:    tsqlPrefixes,
		refinements: tsqlRefinements,
		parse:       parseTSQL,
	}
}

func parseTSQL(input antlr.CharStream, listener antlr.ErrorListener) (antlr.Tree, []string) {
	lexer := tsql.NewTSqlLexer(input)
	lexer.RemoveErrorListeners()
	lexer.AddErrorListener(listener)

	parser := tsql.NewTSqlParser(antlr.NewCommonTokenStream(lexer, antlr.TokenDefaultChannel))
	parser.RemoveErrorListeners()
	parser.AddErrorListener(listener)
	return parser.Tsql_file(), parser.RuleNames
}

// scripting is the refusal for the control flow that only means something in a
// batch, which is not what opendba sends.
const scripting = "control flow needs a batch, and opendba sends one statement"

// broker is the refusal for Service Broker, whose conversations outlive the
// request that started them.
const broker = "service broker conversations outlive a single request"

var tsqlRules = map[string]semantics{
	"select_statement_standalone": {kind: KindSelect},

	"insert_statement":   {kind: KindInsert, mutating: true},
	"update_statement":   {kind: KindUpdate, mutating: true},
	"delete_statement":   {kind: KindDelete, mutating: true},
	"merge_statement":    {kind: KindMerge, mutating: true},
	"truncate_table":     {kind: KindTruncate, mutating: true},
	"security_statement": {kind: KindGrant, mutating: true},
	"update_statistics":  {kind: KindMaintenance, mutating: true},

	"create_or_alter_procedure": {kind: KindCreate, mutating: true},
	"create_or_alter_function":  {kind: KindCreate, mutating: true},
	"create_or_alter_trigger":   {kind: KindCreate, mutating: true},

	"transaction_statement": {kind: KindTransaction, refusal: "opendba owns transaction boundaries"},
	"set_statement":         {kind: KindSession, refusal: "session settings are managed by opendba"},
	"use_statement":         {kind: KindSession, refusal: "opendba moves between databases by opening a new connection"},
	"setuser_statement":     {kind: KindSession, refusal: "SETUSER changes who this connection is"},
	"declare_statement":     {kind: KindSession, refusal: "variables do not outlive a single request"},
	"cursor_statement":      {kind: KindCursor, refusal: "cursors outlive a single request"},

	"execute_statement":  {kind: KindCall, refusal: "procedures can do anything"},
	"execute_body_batch": {kind: KindCall, refusal: "procedures can do anything"},

	"kill_statement":        {kind: KindMaintenance, refusal: "KILL ends another session, which the activity screen does"},
	"shutdown_statement":    {kind: KindMaintenance, refusal: "SHUTDOWN stops the server"},
	"reconfigure_statement": {kind: KindMaintenance, refusal: "RECONFIGURE applies server configuration"},
	"checkpoint_statement":  {kind: KindMaintenance, refusal: "CHECKPOINT is a maintenance operation"},
	"dbcc_clause":           {kind: KindMaintenance, refusal: "DBCC commands are maintenance operations"},
	"lock_table":            {kind: KindMaintenance, refusal: "LOCK blocks other sessions"},
	"waitfor_statement":     {kind: KindMaintenance, refusal: "WAITFOR holds the connection open doing nothing"},

	"rowset_function": {kind: KindCopy, refusal: "OPENROWSET reads data from files and other servers"},
	"openquery":       {kind: KindCopy, refusal: "OPENQUERY runs a statement on another server"},
	"opendatasource":  {kind: KindCopy, refusal: "OPENDATASOURCE reaches another server"},

	"conversation_statement": {kind: KindSession, refusal: broker},
	"message_statement":      {kind: KindSession, refusal: broker},

	"go_statement": {kind: KindBatch, refusal: "GO separates batches in a client, and the server never sees it"},

	"block_statement":      {kind: KindBatch, refusal: "opendba runs one statement at a time, not a BEGIN END block"},
	"if_statement":         {kind: KindBatch, refusal: scripting},
	"while_statement":      {kind: KindBatch, refusal: scripting},
	"try_catch_statement":  {kind: KindBatch, refusal: scripting},
	"goto_statement":       {kind: KindBatch, refusal: scripting},
	"break_statement":      {kind: KindBatch, refusal: scripting},
	"continue_statement":   {kind: KindBatch, refusal: scripting},
	"return_statement":     {kind: KindBatch, refusal: scripting},
	"print_statement":      {kind: KindBatch, refusal: "PRINT writes to the connection's message stream, not to a result"},
	"throw_statement":      {kind: KindBatch, refusal: "THROW raises an error instead of returning rows"},
	"raiseerror_statement": {kind: KindBatch, refusal: "RAISERROR raises an error instead of returning rows"},

	"create_database":            {kind: KindCreate, mutating: true, refusal: outsideTransaction("CREATE DATABASE")},
	"alter_database":             {kind: KindAlter, mutating: true, refusal: outsideTransaction("ALTER DATABASE")},
	"drop_database":              {kind: KindDrop, mutating: true, refusal: outsideTransaction("DROP DATABASE")},
	"backup_statement":           {kind: KindCopy, mutating: true, refusal: outsideTransaction("BACKUP")},
	"alter_server_configuration": {kind: KindAlter, mutating: true, refusal: outsideTransaction("ALTER SERVER CONFIGURATION")},
}

var tsqlPrefixes = []prefixRule{
	{prefix: "create_", rule: semantics{kind: KindCreate, mutating: true}},
	{prefix: "alter_", rule: semantics{kind: KindAlter, mutating: true}},
	{prefix: "drop_", rule: semantics{kind: KindDrop, mutating: true}},
	{prefix: "enable_", rule: semantics{kind: KindAlter, mutating: true}},
	{prefix: "disable_", rule: semantics{kind: KindAlter, mutating: true}},
}

var tsqlRefinements = map[string]refinement{
	"query_specification": selectInto,
	"table_hint":          lockingHint,
	"sybase_legacy_hint":  lockingHint,
	"built_in_functions":  advancesSequence,
}

// selectInto recognises the SELECT that writes its rows into a new table, which
// the grammar spells as a token inside the query rather than a rule of its own.
func selectInto(ctx antlr.ParserRuleContext) semantics {
	for _, child := range ctx.GetChildren() {
		if keyword(child, tsql.TSqlParserINTO) {
			return semantics{materializes: true}
		}
	}
	return semantics{}
}

// lockingTokens are the hints that take a lock rather than avoid one. NOLOCK and
// its relatives are the opposite and are not listed.
var lockingTokens = []int{
	tsql.TSqlParserHOLDLOCK,
	tsql.TSqlParserUPDLOCK,
	tsql.TSqlParserXLOCK,
	tsql.TSqlParserTABLOCK,
	tsql.TSqlParserTABLOCKX,
	tsql.TSqlParserPAGLOCK,
	tsql.TSqlParserROWLOCK,
	tsql.TSqlParserSERIALIZABLE,
	tsql.TSqlParserREPEATABLEREAD,
}

// lockingHint recognises a table hint that makes a read hold a lock other
// sessions wait on.
func lockingHint(ctx antlr.ParserRuleContext) semantics {
	for _, child := range ctx.GetChildren() {
		for _, token := range lockingTokens {
			if keyword(child, token) {
				return semantics{locking: true}
			}
		}
	}
	return semantics{}
}

// advancesSequence recognises NEXT VALUE FOR, which reads like an expression and
// moves a sequence on for every other session as it does.
func advancesSequence(ctx antlr.ParserRuleContext) semantics {
	if _, ok := ctx.(*tsql.NEXT_VALUE_FORContext); ok {
		return semantics{mutating: true}
	}
	return semantics{}
}
