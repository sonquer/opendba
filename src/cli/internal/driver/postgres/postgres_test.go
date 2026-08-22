package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/pashagolub/pgxmock/v5"

	"github.com/sonquer/tui4db/src/cli/internal/driver"
	"github.com/sonquer/tui4db/src/cli/pkg/sqlguard"
)

func readOnlyConfig() driver.Config {
	return driver.Config{
		Host:        "db.example.com",
		Port:        5432,
		Database:    "app",
		User:        "readonly",
		SSLMode:     "verify-full",
		Mode:        sqlguard.ModeReadOnly,
		Application: "production-eu",
		Timeouts:    driver.DefaultTimeouts(),
	}
}

func mocked(t *testing.T, config driver.Config) (*connection, pgxmock.PgxPoolIface) {
	t.Helper()
	pool, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	t.Cleanup(func() {
		if err := pool.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet expectations: %v", err)
		}
		pool.Close()
	})
	return &connection{db: pool, config: config}, pool
}

func TestDriverIdentity(t *testing.T) {
	d := New()
	if d.Name() != Name || d.Title() != "PostgreSQL" {
		t.Errorf("identity = %q %q", d.Name(), d.Title())
	}
	caps := d.Capabilities()
	if !caps.Explain || !caps.Relations || !caps.Health || !caps.IndexStats || !caps.ReadOnlySession {
		t.Errorf("capabilities = %+v", caps)
	}
}

func TestDSN(t *testing.T) {
	dsn := DSN(readOnlyConfig())
	for _, want := range []string{"postgres://readonly@db.example.com:5432/app", "sslmode=verify-full", "application_name=tui4db%2Fproduction-eu"} {
		if !strings.Contains(dsn, want) {
			t.Errorf("dsn missing %q: %s", want, dsn)
		}
	}
	if strings.Contains(dsn, "password") {
		t.Errorf("the password must never reach the connection string: %s", dsn)
	}

	bare := DSN(driver.Config{Database: "app"})
	if !strings.Contains(bare, "localhost:5432") {
		t.Errorf("defaults = %s", bare)
	}
	if !strings.Contains(bare, "application_name=tui4db") {
		t.Errorf("application name = %s", bare)
	}

	withoutDatabase := DSN(driver.Config{Host: "db.example.com", User: "readonly"})
	if strings.Contains(withoutDatabase, "5432/") {
		t.Errorf("without a database the server picks its own default: %s", withoutDatabase)
	}
	settings, err := poolConfig(driver.Config{Host: "db.example.com", User: "readonly"})
	if err != nil {
		t.Fatalf("poolConfig: %v", err)
	}
	if settings.ConnConfig.Database != "" {
		t.Errorf("pgx must leave the database to the server, got %q", settings.ConnConfig.Database)
	}

	withOptions := DSN(driver.Config{Database: "app", Options: "-c search_path=app"})
	if !strings.Contains(withOptions, "options=") {
		t.Errorf("options = %s", withOptions)
	}
}

func TestSessionStatements(t *testing.T) {
	statements := SessionStatements(readOnlyConfig())
	want := []string{
		"SET statement_timeout = 15000",
		"SET lock_timeout = 2000",
		"SET idle_in_transaction_session_timeout = 30000",
		"SET default_transaction_read_only = on",
	}
	if len(statements) != len(want) {
		t.Fatalf("statements = %v", statements)
	}
	for i, statement := range want {
		if statements[i] != statement {
			t.Errorf("statement %d = %q, want %q", i, statements[i], statement)
		}
	}

	writable := readOnlyConfig()
	writable.Mode = sqlguard.ModeReadWrite
	for _, statement := range SessionStatements(writable) {
		if strings.Contains(statement, "read_only") {
			t.Error("a read write connection must not be pinned read only")
		}
	}

	unset := SessionStatements(driver.Config{Mode: sqlguard.ModeReadWrite})
	if unset[0] != "SET statement_timeout = 15000" {
		t.Errorf("missing timeouts must fall back to the defaults: %v", unset)
	}
}

func TestRedact(t *testing.T) {
	cases := map[string]string{
		"postgres://user:hunter2@host:5432/app":          "xxxxx",
		"postgres://user@host:5432/app?password=hunter2": "xxxxx",
	}
	for dsn, want := range cases {
		got := Redact(dsn)
		if strings.Contains(got, "hunter2") {
			t.Errorf("Redact(%q) leaked the password: %s", dsn, got)
		}
		if !strings.Contains(got, want) {
			t.Errorf("Redact(%q) = %s", dsn, got)
		}
	}
	if got := Redact("postgres://user@host/app"); strings.Contains(got, "xxxxx") {
		t.Errorf("a connection string without a password must be left alone: %s", got)
	}
	if got := Redact("://nope"); got != "postgres://redacted" {
		t.Errorf("an unparsable string must still be safe to show: %s", got)
	}
}

func TestSplit(t *testing.T) {
	dsn, password, err := Split("  postgres://user:hunter2@host:5432/app  ")
	if err != nil {
		t.Fatalf("Split: %v", err)
	}
	if string(password) != "hunter2" {
		t.Errorf("password = %q", password)
	}
	if strings.Contains(dsn, "hunter2") {
		t.Errorf("the remainder still holds the password: %s", dsn)
	}
	if !strings.Contains(dsn, "user@host") {
		t.Errorf("the user must survive: %s", dsn)
	}

	if _, password, err := Split("postgres://user@host/app"); err != nil || password != nil {
		t.Errorf("Split without a password = %q, %v", password, err)
	}
	if _, _, err := Split("postgres://host/app"); err != nil {
		t.Errorf("Split without a user = %v", err)
	}
	if _, _, err := Split("://nope"); err == nil {
		t.Error("an unparsable connection string must be reported")
	}
}

func TestPoolConfigCarriesThePasswordOutOfBand(t *testing.T) {
	config := readOnlyConfig()
	config.Password = []byte("hunter2")
	settings, err := poolConfig(config)
	if err != nil {
		t.Fatalf("poolConfig: %v", err)
	}
	if settings.ConnConfig.Password != "hunter2" {
		t.Errorf("password = %q", settings.ConnConfig.Password)
	}
	if settings.ConnConfig.ConnectTimeout != config.Timeouts.Connect {
		t.Errorf("connect timeout = %v", settings.ConnConfig.ConnectTimeout)
	}
	if settings.AfterConnect == nil {
		t.Error("the session must be pinned on every connection")
	}
	if _, err := poolConfig(driver.Config{Host: "\x00"}); err == nil {
		t.Error("an unusable host must be reported")
	}
}

func TestConnectReportsAnUnreachableServer(t *testing.T) {
	config := readOnlyConfig()
	config.Host = "127.0.0.1"
	config.Port = 1
	config.Timeouts.Connect = 200 * time.Millisecond
	if _, err := New().Connect(context.Background(), config); err == nil {
		t.Fatal("connecting to a closed port must fail")
	}
}

func TestInfo(t *testing.T) {
	conn, pool := mocked(t, readOnlyConfig())
	pool.ExpectQuery("current_setting").WillReturnRows(
		pgxmock.NewRows([]string{"version", "database", "user", "read_only", "can_write", "superuser"}).
			AddRow("16.3", "app", "readonly", true, false, false))

	info, err := conn.Info(context.Background())
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if info.Version != "16.3" || info.Database != "app" || info.User != "readonly" {
		t.Fatalf("info = %+v", info)
	}
	if !info.ReadOnly || info.CanWrite {
		t.Errorf("a read only session must be reported: %+v", info)
	}
}

func TestInfoReportsFailures(t *testing.T) {
	conn, pool := mocked(t, readOnlyConfig())
	pool.ExpectQuery("current_setting").WillReturnError(errors.New("connection reset"))
	if _, err := conn.Info(context.Background()); err == nil {
		t.Fatal("want an error")
	}
}

func TestDatabases(t *testing.T) {
	conn, pool := mocked(t, readOnlyConfig())
	pool.ExpectQuery("pg_database").WillReturnRows(
		pgxmock.NewRows([]string{"name", "current", "comment"}).
			AddRow("app", true, "the one in use").
			AddRow("reporting", false, ""))

	databases, err := conn.Databases(context.Background())
	if err != nil {
		t.Fatalf("Databases: %v", err)
	}
	if len(databases) != 2 || databases[0].Name != "app" || !databases[0].Current {
		t.Fatalf("databases = %+v", databases)
	}
	if databases[1].Current {
		t.Error("only one database can be the one in use")
	}
}

func TestSchemas(t *testing.T) {
	conn, pool := mocked(t, readOnlyConfig())
	pool.ExpectQuery("pg_namespace").WillReturnRows(
		pgxmock.NewRows([]string{"name", "tables", "system"}).
			AddRow("public", 12, false).
			AddRow("pg_catalog", 62, true))

	schemas, err := conn.Schemas(context.Background())
	if err != nil {
		t.Fatalf("Schemas: %v", err)
	}
	if len(schemas) != 2 || schemas[0].Name != "public" || schemas[0].Tables != 12 {
		t.Fatalf("schemas = %+v", schemas)
	}
	if !schemas[1].System {
		t.Error("catalog schemas must be marked as system schemas")
	}
}

func TestTables(t *testing.T) {
	conn, pool := mocked(t, readOnlyConfig())
	pool.ExpectQuery("pg_class").WithArgs("app").WillReturnRows(
		pgxmock.NewRows([]string{"schema", "name", "kind", "rows", "size", "comment"}).
			AddRow("app", "users", "table", int64(1200), int64(8192), "people"))

	tables, err := conn.Tables(context.Background(), "app")
	if err != nil {
		t.Fatalf("Tables: %v", err)
	}
	if len(tables) != 1 || tables[0].Qualified() != "app.users" {
		t.Fatalf("tables = %+v", tables)
	}
	if tables[0].Rows != 1200 || tables[0].Size != 8192 || tables[0].Comment != "people" {
		t.Errorf("table = %+v", tables[0])
	}
}

// A connection with no schema configured lists what the whole database holds,
// with each row saying where it came from. Anything else hides tables from
// someone who never picked a schema.
func TestTablesWithoutASchemaReachEveryOne(t *testing.T) {
	conn, pool := mocked(t, readOnlyConfig())
	pool.ExpectQuery("pg_class").WithArgs("").WillReturnRows(
		pgxmock.NewRows([]string{"schema", "name", "kind", "rows", "size", "comment"}).
			AddRow("app", "users", "table", int64(2), int64(8192), "").
			AddRow("billing", "invoices", "table", int64(9), int64(4096), ""))

	tables, err := conn.Tables(context.Background(), "")
	if err != nil {
		t.Fatalf("Tables: %v", err)
	}
	if len(tables) != 2 {
		t.Fatalf("tables = %+v", tables)
	}
	if tables[0].Qualified() != "app.users" || tables[1].Qualified() != "billing.invoices" {
		t.Errorf("tables = %+v", tables)
	}
}

func TestIndexesWithoutASchemaReachEveryOne(t *testing.T) {
	conn, pool := mocked(t, readOnlyConfig())
	pool.ExpectQuery("pg_index").WithArgs("").WillReturnRows(
		pgxmock.NewRows([]string{"schema", "table", "name", "definition", "size", "scans", "unique", "primary"}).
			AddRow("billing", "invoices", "invoices_pkey", "CREATE UNIQUE INDEX", int64(16384), int64(94), true, true))

	indexes, err := conn.Indexes(context.Background(), "")
	if err != nil {
		t.Fatalf("Indexes: %v", err)
	}
	if len(indexes) != 1 || indexes[0].Schema != "billing" {
		t.Fatalf("indexes = %+v", indexes)
	}
}

func TestListingFailuresNameThePlace(t *testing.T) {
	conn, pool := mocked(t, readOnlyConfig())
	pool.ExpectQuery("pg_class").WithArgs("").WillReturnError(errors.New("connection reset"))
	_, err := conn.Tables(context.Background(), "")
	if err == nil || !strings.Contains(err.Error(), "every schema") {
		t.Errorf("err = %v", err)
	}
}

func TestColumnsMarkForeignKeys(t *testing.T) {
	conn, pool := mocked(t, readOnlyConfig())
	pool.ExpectQuery("pg_attribute").WithArgs("public", "orders").WillReturnRows(
		pgxmock.NewRows([]string{"name", "type", "nullable", "default", "primary", "position", "comment"}).
			AddRow("id", "bigint", false, "", true, 1, "").
			AddRow("user_id", "bigint", false, "", false, 2, ""))
	pool.ExpectQuery("pg_constraint").WithArgs("public", "orders").WillReturnRows(
		pgxmock.NewRows([]string{"name", "from_schema", "from_table", "from_columns", "to_schema", "to_table", "to_columns", "on_delete", "deferrable", "definition", "inbound"}).
			AddRow("orders_user_id_fkey", "public", "orders", []string{"user_id"}, "public", "users", []string{"id"}, "c", false, "FOREIGN KEY (user_id) REFERENCES users(id)", false))

	columns, err := conn.Columns(context.Background(), "public", "orders")
	if err != nil {
		t.Fatalf("Columns: %v", err)
	}
	if len(columns) != 2 {
		t.Fatalf("columns = %+v", columns)
	}
	if !columns[0].PrimaryKey {
		t.Errorf("primary key = %+v", columns[0])
	}
	if columns[1].ForeignKey != "users.id" {
		t.Errorf("foreign key = %+v", columns[1])
	}
}

func TestRelations(t *testing.T) {
	conn, pool := mocked(t, readOnlyConfig())
	pool.ExpectQuery("pg_constraint").WithArgs("public", "users").WillReturnRows(
		pgxmock.NewRows([]string{"name", "from_schema", "from_table", "from_columns", "to_schema", "to_table", "to_columns", "on_delete", "deferrable", "definition", "inbound"}).
			AddRow("orders_user_id_fkey", "public", "orders", []string{"user_id"}, "public", "users", []string{"id"}, "c", false, "FOREIGN KEY", true))

	relations, err := conn.Relations(context.Background(), "public", "users")
	if err != nil {
		t.Fatalf("Relations: %v", err)
	}
	if len(relations) != 1 || !relations[0].Inbound {
		t.Fatalf("relations = %+v", relations)
	}
	if relations[0].OnDelete != "CASCADE" {
		t.Errorf("on delete = %q", relations[0].OnDelete)
	}
}

func TestDeleteAction(t *testing.T) {
	cases := map[string]string{"a": "NO ACTION", "r": "RESTRICT", "c": "CASCADE", "n": "SET NULL", "d": "SET DEFAULT", "x": "X"}
	for code, want := range cases {
		if got := DeleteAction(code); got != want {
			t.Errorf("DeleteAction(%q) = %q, want %q", code, got, want)
		}
	}
}

func TestIndexes(t *testing.T) {
	conn, pool := mocked(t, readOnlyConfig())
	pool.ExpectQuery("pg_index").WithArgs("public").WillReturnRows(
		pgxmock.NewRows([]string{"schema", "table", "name", "definition", "size", "scans", "unique", "primary"}).
			AddRow("public", "orders", "orders_pkey", "CREATE UNIQUE INDEX", int64(16384), int64(94), true, true))

	indexes, err := conn.Indexes(context.Background(), "public")
	if err != nil {
		t.Fatalf("Indexes: %v", err)
	}
	if len(indexes) != 1 || !indexes[0].Primary || indexes[0].Scans != 94 {
		t.Fatalf("indexes = %+v", indexes)
	}
}

func TestListingsReportFailures(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name string
		args []any
		call func(*connection) error
	}{
		{"Databases", nil, func(c *connection) error { _, err := c.Databases(ctx); return err }},
		{"Schemas", nil, func(c *connection) error { _, err := c.Schemas(ctx); return err }},
		{"Tables", []any{"public"}, func(c *connection) error { _, err := c.Tables(ctx, "public"); return err }},
		{"Columns", []any{"public", "users"}, func(c *connection) error { _, err := c.Columns(ctx, "public", "users"); return err }},
		{"Relations", []any{"public", "users"}, func(c *connection) error { _, err := c.Relations(ctx, "public", "users"); return err }},
		{"Indexes", []any{"public"}, func(c *connection) error { _, err := c.Indexes(ctx, "public"); return err }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			conn, pool := mocked(t, readOnlyConfig())
			expectation := pool.ExpectQuery(".*")
			if len(c.args) > 0 {
				expectation = expectation.WithArgs(c.args...)
			}
			expectation.WillReturnError(errors.New("connection reset"))
			if err := c.call(conn); err == nil {
				t.Fatal("want an error")
			}
		})
	}
}

func TestColumnsReportsRelationFailures(t *testing.T) {
	conn, pool := mocked(t, readOnlyConfig())
	pool.ExpectQuery("pg_attribute").WithArgs("public", "orders").WillReturnRows(
		pgxmock.NewRows([]string{"name", "type", "nullable", "default", "primary", "position", "comment"}).
			AddRow("id", "bigint", false, "", true, 1, ""))
	pool.ExpectQuery("pg_constraint").WithArgs("public", "orders").WillReturnError(errors.New("connection reset"))
	if _, err := conn.Columns(context.Background(), "public", "orders"); err == nil {
		t.Fatal("want an error")
	}
}

func TestScanFailuresAreReported(t *testing.T) {
	conn, pool := mocked(t, readOnlyConfig())
	pool.ExpectQuery("pg_namespace").WillReturnRows(
		pgxmock.NewRows([]string{"name"}).AddRow("public"))
	if _, err := conn.Schemas(context.Background()); err == nil {
		t.Fatal("a result that does not match must be reported")
	}
}
