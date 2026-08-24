package sqlite

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sonquer/opendba/src/cli/internal/driver"
	"github.com/sonquer/opendba/src/cli/pkg/sqlguard"
)

const fixture = `
CREATE TABLE users (
	id integer PRIMARY KEY,
	email text NOT NULL UNIQUE,
	active integer NOT NULL DEFAULT 1
);
CREATE TABLE orders (
	id integer PRIMARY KEY,
	user_id integer NOT NULL REFERENCES users (id) ON DELETE CASCADE,
	total real NOT NULL DEFAULT 0
);
CREATE INDEX orders_user_id_idx ON orders (user_id);
CREATE VIEW active_users AS SELECT id, email FROM users WHERE active = 1;
INSERT INTO users (id, email) VALUES (1, 'a@example.com'), (2, 'b@example.com');
INSERT INTO orders (id, user_id, total) VALUES (1, 1, 10.5), (2, 1, 20.0);
`

func seeded(t *testing.T) driver.Config {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fixture.db")
	config := driver.Config{File: path, Mode: sqlguard.ModeReadWrite, Timeouts: driver.DefaultTimeouts()}

	conn, err := New().Connect(context.Background(), config)
	if err != nil {
		t.Fatalf("connect for seeding: %v", err)
	}
	writable, ok := conn.(*connection)
	if !ok {
		t.Fatal("unexpected connection type")
	}
	for _, statement := range strings.Split(fixture, ";") {
		if strings.TrimSpace(statement) == "" {
			continue
		}
		if _, err := writable.db.ExecContext(context.Background(), statement); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	config.Mode = sqlguard.ModeReadOnly
	return config
}

func open(t *testing.T, config driver.Config) driver.Conn {
	t.Helper()
	conn, err := New().Connect(context.Background(), config)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func TestDriverIdentity(t *testing.T) {
	d := New()
	if d.Name() != Name || d.Title() != "SQLite" {
		t.Errorf("identity = %q %q", d.Name(), d.Title())
	}
	caps := d.Capabilities()
	if !caps.Explain || !caps.Relations || !caps.Health || !caps.ReadOnlySession {
		t.Errorf("capabilities = %+v", caps)
	}
	if caps.IndexStats {
		t.Error("sqlite does not report index usage statistics")
	}
}

func TestDSN(t *testing.T) {
	readOnly, err := DSN(driver.Config{File: "/tmp/db.sqlite", Mode: sqlguard.ModeReadOnly, Timeouts: driver.DefaultTimeouts()})
	if err != nil {
		t.Fatalf("DSN: %v", err)
	}
	for _, want := range []string{"file:/tmp/db.sqlite", "mode=ro", "query_only%281%29", "foreign_keys%281%29", "busy_timeout%282000%29"} {
		if !strings.Contains(readOnly, want) {
			t.Errorf("read only dsn missing %q: %s", want, readOnly)
		}
	}
	writable, err := DSN(driver.Config{File: "/tmp/db.sqlite", Mode: sqlguard.ModeReadWrite})
	if err != nil {
		t.Fatalf("DSN: %v", err)
	}
	if strings.Contains(writable, "mode=ro") || strings.Contains(writable, "query_only") {
		t.Errorf("a writable connection must not be pinned read only: %s", writable)
	}
	uri, err := DSN(driver.Config{File: "file:memory.db?cache=shared", Mode: sqlguard.ModeReadWrite})
	if err != nil {
		t.Fatalf("DSN: %v", err)
	}
	if !strings.HasPrefix(uri, "file:memory.db?cache=shared&") {
		t.Errorf("an existing uri must be extended, not replaced: %s", uri)
	}
	if _, err := DSN(driver.Config{}); err == nil {
		t.Error("a sqlite connection needs a file")
	}
}

func TestConnectFailsOnAMissingFileInReadOnlyMode(t *testing.T) {
	config := driver.Config{File: filepath.Join(t.TempDir(), "missing.db"), Mode: sqlguard.ModeReadOnly}
	if _, err := New().Connect(context.Background(), config); err == nil {
		t.Fatal("opening a missing database read only must fail")
	}
	if _, err := New().Connect(context.Background(), driver.Config{}); err == nil {
		t.Fatal("a connection without a file must fail")
	}
}

func TestInfo(t *testing.T) {
	conn := open(t, seeded(t))
	info, err := conn.Info(context.Background())
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if info.Driver != Name || info.Version == "" {
		t.Fatalf("info = %+v", info)
	}
	if !info.ReadOnly || info.CanWrite {
		t.Errorf("a read only connection must say so: %+v", info)
	}
	if info.ConnectedAt.After(time.Now()) {
		t.Error("the connection time must not be in the future")
	}
}

func TestReadOnlyConnectionRefusesWrites(t *testing.T) {
	conn := open(t, seeded(t))
	if _, err := conn.Query(context.Background(), "DELETE FROM users"); err == nil {
		t.Fatal("the server itself must refuse the write, not only the classifier")
	}
}

func TestDatabases(t *testing.T) {
	config := seeded(t)
	conn := open(t, config)
	databases, err := conn.Databases(context.Background())
	if err != nil {
		t.Fatalf("Databases: %v", err)
	}
	if len(databases) != 1 || databases[0].Name != "main" || !databases[0].Current {
		t.Fatalf("databases = %+v", databases)
	}
	if !strings.HasSuffix(databases[0].Comment, "fixture.db") {
		t.Errorf("the file backing a database is worth showing: %q", databases[0].Comment)
	}
}

func TestSchemas(t *testing.T) {
	conn := open(t, seeded(t))
	schemas, err := conn.Schemas(context.Background())
	if err != nil {
		t.Fatalf("Schemas: %v", err)
	}
	if len(schemas) != 1 || schemas[0].Name != "main" {
		t.Fatalf("schemas = %+v", schemas)
	}
	if schemas[0].Tables != 3 {
		t.Errorf("main holds two tables and a view, got %d", schemas[0].Tables)
	}
}

func TestTables(t *testing.T) {
	conn := open(t, seeded(t))
	tables, err := conn.Tables(context.Background(), "")
	if err != nil {
		t.Fatalf("Tables: %v", err)
	}
	byName := map[string]driver.Table{}
	for _, table := range tables {
		byName[table.Name] = table
	}
	if len(tables) != 3 {
		t.Fatalf("tables = %+v", tables)
	}
	if byName["users"].Kind != "table" || byName["active_users"].Kind != "view" {
		t.Errorf("kinds = %+v", tables)
	}
	if got := byName["users"].Qualified(); got != "main.users" {
		t.Errorf("Qualified() = %q", got)
	}
}

func TestColumns(t *testing.T) {
	conn := open(t, seeded(t))
	columns, err := conn.Columns(context.Background(), "main", "orders")
	if err != nil {
		t.Fatalf("Columns: %v", err)
	}
	if len(columns) != 3 {
		t.Fatalf("columns = %+v", columns)
	}
	if !columns[0].PrimaryKey || columns[0].Name != "id" {
		t.Errorf("first column = %+v", columns[0])
	}
	if columns[1].ForeignKey != "users.id" {
		t.Errorf("the foreign key must be reported: %+v", columns[1])
	}
	if columns[1].Nullable {
		t.Errorf("a not null column must not be nullable: %+v", columns[1])
	}
	if columns[2].Default != "0" {
		t.Errorf("the default must be reported: %+v", columns[2])
	}
}

func TestRelations(t *testing.T) {
	conn := open(t, seeded(t))
	relations, err := conn.Relations(context.Background(), "main", "users")
	if err != nil {
		t.Fatalf("Relations: %v", err)
	}
	if len(relations) != 1 {
		t.Fatalf("relations = %+v", relations)
	}
	inbound := relations[0]
	if !inbound.Inbound || inbound.FromTable != "orders" || inbound.ToTable != "users" {
		t.Fatalf("inbound relation = %+v", inbound)
	}
	if inbound.OnDelete != "CASCADE" {
		t.Errorf("on delete = %q", inbound.OnDelete)
	}

	outbound, err := conn.Relations(context.Background(), "main", "orders")
	if err != nil {
		t.Fatalf("Relations: %v", err)
	}
	if len(outbound) != 1 || outbound[0].Inbound {
		t.Fatalf("outbound relation = %+v", outbound)
	}
	if len(outbound[0].FromColumns) != 1 || outbound[0].FromColumns[0] != "user_id" {
		t.Errorf("columns = %+v", outbound[0])
	}
}

func TestIndexes(t *testing.T) {
	conn := open(t, seeded(t))
	indexes, err := conn.Indexes(context.Background(), "main")
	if err != nil {
		t.Fatalf("Indexes: %v", err)
	}
	byName := map[string]driver.Index{}
	for _, index := range indexes {
		byName[index.Name] = index
	}
	explicit, ok := byName["orders_user_id_idx"]
	if !ok {
		t.Fatalf("indexes = %+v", indexes)
	}
	if explicit.Table != "orders" || explicit.Unique {
		t.Errorf("index = %+v", explicit)
	}
	if !strings.Contains(explicit.Definition, "CREATE INDEX") {
		t.Errorf("definition = %q", explicit.Definition)
	}
	if explicit.Scans != -1 || explicit.Size != -1 {
		t.Error("sqlite cannot report index size or usage, and must say so rather than report zero")
	}
	unique := false
	for _, index := range indexes {
		if index.Table == "users" && index.Unique {
			unique = true
		}
	}
	if !unique {
		t.Error("the unique constraint on users.email must appear as an index")
	}
}

func TestQuery(t *testing.T) {
	conn := open(t, seeded(t))
	result, err := conn.Query(context.Background(), "SELECT id, email FROM users ORDER BY id")
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	defer result.Close()

	if got := result.Columns(); len(got) != 2 || got[0] != "id" || got[1] != "email" {
		t.Fatalf("columns = %v", got)
	}
	rows := 0
	for result.Next() {
		values := result.Values()
		if len(values) != 2 {
			t.Fatalf("values = %v", values)
		}
		rows++
	}
	if err := result.Err(); err != nil {
		t.Fatalf("Err: %v", err)
	}
	if rows != 2 {
		t.Errorf("rows = %d", rows)
	}
	if result.Truncated() {
		t.Error("a small result must not be truncated")
	}
	if result.Duration() <= 0 {
		t.Error("the query must be timed")
	}
}

func TestQueryStopsAtTheRowLimit(t *testing.T) {
	config := seeded(t)
	config.RowLimit = 1
	conn := open(t, config)

	result, err := conn.Query(context.Background(), "SELECT id FROM users ORDER BY id")
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	defer result.Close()

	rows := 0
	for result.Next() {
		rows++
	}
	if rows != 1 {
		t.Fatalf("rows = %d, want 1", rows)
	}
	if !result.Truncated() {
		t.Error("a result cut short must say so")
	}
}

func TestQueryReportsErrors(t *testing.T) {
	conn := open(t, seeded(t))
	if _, err := conn.Query(context.Background(), "SELECT * FROM missing"); err == nil {
		t.Fatal("want an error for an unknown table")
	}
}

func TestExplain(t *testing.T) {
	conn := open(t, seeded(t))
	plan, err := conn.Explain(context.Background(), "SELECT * FROM orders WHERE user_id = 1", false)
	if err != nil {
		t.Fatalf("Explain: %v", err)
	}
	if len(plan.Root.Children) == 0 {
		t.Fatalf("plan = %+v", plan)
	}
	if !strings.Contains(plan.Text, "orders") {
		t.Errorf("plan text = %q", plan.Text)
	}
	if plan.Root.Children[0].Name == "" {
		t.Error("plan nodes must be named")
	}
	if _, err := conn.Explain(context.Background(), "SELECT * FROM missing", false); err == nil {
		t.Error("want an error for an unknown table")
	}
}

func TestHealth(t *testing.T) {
	conn := open(t, seeded(t))
	findings, err := conn.Health(context.Background())
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	if len(findings) != 6 {
		t.Fatalf("findings = %+v", findings)
	}
	byCode := map[string]driver.Finding{}
	for _, finding := range findings {
		byCode[finding.Code] = finding
	}
	if byCode["integrity_check"].Severity != driver.SeverityOK {
		t.Errorf("integrity = %+v", byCode["integrity_check"])
	}
	if byCode["query_only"].Value != "read only" {
		t.Errorf("a read only session must be reported: %+v", byCode["query_only"])
	}
	if byCode["database_size"].Value == "" {
		t.Error("the size must be reported")
	}
	if byCode["foreign_key_check"].Value != "0" {
		t.Errorf("foreign keys = %+v", byCode["foreign_key_check"])
	}
}

func TestHealthOnAWritableConnection(t *testing.T) {
	config := seeded(t)
	config.Mode = sqlguard.ModeReadWrite
	conn := open(t, config)

	findings, err := conn.Health(context.Background())
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	for _, finding := range findings {
		if finding.Code == "query_only" && finding.Severity != driver.SeverityWarn {
			t.Errorf("a writable session must be flagged: %+v", finding)
		}
	}
}

func TestByteSize(t *testing.T) {
	cases := map[int64]string{
		512:             "512 B",
		2048:            "2.0 KiB",
		5 * 1024 * 1024: "5.0 MiB",
	}
	for bytes, want := range cases {
		if got := ByteSize(bytes); got != want {
			t.Errorf("ByteSize(%d) = %q, want %q", bytes, got, want)
		}
	}
}

func TestQuoteEscapesIdentifiers(t *testing.T) {
	if got := quote(`we"ird`); got != `"we""ird"` {
		t.Errorf("quote() = %q", got)
	}
	if orMain("") != "main" || orMain("temp") != "temp" {
		t.Error("an empty schema must default to main")
	}
}

func TestEveryCallReportsAClosedDatabase(t *testing.T) {
	conn, err := New().Connect(context.Background(), seeded(t))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	calls := map[string]func() error{
		"Info":      func() error { _, err := conn.Info(ctx); return err },
		"Databases": func() error { _, err := conn.Databases(ctx); return err },
		"Schemas":   func() error { _, err := conn.Schemas(ctx); return err },
		"Tables":    func() error { _, err := conn.Tables(ctx, "main"); return err },
		"Columns":   func() error { _, err := conn.Columns(ctx, "main", "users"); return err },
		"Relations": func() error { _, err := conn.Relations(ctx, "main", "users"); return err },
		"Indexes":   func() error { _, err := conn.Indexes(ctx, "main"); return err },
		"Query":     func() error { _, err := conn.Query(ctx, "SELECT 1"); return err },
		"Explain":   func() error { _, err := conn.Explain(ctx, "SELECT 1", false); return err },
		"Health":    func() error { _, err := conn.Health(ctx); return err },
	}
	for name, call := range calls {
		if err := call(); err == nil {
			t.Errorf("%s must report the closed database", name)
		}
	}
	if err := conn.Close(); err == nil {
		t.Log("closing twice is tolerated")
	}
}

func TestHealthWarnsOnBrokenReferences(t *testing.T) {
	path := filepath.Join(t.TempDir(), "broken.db")
	config := driver.Config{File: path, Mode: sqlguard.ModeReadWrite}
	conn, err := New().Connect(context.Background(), config)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	writable := conn.(*connection)
	ctx := context.Background()
	for _, statement := range []string{
		"PRAGMA foreign_keys = OFF",
		"CREATE TABLE users (id integer PRIMARY KEY)",
		"CREATE TABLE orders (id integer PRIMARY KEY, user_id integer REFERENCES users (id))",
		"INSERT INTO orders (id, user_id) VALUES (1, 42)",
	} {
		if _, err := writable.db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("seed %q: %v", statement, err)
		}
	}
	findings, err := conn.Health(ctx)
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	for _, finding := range findings {
		if finding.Code != "foreign_key_check" {
			continue
		}
		if finding.Severity != driver.SeverityWarn || finding.Value != "1" {
			t.Fatalf("broken references must be reported: %+v", finding)
		}
		return
	}
	t.Fatal("the foreign key check is missing from the report")
}

func TestHealthReportsWriteAheadLogging(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wal.db")
	conn, err := New().Connect(context.Background(), driver.Config{File: path, Mode: sqlguard.ModeReadWrite})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close()
	if _, err := conn.(*connection).db.ExecContext(context.Background(), "PRAGMA journal_mode = WAL"); err != nil {
		t.Fatalf("switch to wal: %v", err)
	}
	findings, err := conn.Health(context.Background())
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	for _, finding := range findings {
		if finding.Code == "journal_mode" && finding.Value == "wal" && finding.Severity == driver.SeverityOK {
			return
		}
	}
	t.Fatalf("write ahead logging must be reported as healthy: %+v", findings)
}

func TestQueryOnAWritableConnectionStillRollsBack(t *testing.T) {
	config := seeded(t)
	config.Mode = sqlguard.ModeReadWrite
	conn := open(t, config)

	result, err := conn.Query(context.Background(), "SELECT count(*) FROM users")
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if !result.Next() {
		t.Fatalf("no rows: %v", result.Err())
	}
	if err := result.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestUnknownSchemaIsReported(t *testing.T) {
	conn := open(t, seeded(t))
	ctx := context.Background()
	calls := map[string]func() error{
		"Tables":    func() error { _, err := conn.Tables(ctx, "nope"); return err },
		"Columns":   func() error { _, err := conn.Columns(ctx, "nope", "users"); return err },
		"Relations": func() error { _, err := conn.Relations(ctx, "nope", "users"); return err },
		"Indexes":   func() error { _, err := conn.Indexes(ctx, "nope"); return err },
	}
	for name, call := range calls {
		if err := call(); err == nil {
			t.Errorf("%s must report an unknown schema", name)
		}
	}
}

func TestUnknownTableYieldsNothing(t *testing.T) {
	conn := open(t, seeded(t))
	ctx := context.Background()
	columns, err := conn.Columns(ctx, "main", "missing")
	if err != nil {
		t.Fatalf("Columns: %v", err)
	}
	if len(columns) != 0 {
		t.Errorf("columns = %+v", columns)
	}
	relations, err := conn.Relations(ctx, "main", "missing")
	if err != nil {
		t.Fatalf("Relations: %v", err)
	}
	if len(relations) != 0 {
		t.Errorf("relations = %+v", relations)
	}
}

func TestQueryReportsScanFailures(t *testing.T) {
	conn := open(t, seeded(t))
	result, err := conn.Query(context.Background(), "SELECT id FROM users ORDER BY id")
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if err := result.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if result.Next() {
		t.Fatal("a closed result must not produce rows")
	}
	if result.Err() != nil {
		t.Logf("closed result reported %v", result.Err())
	}
}

func TestConnectRejectsAnUnwritableFile(t *testing.T) {
	dir := t.TempDir()
	config := driver.Config{File: dir, Mode: sqlguard.ModeReadWrite}
	if _, err := New().Connect(context.Background(), config); err == nil {
		t.Fatal("opening a directory as a database must fail")
	}
}

func closedConnection(t *testing.T) *connection {
	t.Helper()
	conn, err := New().Connect(context.Background(), seeded(t))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	inner := conn.(*connection)
	if err := inner.db.Close(); err != nil {
		t.Fatal(err)
	}
	return inner
}

func TestEveryHealthCheckReportsAClosedDatabase(t *testing.T) {
	conn := closedConnection(t)
	ctx := context.Background()
	checks := map[string]func(context.Context) (driver.Finding, error){
		"integrity":   conn.integrity,
		"foreignKeys": conn.foreignKeys,
		"size":        conn.size,
		"freeSpace":   conn.freeSpace,
		"journal":     conn.journal,
		"sessionMode": conn.sessionMode,
	}
	for name, check := range checks {
		if _, err := check(ctx); err == nil {
			t.Errorf("%s must report the closed database", name)
		}
	}
}

func TestEveryIntrospectionHelperReportsAClosedDatabase(t *testing.T) {
	conn := closedConnection(t)
	ctx := context.Background()
	calls := map[string]func() error{
		"countTables":      func() error { _, err := conn.countTables(ctx, "main"); return err },
		"outbound":         func() error { _, err := conn.outbound(ctx, "main", "orders"); return err },
		"markForeignKeys":  func() error { return conn.markForeignKeys(ctx, "main", "orders", nil) },
		"indexDefinitions": func() error { _, err := conn.indexDefinitions(ctx, "main"); return err },
		"indexesOf":        func() error { _, err := conn.indexesOf(ctx, "main", "orders", nil); return err },
		"pin":              func() error { return conn.pin(ctx) },
	}
	for name, call := range calls {
		if err := call(); err == nil {
			t.Errorf("%s must report the closed database", name)
		}
	}
}

func TestHealthWarnsWhenMostOfTheFileIsUnused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sparse.db")
	conn, err := New().Connect(context.Background(), driver.Config{File: path, Mode: sqlguard.ModeReadWrite})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close()
	inner := conn.(*connection)
	ctx := context.Background()
	for _, statement := range []string{
		"PRAGMA auto_vacuum = NONE",
		"CREATE TABLE filler (id integer PRIMARY KEY, payload blob)",
		"INSERT INTO filler (payload) SELECT randomblob(2000) FROM generate_series(1, 400)",
		"DELETE FROM filler",
	} {
		if _, err := inner.db.ExecContext(ctx, statement); err != nil {
			t.Skipf("cannot build a sparse database here: %v", err)
		}
	}
	finding, err := inner.freeSpace(ctx)
	if err != nil {
		t.Fatalf("freeSpace: %v", err)
	}
	if finding.Severity != driver.SeverityWarn {
		t.Skipf("the file did not end up sparse enough: %+v", finding)
	}
	if finding.Note == "" {
		t.Error("a warning must explain itself")
	}
}

func TestIntegrityFailureIsCritical(t *testing.T) {
	finding := driver.Finding{Subsystem: "integrity", Severity: driver.SeverityOK}
	if finding.Severity != driver.SeverityOK {
		t.Fatal("unexpected fixture")
	}
	conn := open(t, seeded(t))
	inner := conn.(*connection)
	healthy, err := inner.integrity(context.Background())
	if err != nil {
		t.Fatalf("integrity: %v", err)
	}
	if healthy.Severity != driver.SeverityOK || healthy.Value != "ok" {
		t.Errorf("a healthy database must pass: %+v", healthy)
	}
}

func TestPlanNameFallsBackToTheWholeDetail(t *testing.T) {
	if got := planName("SCAN"); got != "SCAN" {
		t.Errorf("planName() = %q", got)
	}
	if got := planName("SCAN orders"); got != "SCAN" {
		t.Errorf("planName() = %q", got)
	}
}

func TestAppendQueryWithoutParameters(t *testing.T) {
	if got := appendQuery("file:x.db", nil); got != "file:x.db" {
		t.Errorf("appendQuery() = %q", got)
	}
}

func TestRowLimitFallsBackToTheDefault(t *testing.T) {
	if rowLimit(0) != defaultRowLimit || rowLimit(-1) != defaultRowLimit {
		t.Error("an unset row limit must fall back to the default")
	}
	if rowLimit(25) != 25 {
		t.Error("a configured row limit must be used")
	}
}

func TestResultCloseReportsARolledBackTransaction(t *testing.T) {
	conn := open(t, seeded(t))
	result, err := conn.Query(context.Background(), "SELECT id FROM users")
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	inner := result.(*resultSet)
	if err := inner.tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := result.Close(); err == nil {
		t.Fatal("closing a result whose transaction is gone must report it")
	}
}

func TestResultStopsAfterAnError(t *testing.T) {
	result := &resultSet{limit: 10, err: context.Canceled}
	if result.Next() {
		t.Fatal("a failed result must not produce rows")
	}
	if result.Truncated() {
		t.Error("a failed result is not a truncated one")
	}
}

func corrupt(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "corrupt.db")
	if err := os.WriteFile(path, []byte("SQLite format 3\x00 and then nonsense"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestConnectRejectsACorruptDatabase(t *testing.T) {
	config := driver.Config{File: corrupt(t), Mode: sqlguard.ModeReadWrite, Timeouts: driver.DefaultTimeouts()}
	if _, err := New().Connect(context.Background(), config); err == nil {
		t.Fatal("a corrupt file must not open")
	}
	config.Mode = sqlguard.ModeReadOnly
	if _, err := New().Connect(context.Background(), config); err == nil {
		t.Fatal("a corrupt file must not open read only either")
	}
}

func TestIndexesReportsAnUnknownSchemaFromEveryStep(t *testing.T) {
	conn := open(t, seeded(t)).(*connection)
	ctx := context.Background()
	if _, err := conn.indexDefinitions(ctx, "nope"); err == nil {
		t.Error("index definitions must report an unknown schema")
	}
	if _, err := conn.indexesOf(ctx, "nope", "users", nil); err == nil {
		t.Error("index listing must report an unknown schema")
	}
	if _, err := conn.countTables(ctx, "nope"); err == nil {
		t.Error("counting tables must report an unknown schema")
	}
}

func TestExplainRejectsAnUnknownSchema(t *testing.T) {
	conn := open(t, seeded(t))
	if _, err := conn.Explain(context.Background(), "SELECT * FROM nope.users", false); err == nil {
		t.Error("explaining an unknown schema must fail")
	}
}

func TestEachReportsScanFailures(t *testing.T) {
	conn := open(t, seeded(t)).(*connection)
	err := conn.each(context.Background(), "SELECT 1", func(rows *sql.Rows) error {
		var first, second int
		return rows.Scan(&first, &second)
	})
	if err == nil {
		t.Fatal("a scan that does not match the result must be reported")
	}
}

func TestEachStopsAtTheFirstFailure(t *testing.T) {
	conn := open(t, seeded(t)).(*connection)
	seen := 0
	err := conn.each(context.Background(), "SELECT id FROM users ORDER BY id", func(*sql.Rows) error {
		seen++
		return context.Canceled
	})
	if err == nil {
		t.Fatal("want the error from the scanner")
	}
	if seen != 1 {
		t.Errorf("the walk must stop at the first failure, saw %d rows", seen)
	}
}

// A name is quoted the way SQLite reads one back, or a table with a capital in
// it is a table the interface cannot open.
func TestPreviewQuotesTheName(t *testing.T) {
	conn := &connection{}
	for _, want := range []struct {
		name          string
		schema, table string
		statement     string
	}{
		{"a plain name", "main", "users", `SELECT * FROM "main"."users"`},
		{"no schema", "", "users", `SELECT * FROM "users"`},
		{"a capital", "main", "Orders", `SELECT * FROM "main"."Orders"`},
		{"a quote in it", "main", `we"ird`, `SELECT * FROM "main"."we""ird"`},
		{"a dot in it", "main", "a.b", `SELECT * FROM "main"."a.b"`},
	} {
		t.Run(want.name, func(t *testing.T) {
			if got := conn.Preview(want.schema, want.table); got != want.statement {
				t.Errorf("Preview = %q, want %q", got, want.statement)
			}
		})
	}
}

// A stream reads past the cap a drawn result stops at.
func TestStreamIgnoresTheRowLimit(t *testing.T) {
	config := seeded(t)
	config.RowLimit = 1
	conn := open(t, config)

	result, err := conn.Stream(context.Background(), "SELECT id FROM users ORDER BY id")
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	rows := 0
	for result.Next() {
		rows++
	}
	if rows <= config.RowLimit {
		t.Fatalf("rows = %d, a stream reads past the cap of %d", rows, config.RowLimit)
	}
	if result.Truncated() {
		t.Error("and nothing was left behind to be truncated")
	}
	if err := result.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}
