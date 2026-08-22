package sqlguard

import (
	"strings"
	"testing"

	"github.com/sonquer/tui4db/src/cli/pkg/sqldialect"
)

type goldenCase struct {
	name      string
	sql       string
	kind      sqldialect.Kind
	readOnly  Verdict
	readWrite Verdict
}

var postgresGolden = []goldenCase{
	{"plain select", "SELECT 1", sqldialect.KindSelect, Allow, Allow},
	{"lower case select", "select id from users", sqldialect.KindSelect, Allow, Allow},
	{"select with semicolon", "SELECT 1;", sqldialect.KindSelect, Allow, Allow},
	{"select with line comment", "-- comment\nSELECT 1", sqldialect.KindSelect, Allow, Allow},
	{"select with block comment", "SELECT /* here */ 1", sqldialect.KindSelect, Allow, Allow},
	{"select with semicolon in a literal", "SELECT ';'", sqldialect.KindSelect, Allow, Allow},
	{"select with dollar quoted literal", "SELECT $tag$ delete from t; $tag$", sqldialect.KindSelect, Allow, Allow},
	{"select from a table named like a keyword", `SELECT * FROM "select"`, sqldialect.KindSelect, Allow, Allow},
	{"select with a placeholder", "SELECT * FROM t WHERE id = $1", sqldialect.KindSelect, Allow, Allow},
	{"join", "SELECT u.id FROM users u LEFT JOIN orders o ON o.user_id = u.id", sqldialect.KindSelect, Allow, Allow},
	{"read only cte", "WITH recent AS (SELECT 1) SELECT * FROM recent", sqldialect.KindSelect, Allow, Allow},
	{"union", "SELECT 1 UNION SELECT 2", sqldialect.KindSelect, Allow, Allow},
	{"values", "VALUES (1), (2)", sqldialect.KindSelect, Allow, Allow},
	{"show", "SHOW work_mem", sqldialect.KindShow, Allow, Allow},
	{"explain", "EXPLAIN SELECT 1", sqldialect.KindExplain, Allow, Allow},
	{"explain with options", "EXPLAIN (COSTS OFF, VERBOSE) SELECT 1", sqldialect.KindExplain, Allow, Allow},
	{"explain analyze select", "EXPLAIN ANALYZE SELECT 1", sqldialect.KindExplain, Allow, Allow},
	{"explain a delete without running it", "EXPLAIN DELETE FROM users", sqldialect.KindExplain, Allow, Allow},

	{"insert", "INSERT INTO users (email) VALUES ('a@b.c')", sqldialect.KindInsert, Block, Warn},
	{"update", "UPDATE users SET active = false", sqldialect.KindUpdate, Block, Warn},
	{"delete", "DELETE FROM users WHERE id = 1", sqldialect.KindDelete, Block, Warn},
	{"truncate", "TRUNCATE users", sqldialect.KindTruncate, Block, Warn},
	{"create table", "CREATE TABLE t (id int)", sqldialect.KindCreate, Block, Warn},
	{"create index", "CREATE INDEX i ON t (id)", sqldialect.KindCreate, Block, Warn},
	{"alter table", "ALTER TABLE users ADD COLUMN x int", sqldialect.KindAlter, Block, Warn},
	{"drop table", "DROP TABLE users", sqldialect.KindDrop, Block, Warn},
	{"grant", "GRANT SELECT ON users TO readonly", sqldialect.KindGrant, Block, Warn},
	{"revoke", "REVOKE SELECT ON users FROM readonly", sqldialect.KindRevoke, Block, Warn},
	{"comment on", "COMMENT ON TABLE users IS 'x'", sqldialect.KindAlter, Block, Warn},
	{"create view", "CREATE VIEW v AS SELECT 1", sqldialect.KindCreate, Block, Warn},
	{"create table as", "CREATE TABLE t AS SELECT 1", sqldialect.KindCreate, Block, Warn},
	{"create role", "CREATE ROLE intruder", sqldialect.KindCreate, Block, Warn},
	{"alter system", "ALTER SYSTEM SET work_mem = '1GB'", sqldialect.KindAlter, Block, Warn},

	{"delete hidden in a cte", "WITH removed AS (DELETE FROM users RETURNING *) SELECT * FROM removed", sqldialect.KindSelect, Block, Warn},
	{"insert hidden in a cte", "WITH added AS (INSERT INTO t VALUES (1) RETURNING *) SELECT * FROM added", sqldialect.KindSelect, Block, Warn},
	{"update hidden in a cte", "WITH bumped AS (UPDATE t SET x = 1 RETURNING *) SELECT * FROM bumped", sqldialect.KindSelect, Block, Warn},
	{"select into", "SELECT * INTO backup FROM users", sqldialect.KindSelect, Block, Warn},
	{"select for update", "SELECT * FROM users FOR UPDATE", sqldialect.KindSelect, Block, Warn},
	{"select for no key update", "SELECT * FROM users FOR NO KEY UPDATE", sqldialect.KindSelect, Block, Warn},
	{"select for share", "SELECT * FROM users FOR SHARE", sqldialect.KindSelect, Block, Warn},
	{"delete disguised by a comment", "/* SELECT */ DELETE FROM users", sqldialect.KindDelete, Block, Warn},
	{"explain analyze runs the delete", "EXPLAIN ANALYZE DELETE FROM users", sqldialect.KindExplain, Block, Warn},

	{"copy from a file", "COPY users FROM '/tmp/users.csv'", sqldialect.KindCopy, Block, Block},
	{"copy to stdout", "COPY users TO STDOUT", sqldialect.KindCopy, Block, Block},
	{"anonymous block", "DO $$ BEGIN PERFORM 1; END $$", sqldialect.KindCall, Block, Block},
	{"call a procedure", "CALL do_work()", sqldialect.KindCall, Block, Block},
	{"set a session variable", "SET search_path TO public", sqldialect.KindSession, Block, Block},
	{"set role", "SET ROLE admin", sqldialect.KindSession, Block, Block},
	{"begin", "BEGIN", sqldialect.KindTransaction, Block, Block},
	{"commit", "COMMIT", sqldialect.KindTransaction, Block, Block},
	{"rollback", "ROLLBACK", sqldialect.KindTransaction, Block, Block},
	{"lock table", "LOCK TABLE users", sqldialect.KindMaintenance, Block, Block},
	{"vacuum", "VACUUM users", sqldialect.KindMaintenance, Block, Block},
	{"cluster", "CLUSTER users USING users_pkey", sqldialect.KindMaintenance, Block, Block},
	{"reindex", "REINDEX TABLE users", sqldialect.KindMaintenance, Block, Block},
	{"checkpoint", "CHECKPOINT", sqldialect.KindMaintenance, Block, Block},
	{"declare a cursor", "DECLARE c CURSOR FOR SELECT 1", sqldialect.KindCursor, Block, Block},
	{"fetch", "FETCH ALL FROM c", sqldialect.KindCursor, Block, Block},
	{"prepare", "PREPARE p AS SELECT 1", sqldialect.KindPrepared, Block, Block},
	{"execute", "EXECUTE p", sqldialect.KindPrepared, Block, Block},
	{"listen", "LISTEN channel", sqldialect.KindSession, Block, Block},
	{"notify", "NOTIFY channel", sqldialect.KindSession, Block, Block},
	{"discard all", "DISCARD ALL", sqldialect.KindSession, Block, Block},

	{"empty input", "", sqldialect.KindUnknown, Block, Block},
	{"whitespace only", "   \n\t", sqldialect.KindUnknown, Block, Block},
	{"comment only", "-- nothing here", sqldialect.KindUnknown, Block, Block},
	{"semicolon only", ";", sqldialect.KindUnknown, Block, Block},
	{"nonsense", "FROBNICATE users", sqldialect.KindUnknown, Block, Block},
	{"incomplete statement", "SELECT * FROM", sqldialect.KindUnknown, Block, Block},
}

func TestPostgresGolden(t *testing.T) {
	guard := New(sqldialect.PostgreSQL())
	for _, c := range postgresGolden {
		t.Run(c.name, func(t *testing.T) {
			readOnly := guard.Classify(c.sql, ModeReadOnly)
			if readOnly.Verdict != c.readOnly {
				t.Errorf("read only verdict = %v (%s), want %v", readOnly.Verdict, readOnly.Reason, c.readOnly)
			}
			if readOnly.Kind != c.kind {
				t.Errorf("kind = %v, want %v", readOnly.Kind, c.kind)
			}
			if readOnly.Reason == "" {
				t.Error("every verdict must explain itself")
			}
			readWrite := guard.Classify(c.sql, ModeReadWrite)
			if readWrite.Verdict != c.readWrite {
				t.Errorf("read write verdict = %v (%s), want %v", readWrite.Verdict, readWrite.Reason, c.readWrite)
			}
		})
	}
}

var sqliteGolden = []goldenCase{
	{"select", "SELECT 1", sqldialect.KindSelect, Allow, Allow},
	{"select from a table", "SELECT id FROM users WHERE active = 1", sqldialect.KindSelect, Allow, Allow},
	{"read only cte", "WITH x AS (SELECT 1) SELECT * FROM x", sqldialect.KindSelect, Allow, Allow},
	{"explain", "EXPLAIN SELECT 1", sqldialect.KindExplain, Allow, Allow},
	{"explain query plan", "EXPLAIN QUERY PLAN SELECT 1", sqldialect.KindExplain, Allow, Allow},
	{"insert", "INSERT INTO users (email) VALUES ('a')", sqldialect.KindInsert, Block, Warn},
	{"update", "UPDATE users SET active = 0", sqldialect.KindUpdate, Block, Warn},
	{"delete", "DELETE FROM users", sqldialect.KindDelete, Block, Warn},
	{"create table", "CREATE TABLE t (id integer)", sqldialect.KindCreate, Block, Warn},
	{"drop table", "DROP TABLE t", sqldialect.KindDrop, Block, Warn},
	{"alter table", "ALTER TABLE t RENAME TO t2", sqldialect.KindAlter, Block, Warn},
	{"pragma", "PRAGMA journal_mode", sqldialect.KindPragma, Block, Block},
	{"attach", "ATTACH DATABASE 'other.db' AS other", sqldialect.KindSession, Block, Block},
	{"detach", "DETACH DATABASE other", sqldialect.KindSession, Block, Block},
	{"begin", "BEGIN", sqldialect.KindTransaction, Block, Block},
	{"commit", "COMMIT", sqldialect.KindTransaction, Block, Block},
	{"vacuum", "VACUUM", sqldialect.KindMaintenance, Block, Block},
	{"reindex", "REINDEX", sqldialect.KindMaintenance, Block, Block},
	{"nonsense", "NONSENSE", sqldialect.KindUnknown, Block, Block},
	{"empty", "", sqldialect.KindUnknown, Block, Block},
}

func TestSQLiteGolden(t *testing.T) {
	guard := New(sqldialect.SQLite())
	for _, c := range sqliteGolden {
		t.Run(c.name, func(t *testing.T) {
			readOnly := guard.Classify(c.sql, ModeReadOnly)
			if readOnly.Verdict != c.readOnly {
				t.Errorf("read only verdict = %v (%s), want %v", readOnly.Verdict, readOnly.Reason, c.readOnly)
			}
			if readOnly.Kind != c.kind {
				t.Errorf("kind = %v, want %v", readOnly.Kind, c.kind)
			}
			readWrite := guard.Classify(c.sql, ModeReadWrite)
			if readWrite.Verdict != c.readWrite {
				t.Errorf("read write verdict = %v (%s), want %v", readWrite.Verdict, readWrite.Reason, c.readWrite)
			}
		})
	}
}

func TestMultipleStatementsAreAlwaysBlocked(t *testing.T) {
	guards := map[string]Guard{
		"postgres": New(sqldialect.PostgreSQL()),
		"sqlite":   New(sqldialect.SQLite()),
	}
	statements := []string{
		"SELECT 1; SELECT 2",
		"SELECT 1; DELETE FROM users",
		"SELECT 1;\nUPDATE users SET x = 1;",
	}
	for name, guard := range guards {
		for _, sql := range statements {
			for _, mode := range []Mode{ModeReadOnly, ModeReadWrite} {
				result := guard.Classify(sql, mode)
				if !result.Blocked() {
					t.Errorf("%s: Classify(%q, %v) = %v", name, sql, mode, result.Verdict)
				}
				if result.Statements < 2 {
					t.Errorf("%s: Classify(%q) counted %d statements", name, sql, result.Statements)
				}
				if !strings.Contains(result.Reason, "never executed together") {
					t.Errorf("%s: reason = %q", name, result.Reason)
				}
			}
		}
	}
}

func TestResultHelpers(t *testing.T) {
	guard := New(sqldialect.PostgreSQL())
	allowed := guard.Classify("SELECT 1", ModeReadOnly)
	if !allowed.Allowed() || allowed.Blocked() || allowed.NeedsConfirmation() {
		t.Errorf("SELECT should be allowed: %+v", allowed)
	}
	if allowed.Statements != 1 {
		t.Errorf("Statements = %d", allowed.Statements)
	}
	warned := guard.Classify("DELETE FROM t", ModeReadWrite)
	if !warned.NeedsConfirmation() {
		t.Errorf("DELETE in read write should need confirmation: %+v", warned)
	}
	blocked := guard.Classify("DELETE FROM t", ModeReadOnly)
	if !blocked.Blocked() || !strings.Contains(blocked.Reason, "READ ONLY") {
		t.Errorf("DELETE in read only should be blocked: %+v", blocked)
	}
}

func TestModeLabels(t *testing.T) {
	if ModeReadOnly.Label() != "READ ONLY" || ModeReadWrite.Label() != "READ / WRITE" {
		t.Error("mode labels are shown to the user")
	}
	if ModeReadOnly.Writable() || !ModeReadWrite.Writable() {
		t.Error("only read write mode is writable")
	}
	if Mode("nonsense").Writable() || Mode("nonsense").Label() != "READ ONLY" {
		t.Error("an unknown mode must fall back to the safe behaviour")
	}
}

func TestGuardReportsItsDialect(t *testing.T) {
	if got := New(sqldialect.PostgreSQL()).Dialect(); got != "postgres" {
		t.Errorf("Dialect() = %q", got)
	}
}
