package sqlguard

import (
	"testing"

	"github.com/sonquer/tui4db/src/cli/pkg/sqldialect"
)

func TestReadmeSpotTheWrite(t *testing.T) {
	guard := New(sqldialect.PostgreSQL())
	cases := map[string]string{
		"A": "SELECT * FROM users FOR UPDATE;",
		"B": "WITH removed AS (DELETE FROM users WHERE last_login < now() - interval '1 year' RETURNING *)\nSELECT count(*) FROM removed;",
		"C": "EXPLAIN ANALYZE DELETE FROM sessions WHERE expired;",
		"D": "/* SELECT */ SELECT * INTO backup FROM orders;",
	}
	for name, sql := range cases {
		result := guard.Classify(sql, ModeReadOnly)
		t.Logf("%s: %v (%s) kind=%s", name, result.Verdict, result.Reason, result.Kind)
		if !result.Blocked() {
			t.Errorf("%s must be blocked in read only mode, got %v", name, result.Verdict)
		}
	}
}
