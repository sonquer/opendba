package sqlguard

import (
	"testing"

	"github.com/sonquer/opendba/src/cli/pkg/sqldialect"
)

func seedGuard(f *testing.F) {
	f.Helper()
	for _, golden := range [][]goldenCase{postgresGolden, sqliteGolden, mssqlGolden} {
		for _, test := range golden {
			f.Add(test.sql)
		}
	}
	f.Add("")
	f.Add(";")
	f.Add(";;;")
	f.Add("SELECT 1; DROP TABLE users")
	f.Add("\x00")
}

// FuzzClassifyKeepsReadOnlyReadOnly is the property the whole safety layer rests
// on: whatever is typed, a statement allowed in read only mode reads and nothing
// more, and it is exactly one statement.
func FuzzClassifyKeepsReadOnlyReadOnly(f *testing.F) {
	seedGuard(f)
	guards := map[string]Guard{
		"postgresql": New(sqldialect.PostgreSQL()),
		"sqlite":     New(sqldialect.SQLite()),
		"mssql":      New(sqldialect.MSSQL()),
	}
	f.Fuzz(func(t *testing.T, sql string) {
		for name, guard := range guards {
			result := guard.Classify(sql, ModeReadOnly)
			if !result.Allowed() {
				continue
			}
			if result.Statements != 1 {
				t.Errorf("%s allowed %d statements for %q", name, result.Statements, sql)
			}
			analysis := guard.dialect.Analyze(sql)
			if len(analysis.Statements) != 1 || !analysis.Statements[0].Reads() {
				t.Errorf("%s allowed a statement that does not only read: %q", name, sql)
			}
		}
	})
}

// FuzzClassifyRefusesMoreThanOneStatement holds the rule that multi statement
// input is refused in every mode, which is what stops a second statement being
// smuggled past a verdict given for the first.
func FuzzClassifyRefusesMoreThanOneStatement(f *testing.F) {
	seedGuard(f)
	guard := New(sqldialect.PostgreSQL())
	f.Fuzz(func(t *testing.T, sql string) {
		for _, mode := range []Mode{ModeReadOnly, ModeReadWrite} {
			result := guard.Classify(sql, mode)
			if result.Statements > 1 && !result.Blocked() {
				t.Errorf("%s let %d statements through: %q", mode, result.Statements, sql)
			}
		}
	})
}
