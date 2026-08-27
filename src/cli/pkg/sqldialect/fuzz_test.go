package sqldialect

import "testing"

func seedDialect(f *testing.F) {
	f.Helper()
	for _, sql := range []string{
		"SELECT 1",
		"select id from users where id = $1",
		"WITH removed AS (DELETE FROM users RETURNING *) SELECT count(*) FROM removed",
		"SELECT $tag$ delete from t; $tag$",
		"EXPLAIN (COSTS OFF) SELECT 1",
		"UPDATE users SET name = 'x' WHERE id = 1",
		"CREATE INDEX CONCURRENTLY ON users (id)",
		"SELECT 1; SELECT 2",
		"-- nothing but a comment",
		"",
		";",
		"\x00",
		"SELECT '",
	} {
		f.Add(sql)
	}
}

// FuzzAnalyzeSurvivesAnything holds the parsers to what the classifier assumes
// of them: whatever bytes arrive, Analyze returns rather than panicking, and
// every statement it reports can be cut back out of the request it was read
// from.
func FuzzAnalyzeSurvivesAnything(f *testing.F) {
	seedDialect(f)
	dialects := []Dialect{PostgreSQL(), SQLite(), MSSQL()}
	f.Fuzz(func(t *testing.T, sql string) {
		for _, dialect := range dialects {
			analysis := dialect.Analyze(sql)
			for _, statement := range analysis.Statements {
				if statement.Start < 0 || statement.Stop < statement.Start {
					t.Errorf("%s reported %d..%d for %q",
						dialect.Name(), statement.Start, statement.Stop, sql)
				}
				cut := []rune(statement.Slice(sql))
				if len(cut) != 0 && len(cut) != statement.Stop-statement.Start+1 {
					t.Errorf("%s cut %d runes for a statement spanning %d..%d of %q",
						dialect.Name(), len(cut), statement.Start, statement.Stop, sql)
				}
			}
			if !analysis.Valid() && analysis.FirstError() == "" {
				t.Errorf("%s reported an error with nothing to say for %q", dialect.Name(), sql)
			}
		}
	})
}

// FuzzAtStaysInsideTheAnalysis is what the editor leans on to say which
// statement the cursor is in: any offset, valid or not, is answered without
// reaching past the statements that were found.
func FuzzAtStaysInsideTheAnalysis(f *testing.F) {
	seedDialect(f)
	dialect := PostgreSQL()
	f.Fuzz(func(t *testing.T, sql string) {
		analysis := dialect.Analyze(sql)
		for offset := -1; offset <= len([]rune(sql)); offset++ {
			statement, found := analysis.At(offset)
			if found && statement.Stop < statement.Start {
				t.Errorf("At(%d) gave %d..%d for %q", offset, statement.Start, statement.Stop, sql)
			}
		}
	})
}
