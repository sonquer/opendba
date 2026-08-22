package sqldialect

import (
	"strings"
	"testing"

	"github.com/antlr4-go/antlr/v4"
)

func TestDefaultFactoryKnowsBothDialects(t *testing.T) {
	factory := Default()
	names := factory.Names()
	if len(names) != 2 || names[0] != "postgres" || names[1] != "sqlite" {
		t.Fatalf("Names() = %v", names)
	}
	for _, name := range []string{"postgres", "POSTGRES", "SQLite"} {
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
		name:          "empty",
		statementRule: "stmt",
		parse:         PostgreSQL().(grammar).parse,
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
		name:          "broken",
		statementRule: "stmt",
		rules:         base.rules,
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
