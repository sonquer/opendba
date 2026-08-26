package sqlguard

import (
	"strings"
	"testing"

	"github.com/sonquer/opendba/src/cli/pkg/sqldialect"
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
	{"killing a session", "SELECT pg_terminate_backend(42)", sqldialect.KindSelect, Block, Warn},
	{"cancelling a statement", "select PG_CANCEL_BACKEND(42)", sqldialect.KindSelect, Block, Warn},
	{"reloading the configuration", "SELECT pg_reload_conf()", sqldialect.KindSelect, Block, Warn},
	{"moving a sequence", "SELECT setval('users_id_seq', 1)", sqldialect.KindSelect, Block, Warn},
	{"resetting the statistics", "SELECT pg_stat_reset()", sqldialect.KindSelect, Block, Warn},
	{"a column named after a killer", "SELECT pg_terminate_backend FROM audit", sqldialect.KindSelect, Allow, Allow},
	{"a qualified call to a killer", "SELECT pg_catalog.setval('s', 1)", sqldialect.KindSelect, Block, Warn},
	{"a killer with room before its bracket", "SELECT pg_terminate_backend  (42)", sqldialect.KindSelect, Block, Warn},
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
	{"alter database options", "ALTER DATABASE app WITH CONNECTION LIMIT 10", sqldialect.KindAlter, Block, Warn},
	{"alter database settings", "ALTER DATABASE app SET work_mem = '1GB'", sqldialect.KindAlter, Block, Warn},
	{"drop index", "DROP INDEX i", sqldialect.KindDrop, Block, Warn},
	{"refresh a materialised view concurrently", "REFRESH MATERIALIZED VIEW CONCURRENTLY mv", sqldialect.KindCreate, Block, Warn},
	{"add an enum value", "ALTER TYPE mood ADD VALUE 'happy'", sqldialect.KindAlter, Block, Warn},

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

	{"create database", "CREATE DATABASE bullet", sqldialect.KindCreate, Block, Block},
	{"drop database", "DROP DATABASE bullet", sqldialect.KindDrop, Block, Block},
	{"drop database if it is there", "DROP DATABASE IF EXISTS bullet", sqldialect.KindDrop, Block, Block},
	{"create tablespace", "CREATE TABLESPACE fast LOCATION '/mnt/fast'", sqldialect.KindCreate, Block, Block},
	{"drop tablespace", "DROP TABLESPACE fast", sqldialect.KindDrop, Block, Block},
	{"move a database to another tablespace", "ALTER DATABASE app SET TABLESPACE fast", sqldialect.KindAlter, Block, Block},
	{"alter system", "ALTER SYSTEM SET work_mem = '1GB'", sqldialect.KindAlter, Block, Block},
	{"create index concurrently", "CREATE INDEX CONCURRENTLY i ON t (id)", sqldialect.KindCreate, Block, Block},
	{"create unique index concurrently", "CREATE UNIQUE INDEX CONCURRENTLY i ON t (id)", sqldialect.KindCreate, Block, Block},
	{"drop index concurrently", "DROP INDEX CONCURRENTLY i", sqldialect.KindDrop, Block, Block},
	{"reindex concurrently", "REINDEX TABLE CONCURRENTLY users", sqldialect.KindMaintenance, Block, Block},
	{"create subscription", "CREATE SUBSCRIPTION s CONNECTION 'host=x' PUBLICATION p", sqldialect.KindCreate, Block, Block},
	{"refresh a subscription", "ALTER SUBSCRIPTION s REFRESH PUBLICATION", sqldialect.KindAlter, Block, Block},
	{"drop subscription", "DROP SUBSCRIPTION s", sqldialect.KindDrop, Block, Block},

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
	{"vacuum into a file", "VACUUM INTO 'copy.db'", sqldialect.KindMaintenance, Block, Block},
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

var mssqlGolden = []goldenCase{
	{"select", "SELECT 1", sqldialect.KindSelect, Allow, Allow},
	{"select with a row cap", "SELECT TOP (10) id FROM dbo.users", sqldialect.KindSelect, Allow, Allow},
	{"select with a join", "SELECT * FROM users u JOIN orders o ON o.user_id = u.id", sqldialect.KindSelect, Allow, Allow},
	{"read only cte", "WITH x AS (SELECT 1 AS a) SELECT a FROM x", sqldialect.KindSelect, Allow, Allow},
	{"bracket quoted names", "SELECT * FROM [dbo].[users]", sqldialect.KindSelect, Allow, Allow},
	{"a hint that avoids a lock", "SELECT * FROM users WITH (NOLOCK)", sqldialect.KindSelect, Allow, Allow},

	{"select into a new table", "SELECT * INTO copy FROM users", sqldialect.KindSelect, Block, Warn},
	{"a hint that takes a lock", "SELECT * FROM users WITH (UPDLOCK)", sqldialect.KindSelect, Block, Warn},
	{"a legacy hint that takes a lock", "SELECT * FROM users (HOLDLOCK)", sqldialect.KindSelect, Block, Warn},
	{"moving a sequence on", "SELECT NEXT VALUE FOR dbo.seq", sqldialect.KindSelect, Block, Warn},

	{"insert", "INSERT INTO users (email) VALUES ('a')", sqldialect.KindInsert, Block, Warn},
	{"update", "UPDATE users SET active = 0", sqldialect.KindUpdate, Block, Warn},
	{"delete", "DELETE FROM users", sqldialect.KindDelete, Block, Warn},
	{"merge", "MERGE t USING s ON t.id = s.id WHEN MATCHED THEN UPDATE SET t.a = s.a;", sqldialect.KindMerge, Block, Warn},
	{"truncate", "TRUNCATE TABLE users", sqldialect.KindTruncate, Block, Warn},
	{"grant", "GRANT SELECT ON t TO r", sqldialect.KindGrant, Block, Warn},
	{"update statistics", "UPDATE STATISTICS dbo.users", sqldialect.KindMaintenance, Block, Warn},

	{"create table", "CREATE TABLE t (id int)", sqldialect.KindCreate, Block, Warn},
	{"drop table", "DROP TABLE t", sqldialect.KindDrop, Block, Warn},
	{"alter table", "ALTER TABLE t ADD c int", sqldialect.KindAlter, Block, Warn},
	{"create index", "CREATE INDEX ix ON t (a)", sqldialect.KindCreate, Block, Warn},
	{"create view", "CREATE VIEW v AS SELECT 1 AS a", sqldialect.KindCreate, Block, Warn},
	{"create procedure", "CREATE PROCEDURE p AS SELECT 1", sqldialect.KindCreate, Block, Warn},

	{"begin transaction", "BEGIN TRANSACTION", sqldialect.KindTransaction, Block, Block},
	{"commit", "COMMIT", sqldialect.KindTransaction, Block, Block},
	{"rollback", "ROLLBACK", sqldialect.KindTransaction, Block, Block},
	{"set a session option", "SET ANSI_NULLS ON", sqldialect.KindSession, Block, Block},
	{"use another database", "USE app", sqldialect.KindSession, Block, Block},
	{"declare a variable", "DECLARE @x int", sqldialect.KindSession, Block, Block},

	{"execute a procedure", "EXEC sp_who", sqldialect.KindCall, Block, Block},
	{"a bare procedure name", "sp_who", sqldialect.KindCall, Block, Block},
	{"kill a session", "KILL 52", sqldialect.KindMaintenance, Block, Block},
	{"shutdown", "SHUTDOWN", sqldialect.KindMaintenance, Block, Block},
	{"dbcc", "DBCC CHECKDB", sqldialect.KindMaintenance, Block, Block},
	{"checkpoint", "CHECKPOINT", sqldialect.KindMaintenance, Block, Block},
	{"reconfigure", "RECONFIGURE", sqldialect.KindMaintenance, Block, Block},
	{"waitfor", "WAITFOR DELAY '00:00:05'", sqldialect.KindMaintenance, Block, Block},

	{"a block", "BEGIN SELECT 1 END", sqldialect.KindBatch, Block, Block},
	{"a conditional", "IF 1=1 SELECT 1", sqldialect.KindBatch, Block, Block},
	{"a conditional hiding a delete", "IF 1=1 DELETE FROM users", sqldialect.KindBatch, Block, Block},
	{"a loop", "WHILE 1=1 BEGIN SELECT 1 END", sqldialect.KindBatch, Block, Block},
	{"print", "PRINT 'hi'", sqldialect.KindBatch, Block, Block},
	{"the batch separator", "GO", sqldialect.KindBatch, Block, Block},

	{"create database", "CREATE DATABASE app", sqldialect.KindCreate, Block, Block},
	{"alter database", "ALTER DATABASE app SET RECOVERY SIMPLE", sqldialect.KindAlter, Block, Block},
	{"drop database", "DROP DATABASE app", sqldialect.KindDrop, Block, Block},
	{"backup", "BACKUP DATABASE app TO DISK = 'x'", sqldialect.KindCopy, Block, Block},

	{"nonsense", "!!!", sqldialect.KindUnknown, Block, Block},
	{"empty", "", sqldialect.KindUnknown, Block, Block},

	{"restore is not in the grammar", "RESTORE DATABASE app FROM DISK = 'x'", sqldialect.KindUnknown, Block, Block},
	{"bulk insert is not in the grammar", "BULK INSERT t FROM 'f.txt'", sqldialect.KindUnknown, Block, Block},
	{"revoke is not in the grammar", "REVOKE SELECT ON t FROM r", sqldialect.KindUnknown, Block, Block},
	{"deny is not in the grammar", "DENY SELECT ON t TO r", sqldialect.KindUnknown, Block, Block},
}

func TestMSSQLGolden(t *testing.T) {
	guard := New(sqldialect.MSSQL())
	for _, c := range mssqlGolden {
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
		"mssql":    New(sqldialect.MSSQL()),
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
