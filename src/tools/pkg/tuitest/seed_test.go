package tuitest

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func TestSeedBuildsTheDatabaseTheStatementsDescribe(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "core.sql")
	statements := "CREATE TABLE t (id integer primary key);\nINSERT INTO t (id) VALUES (1), (2);"
	if err := os.WriteFile(script, []byte(statements), 0o600); err != nil {
		t.Fatalf("write = %v", err)
	}
	path := filepath.Join(dir, "core.db")
	if err := Seed(path, script); err != nil {
		t.Fatalf("Seed = %v", err)
	}
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open = %v", err)
	}
	defer func() { _ = database.Close() }()
	var rows int
	if err := database.QueryRow("SELECT count(*) FROM t").Scan(&rows); err != nil {
		t.Fatalf("count = %v", err)
	}
	if rows != 2 {
		t.Errorf("the seed put %d rows in", rows)
	}
}

func TestSeedReplacesWhatWasThereBefore(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "core.sql")
	if err := os.WriteFile(script, []byte("CREATE TABLE t (id integer);"), 0o600); err != nil {
		t.Fatalf("write = %v", err)
	}
	path := filepath.Join(dir, "core.db")
	if err := Seed(path, script); err != nil {
		t.Fatalf("Seed = %v", err)
	}
	if err := Seed(path, script); err != nil {
		t.Errorf("Seed over an existing database = %v", err)
	}
}

func TestSeedReportsWhatWentWrong(t *testing.T) {
	dir := t.TempDir()
	if err := Seed(filepath.Join(dir, "core.db"), filepath.Join(dir, "missing.sql")); err == nil {
		t.Error("a seed that is not there was run")
	}
	script := filepath.Join(dir, "bad.sql")
	if err := os.WriteFile(script, []byte("CREATE NONSENSE;"), 0o600); err != nil {
		t.Fatalf("write = %v", err)
	}
	if err := Seed(filepath.Join(dir, "core.db"), script); err == nil {
		t.Error("a seed that is not SQL was run")
	}
}
