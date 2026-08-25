package sqldialect

import (
	"strings"
	"testing"

	"github.com/antlr4-go/antlr/v4"
)

func TestDefaultFactoryKnowsEveryDialect(t *testing.T) {
	factory := Default()
	names := factory.Names()
	if len(names) != 3 || names[0] != "mssql" || names[1] != "postgres" || names[2] != "sqlite" {
		t.Fatalf("Names() = %v", names)
	}
	for _, name := range []string{"postgres", "POSTGRES", "SQLite", "mssql", "MSSQL"} {
		if _, err := factory.Get(name); err != nil {
			t.Errorf("Get(%q) = %v", name, err)
		}
	}
	_, err := factory.Get("oracle")
	if err == nil {
		t.Fatal("want an error for an unknown dialect")
	}
	if !strings.Contains(err.Error(), "postgres") {
		t.Errorf("the error should list what is available: %v", err)
	}
}

func TestFactoryRegistersLaterDialects(t *testing.T) {
	factory := NewFactory()
	if len(factory.Names()) != 0 {
		t.Fatal("an empty factory must know nothing")
	}
	factory.Register(stubDialect{})
	if _, err := factory.Get("stub"); err != nil {
		t.Fatalf("Get: %v", err)
	}
}

type stubDialect struct{}

func (stubDialect) Name() string { return "stub" }

func (stubDialect) Analyze(string) Analysis { return Analysis{} }

func TestStatementReads(t *testing.T) {
	cases := map[string]Statement{
		"plain select": {Kind: KindSelect},
		"mutating":     {Kind: KindDelete, Mutating: true},
		"locking":      {Kind: KindSelect, Locking: true},
		"materialises": {Kind: KindSelect, Materializes: true},
		"refused":      {Kind: KindCall, Refusal: "no"},
	}
	if !cases["plain select"].Reads() {
		t.Error("a plain select reads")
	}
	for name, statement := range cases {
		if name == "plain select" {
			continue
		}
		if statement.Reads() {
			t.Errorf("%s must not count as a read", name)
		}
	}
}

func TestAnalysisHelpers(t *testing.T) {
	valid := Analysis{Statements: []Statement{{Kind: KindSelect}}}
	if !valid.Valid() || valid.FirstError() != "" {
		t.Error("an analysis without errors is valid")
	}
	broken := Analysis{Errors: []SyntaxError{{Line: 1, Column: 7, Message: "syntax error"}}}
	if broken.Valid() {
		t.Error("an analysis with errors is not valid")
	}
	if got := broken.FirstError(); got != "line 1:7 syntax error" {
		t.Errorf("FirstError() = %q", got)
	}
}

func TestPostgresReportsSyntaxErrorPositions(t *testing.T) {
	analysis := PostgreSQL().Analyze("SELECT * FROM")
	if analysis.Valid() {
		t.Fatal("an incomplete statement must report an error")
	}
	if analysis.Errors[0].Line != 1 {
		t.Errorf("errors = %+v", analysis.Errors)
	}
}

func TestStatementsCarryTheirText(t *testing.T) {
	analysis := PostgreSQL().Analyze("SELECT 1")
	if len(analysis.Statements) != 1 {
		t.Fatalf("statements = %+v", analysis.Statements)
	}
	if analysis.Statements[0].Text == "" {
		t.Error("a statement must carry its text")
	}
}

func TestUnrecognisedStatementsAreRefused(t *testing.T) {
	dialect := grammar{
		name:       "empty",
		statements: []string{"stmt"},
		parse:      PostgreSQL().(grammar).parse,
	}
	analysis := dialect.Analyze("SELECT 1")
	if len(analysis.Statements) != 1 {
		t.Fatalf("statements = %+v", analysis.Statements)
	}
	statement := analysis.Statements[0]
	if statement.Kind != KindUnknown || statement.Refusal == "" {
		t.Fatalf("an unmapped statement must be refused, got %+v", statement)
	}
}

func TestPrefixRulesOnlyMatchStatements(t *testing.T) {
	dialect := PostgreSQL().(grammar)
	if _, ok := dialect.lookup("create_generic_options"); ok {
		t.Error("prefix rules must only classify statement rules")
	}
	if _, ok := dialect.lookup("createstmt"); !ok {
		t.Error("statement rules must be classified by prefix")
	}
}

func TestSQLiteExplainIsAlwaysARead(t *testing.T) {
	dialect := SQLite()
	cases := map[string]Kind{
		"EXPLAIN SELECT 1":                 KindExplain,
		"EXPLAIN QUERY PLAN SELECT 1":      KindExplain,
		"EXPLAIN DELETE FROM t":            KindExplain,
		"EXPLAIN QUERY PLAN DELETE FROM t": KindExplain,
		"EXPLAIN INSERT INTO t VALUES (1)": KindExplain,
	}
	for sql, want := range cases {
		analysis := dialect.Analyze(sql)
		if !analysis.Valid() {
			t.Fatalf("%q did not parse: %s", sql, analysis.FirstError())
		}
		statement := analysis.Statements[0]
		if statement.Kind != want {
			t.Errorf("%q kind = %s, want %s", sql, statement.Kind, want)
		}
		if !statement.Reads() {
			t.Errorf("%q must read only, got %+v", sql, statement)
		}
	}
}

func TestSQLiteExplainDisarmsRefusedStatements(t *testing.T) {
	for _, sql := range []string{"EXPLAIN VACUUM", "EXPLAIN BEGIN", "EXPLAIN PRAGMA journal_mode"} {
		analysis := SQLite().Analyze(sql)
		if !analysis.Valid() {
			t.Fatalf("%q did not parse: %s", sql, analysis.FirstError())
		}
		statement := analysis.Statements[0]
		if statement.Refusal != "" || !statement.Reads() {
			t.Errorf("EXPLAIN prints the program without running it, so %q only reads: %+v", sql, statement)
		}
	}
}

func TestPostgresExplainAnalyzeKeepsTheHazard(t *testing.T) {
	analysis := PostgreSQL().Analyze("EXPLAIN ANALYZE INSERT INTO t VALUES (1)")
	if !analysis.Valid() {
		t.Fatalf("did not parse: %s", analysis.FirstError())
	}
	statement := analysis.Statements[0]
	if !statement.Mutating || statement.MutatedBy != KindInsert {
		t.Fatalf("statement = %+v", statement)
	}
}

func TestEmptyStatementsAreIgnored(t *testing.T) {
	dialects := map[string]Dialect{"postgres": PostgreSQL(), "sqlite": SQLite()}
	for name, dialect := range dialects {
		for _, sql := range []string{";;;", "", "   "} {
			analysis := dialect.Analyze(sql)
			if len(analysis.Statements) != 0 {
				t.Errorf("%s: %q produced %+v", name, sql, analysis.Statements)
			}
		}
	}
}

func TestAGrammarWithoutRuleNamesIsInert(t *testing.T) {
	base := PostgreSQL().(grammar)
	broken := grammar{
		name:       "broken",
		statements: []string{"stmt"},
		rules:      base.rules,
		parse: func(input antlr.CharStream, listener antlr.ErrorListener) (antlr.Tree, []string) {
			tree, _ := base.parse(input, listener)
			return tree, nil
		},
	}
	analysis := broken.Analyze("SELECT 1")
	if len(analysis.Statements) != 0 {
		t.Fatalf("without rule names nothing can be classified: %+v", analysis.Statements)
	}
}

// A statement knows where it was written, which is what lets one of several be
// sent on its own.
func TestAStatementKnowsWhereItWasWritten(t *testing.T) {
	for _, want := range []struct {
		name   string
		sql    string
		slices []string
	}{
		{"one statement", "SELECT 1", []string{"SELECT 1"}},
		{"leading space", "   SELECT 1", []string{"SELECT 1"}},
		{"two of them", "SELECT 1; SELECT 2", []string{"SELECT 1", "SELECT 2"}},
		{
			name:   "over several lines",
			sql:    "SELECT 1;\nSELECT\n  2",
			slices: []string{"SELECT 1", "SELECT\n  2"},
		},
		{
			name:   "a comment in front",
			sql:    "-- why\nSELECT 1",
			slices: []string{"SELECT 1"},
		},
	} {
		t.Run(want.name, func(t *testing.T) {
			analysis := PostgreSQL().Analyze(want.sql)
			if len(analysis.Statements) != len(want.slices) {
				t.Fatalf("statements = %d, want %d", len(analysis.Statements), len(want.slices))
			}
			for i, statement := range analysis.Statements {
				if got := statement.Slice(want.sql); got != want.slices[i] {
					t.Errorf("statement %d = %q, want %q", i, got, want.slices[i])
				}
			}
		})
	}
}

// A slice that is asked for outside the request it came from is nothing rather
// than a panic.
func TestASliceOutsideTheRequestIsNothing(t *testing.T) {
	for _, statement := range []Statement{
		{Start: -1, Stop: 2},
		{Start: 4, Stop: 2},
		{Start: 0, Stop: 40},
	} {
		if got := statement.Slice("SELECT 1"); got != "" {
			t.Errorf("Slice = %q, want nothing", got)
		}
	}
}

// The statement a position falls in is the one being worked on, and a position
// in the space after one still belongs to it.
func TestTheStatementAPositionFallsIn(t *testing.T) {
	sql := "SELECT 1;\n\nSELECT 2;\n"
	analysis := PostgreSQL().Analyze(sql)
	if len(analysis.Statements) != 2 {
		t.Fatalf("statements = %d", len(analysis.Statements))
	}
	for _, want := range []struct {
		name  string
		at    int
		slice string
	}{
		{"the first character", 0, "SELECT 1"},
		{"inside the first", 3, "SELECT 1"},
		{"the blank line after it", 10, "SELECT 1"},
		{"inside the second", 13, "SELECT 2"},
		{"past the end", 100, "SELECT 2"},
	} {
		t.Run(want.name, func(t *testing.T) {
			statement, ok := analysis.At(want.at)
			if !ok {
				t.Fatal("there is a statement there")
			}
			if got := statement.Slice(sql); got != want.slice {
				t.Errorf("At(%d) = %q, want %q", want.at, got, want.slice)
			}
		})
	}
	if _, ok := (Analysis{}).At(0); ok {
		t.Error("a request with no statements has none at any position")
	}
}

// A statement PostgreSQL will only run on its own is refused by the parser
// rather than sent and rejected by the server, which is what SQLSTATE 25001 was.
func TestPostgresRefusesStatementsThatCannotRunInATransaction(t *testing.T) {
	cases := []struct {
		name    string
		sql     string
		refused bool
	}{
		{"drop a database", "DROP DATABASE bullet", true},
		{"create a database", "CREATE DATABASE bullet", true},
		{"create a tablespace", "CREATE TABLESPACE fast LOCATION '/mnt/fast'", true},
		{"drop a tablespace", "DROP TABLESPACE fast", true},
		{"write the server configuration", "ALTER SYSTEM SET work_mem = '1GB'", true},
		{"build an index concurrently", "CREATE INDEX CONCURRENTLY i ON t (id)", true},
		{"build a unique index concurrently", "CREATE UNIQUE INDEX CONCURRENTLY i ON t (id)", true},
		{"drop an index concurrently", "DROP INDEX CONCURRENTLY i", true},
		{"move a database", "ALTER DATABASE app SET TABLESPACE fast", true},
		{"build an index", "CREATE INDEX i ON t (id)", false},
		{"drop an index", "DROP INDEX i", false},
		{"drop a table", "DROP TABLE users", false},
		{"set a database option", "ALTER DATABASE app WITH CONNECTION LIMIT 10", false},
		{"set a database default", "ALTER DATABASE app SET work_mem = '1GB'", false},
		{"refresh a materialised view concurrently", "REFRESH MATERIALIZED VIEW CONCURRENTLY mv", false},
		{"add a value to an enum", "ALTER TYPE mood ADD VALUE 'happy'", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			analysis := PostgreSQL().Analyze(c.sql)
			if !analysis.Valid() {
				t.Fatalf("the statement must parse: %s", analysis.FirstError())
			}
			if len(analysis.Statements) != 1 {
				t.Fatalf("statements = %d", len(analysis.Statements))
			}
			if refused := analysis.Statements[0].Refusal != ""; refused != c.refused {
				t.Errorf("refused = %v (%q), want %v", refused,
					analysis.Statements[0].Refusal, c.refused)
			}
		})
	}
}

// A refinement is asked about its own rule and about nothing else, or every
// statement holding a keyword would answer for the one statement that means it.
func TestARefinementIsOnlyAskedAboutItsOwnRule(t *testing.T) {
	dialect := PostgreSQL().(grammar)
	for name := range dialect.refinements {
		if _, ok := dialect.lookup(name); !ok {
			t.Errorf("%s refines a rule that nothing classifies", name)
		}
	}
	if _, ok := dialect.refinements["refreshmatviewstmt"]; ok {
		t.Error("REFRESH MATERIALIZED VIEW CONCURRENTLY runs inside a transaction")
	}
}
