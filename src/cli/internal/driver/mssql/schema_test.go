package mssql

import (
	"context"
	sqldriver "database/sql/driver"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/sonquer/opendba/src/cli/internal/driver"
)

func TestInfo(t *testing.T) {
	conn, mock := mocked(t, readOnlyConfig())
	mock.ExpectQuery(infoQuery).WillReturnRows(
		sqlmock.NewRows([]string{"version", "edition", "database", "user", "readonly", "write", "super"}).
			AddRow("16.0.4165.4", "Developer Edition", "app", "reader", false, false, false))

	info, err := conn.Info(context.Background())
	if err != nil {
		t.Fatalf("Info() = %v", err)
	}
	if info.Driver != Name || info.Version != "16.0.4165.4 Developer Edition" {
		t.Errorf("info = %+v", info)
	}
	if info.Database != "app" || info.User != "reader" || info.CanWrite || info.Superuser {
		t.Errorf("info = %+v", info)
	}
	if !info.ReadOnly {
		t.Error("a read only profile is read only whatever the database says")
	}
	if info.ConnectedAt.IsZero() {
		t.Error("a connection is made at a time")
	}
}

func TestInfoReportsAServerThatWillNotSay(t *testing.T) {
	conn, mock := mocked(t, readOnlyConfig())
	mock.ExpectQuery(infoQuery).WillReturnError(errRefused)
	if _, err := conn.Info(context.Background()); err == nil {
		t.Fatal("want an error")
	}
}

func TestDatabases(t *testing.T) {
	conn, mock := mocked(t, readOnlyConfig())
	mock.ExpectQuery(databasesQuery).WillReturnRows(
		sqlmock.NewRows([]string{"name", "current", "comment"}).
			AddRow("app", true, "ONLINE - FULL").
			AddRow("master", false, "ONLINE - SIMPLE"))

	databases, err := conn.Databases(context.Background())
	if err != nil {
		t.Fatalf("Databases() = %v", err)
	}
	if len(databases) != 2 || !databases[0].Current || databases[1].Name != "master" {
		t.Errorf("databases = %+v", databases)
	}
	if databases[0].Comment != "ONLINE - FULL" {
		t.Errorf("a database says how it is and how it is kept: %+v", databases[0])
	}
}

func TestDatabasesReportsARefusal(t *testing.T) {
	conn, mock := mocked(t, readOnlyConfig())
	mock.ExpectQuery(databasesQuery).WillReturnError(errRefused)
	if _, err := conn.Databases(context.Background()); err == nil {
		t.Fatal("want an error")
	}
}

func TestSchemasMarkTheServersOwn(t *testing.T) {
	conn, mock := mocked(t, readOnlyConfig())
	mock.ExpectQuery(schemasQuery).WillReturnRows(
		sqlmock.NewRows([]string{"name", "tables", "system"}).
			AddRow("dbo", 12, false).
			AddRow("sys", 0, true))

	schemas, err := conn.Schemas(context.Background())
	if err != nil {
		t.Fatalf("Schemas() = %v", err)
	}
	if len(schemas) != 2 || schemas[0].Name != "dbo" || schemas[0].Tables != 12 {
		t.Errorf("schemas = %+v", schemas)
	}
	if schemas[0].System || !schemas[1].System {
		t.Errorf("the server's own schemas must be marked: %+v", schemas)
	}
}

func TestSchemasReportsARefusal(t *testing.T) {
	conn, mock := mocked(t, readOnlyConfig())
	mock.ExpectQuery(schemasQuery).WillReturnError(errRefused)
	if _, err := conn.Schemas(context.Background()); err == nil {
		t.Fatal("want an error")
	}
}

func tableRows() *sqlmock.Rows {
	refreshed := time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)
	return sqlmock.NewRows([]string{"schema", "name", "kind", "rows", "size", "comment", "indexes", "refreshed"}).
		AddRow("dbo", "users", "table", int64(1000), int64(65536), "people", int64(16384), refreshed).
		AddRow("dbo", "active_users", "view", int64(0), int64(0), "", int64(0), time.Time{})
}

func TestTables(t *testing.T) {
	conn, mock := mocked(t, readOnlyConfig())
	mock.ExpectQuery(tablesQuery).WithArgs("dbo").WillReturnRows(tableRows())
	mock.ExpectQuery(tableUsageQuery).WithArgs("dbo").WillReturnRows(
		sqlmock.NewRows([]string{"schema", "name", "index", "sequential"}).
			AddRow("dbo", "users", int64(900), int64(100)))

	tables, err := conn.Tables(context.Background(), "dbo")
	if err != nil {
		t.Fatalf("Tables() = %v", err)
	}
	if len(tables) != 2 || tables[0].Qualified() != "dbo.users" || tables[1].Kind != "view" {
		t.Errorf("tables = %+v", tables)
	}
	if tables[0].Rows != 1000 || tables[0].Size != 65536 || tables[0].IndexSize != 16384 {
		t.Errorf("table = %+v", tables[0])
	}
	if tables[0].LastVacuum.IsZero() || !tables[1].LastVacuum.IsZero() {
		t.Errorf("statistics are dated only when they were built: %+v", tables)
	}
	share, counted := tables[0].Indexed()
	if !counted || share != 0.9 {
		t.Errorf("Indexed() = %v, %v", share, counted)
	}
	if _, dead := tables[0].Dead(); dead {
		t.Error("sql server has no dead rows to count, and must not pretend to")
	}
}

func TestTablesLeaveTheCountersUnmeasuredWhenTheyAreRefused(t *testing.T) {
	conn, mock := mocked(t, readOnlyConfig())
	mock.ExpectQuery(tablesQuery).WithArgs("").WillReturnRows(tableRows())
	mock.ExpectQuery(tableUsageQuery).WithArgs("").WillReturnError(errRefused)

	tables, err := conn.Tables(context.Background(), "")
	if err != nil {
		t.Fatalf("a refused counter must not fail the listing: %v", err)
	}
	if len(tables) != 2 {
		t.Fatalf("tables = %+v", tables)
	}
	if tables[0].Stats {
		t.Error("a table whose counters were refused must not claim them")
	}
	if tables[0].IndexScans != unmeasured || tables[0].SeqScans != unmeasured {
		t.Errorf("what was not measured is negative, never zero: %+v", tables[0])
	}
	if _, counted := tables[0].Indexed(); counted {
		t.Error("a share that was not measured must not be reported")
	}
}

func TestTablesReportsAListingItCannotRead(t *testing.T) {
	conn, mock := mocked(t, readOnlyConfig())
	mock.ExpectQuery(tablesQuery).WithArgs("").WillReturnError(errRefused)
	_, err := conn.Tables(context.Background(), "")
	if err == nil {
		t.Fatal("want an error")
	}
	if !strings.Contains(err.Error(), "every schema") {
		t.Errorf("an error must say where it was looking: %v", err)
	}
}

func columnRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{"name", "type", "nullable", "default", "primary", "position", "comment"}).
		AddRow("id", "int", false, "", true, 1, "").
		AddRow("email", "nvarchar(320)", false, "", false, 2, "how to reach them").
		AddRow("team_id", "int", true, "((0))", false, 3, "")
}

func relationRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"name", "fs", "ft", "fc", "ts", "tt", "tc", "delete", "position", "inbound"}).
		AddRow("FK_users_teams", "dbo", "users", "team_id", "dbo", "teams", "id", "CASCADE", 1, false)
}

func TestColumns(t *testing.T) {
	conn, mock := mocked(t, readOnlyConfig())
	mock.ExpectQuery(columnsQuery).WithArgs("dbo", "users").WillReturnRows(columnRows())
	mock.ExpectQuery(relationsQuery).WithArgs("dbo", "users").WillReturnRows(relationRows())

	columns, err := conn.Columns(context.Background(), "dbo", "users")
	if err != nil {
		t.Fatalf("Columns() = %v", err)
	}
	if len(columns) != 3 || columns[0].Name != "id" || !columns[0].PrimaryKey {
		t.Errorf("columns = %+v", columns)
	}
	if columns[1].Type != "nvarchar(320)" || columns[1].Comment != "how to reach them" {
		t.Errorf("column = %+v", columns[1])
	}
	if !columns[2].Nullable || columns[2].Default != "((0))" {
		t.Errorf("column = %+v", columns[2])
	}
	if columns[2].ForeignKey != "teams.id" {
		t.Errorf("a column that refers to another must say which: %+v", columns[2])
	}
}

func TestColumnsReportsWhatItCannotRead(t *testing.T) {
	t.Run("the columns", func(t *testing.T) {
		conn, mock := mocked(t, readOnlyConfig())
		mock.ExpectQuery(columnsQuery).WithArgs("dbo", "users").WillReturnError(errRefused)
		if _, err := conn.Columns(context.Background(), "dbo", "users"); err == nil {
			t.Fatal("want an error")
		}
	})
	t.Run("the relations", func(t *testing.T) {
		conn, mock := mocked(t, readOnlyConfig())
		mock.ExpectQuery(columnsQuery).WithArgs("dbo", "users").WillReturnRows(columnRows())
		mock.ExpectQuery(relationsQuery).WithArgs("dbo", "users").WillReturnError(errRefused)
		if _, err := conn.Columns(context.Background(), "dbo", "users"); err == nil {
			t.Fatal("want an error")
		}
	})
}

func TestRelationsGroupTheColumnsOfOneKey(t *testing.T) {
	conn, mock := mocked(t, readOnlyConfig())
	mock.ExpectQuery(relationsQuery).WithArgs("dbo", "memberships").WillReturnRows(
		sqlmock.NewRows([]string{
			"name", "fs", "ft", "fc", "ts", "tt", "tc", "delete", "position", "inbound"}).
			AddRow("FK_pair", "dbo", "memberships", "user_id", "dbo", "users", "id", "NO_ACTION", 1, false).
			AddRow("FK_pair", "dbo", "memberships", "team_id", "dbo", "users", "team", "NO_ACTION", 2, false).
			AddRow("FK_in", "dbo", "audit", "membership_id", "dbo", "memberships", "id", "SET_NULL", 1, true))

	relations, err := conn.Relations(context.Background(), "dbo", "memberships")
	if err != nil {
		t.Fatalf("Relations() = %v", err)
	}
	if len(relations) != 2 {
		t.Fatalf("relations = %+v", relations)
	}
	outbound := relations[0]
	if len(outbound.FromColumns) != 2 || outbound.FromColumns[1] != "team_id" {
		t.Errorf("a key over two columns is one relation: %+v", outbound)
	}
	if outbound.ConstraintDef != "FOREIGN KEY ([user_id], [team_id]) REFERENCES [dbo].[users] ([id], [team])" {
		t.Errorf("definition = %q", outbound.ConstraintDef)
	}
	if !relations[1].Inbound || relations[1].OnDelete != "SET NULL" {
		t.Errorf("relation = %+v", relations[1])
	}
}

func TestRelationsReportsARefusal(t *testing.T) {
	conn, mock := mocked(t, readOnlyConfig())
	mock.ExpectQuery(relationsQuery).WithArgs("dbo", "users").WillReturnError(errRefused)
	if _, err := conn.Relations(context.Background(), "dbo", "users"); err == nil {
		t.Fatal("want an error")
	}
}

func TestDeleteAction(t *testing.T) {
	cases := map[string]string{
		"CASCADE": "CASCADE", "SET_NULL": "SET NULL", "SET_DEFAULT": "SET DEFAULT",
		"NO_ACTION": "NO ACTION", "": "NO ACTION",
	}
	for described, want := range cases {
		if got := DeleteAction(described); got != want {
			t.Errorf("DeleteAction(%q) = %q, want %q", described, got, want)
		}
	}
}

func TestConstraintDefinitionLeavesOffWhatDoesNotHappen(t *testing.T) {
	relation := driver.Relation{
		FromColumns: []string{"a"}, ToSchema: "dbo", ToTable: "t",
		ToColumns: []string{"b"}, OnDelete: "NO ACTION",
	}
	if got := ConstraintDefinition(relation); strings.Contains(got, "ON DELETE") {
		t.Errorf("definition = %q", got)
	}
}

func TestWhereNamesTheSchemasThatWereLookedIn(t *testing.T) {
	if got := where(" "); got != "every schema" {
		t.Errorf("where() = %q", got)
	}
	if got := where("dbo"); got != "dbo" {
		t.Errorf("where() = %q", got)
	}
}

func TestMarkForeignKeysLeavesInboundOnesAlone(t *testing.T) {
	columns := []driver.Column{{Name: "id"}}
	markForeignKeys([]driver.Relation{{
		Inbound: true, FromColumns: []string{"id"}, ToTable: "other", ToColumns: []string{"x"},
	}}, columns)
	if columns[0].ForeignKey != "" {
		t.Errorf("a key pointing at this table does not make its column a reference: %+v", columns[0])
	}
}

// TestEveryListingReportsARowItCannotRead covers the one failure a server can
// produce at any point in a listing: a row whose columns are not what the
// query said they would be.
func TestEveryListingReportsARowItCannotRead(t *testing.T) {
	unreadable := func(names ...string) *sqlmock.Rows {
		values := make([]sqldriver.Value, len(names))
		for i := range values {
			values[i] = "not what the query said"
		}
		return sqlmock.NewRows(names).AddRow(values...)
	}
	cases := []struct {
		name string
		want func(conn *connection, mock sqlmock.Sqlmock) error
	}{
		{"databases", func(conn *connection, mock sqlmock.Sqlmock) error {
			mock.ExpectQuery(databasesQuery).WillReturnRows(unreadable("name", "current", "comment"))
			_, err := conn.Databases(context.Background())
			return err
		}},
		{"schemas", func(conn *connection, mock sqlmock.Sqlmock) error {
			mock.ExpectQuery(schemasQuery).WillReturnRows(unreadable("name", "tables", "system"))
			_, err := conn.Schemas(context.Background())
			return err
		}},
		{"tables", func(conn *connection, mock sqlmock.Sqlmock) error {
			mock.ExpectQuery(tablesQuery).WithArgs("").WillReturnRows(
				unreadable("schema", "name", "kind", "rows", "size", "comment", "indexes", "refreshed"))
			_, err := conn.Tables(context.Background(), "")
			return err
		}},
		{"columns", func(conn *connection, mock sqlmock.Sqlmock) error {
			mock.ExpectQuery(columnsQuery).WithArgs("", "users").WillReturnRows(
				unreadable("name", "type", "nullable", "default", "primary", "position", "comment"))
			_, err := conn.Columns(context.Background(), "", "users")
			return err
		}},
		{"relations", func(conn *connection, mock sqlmock.Sqlmock) error {
			mock.ExpectQuery(relationsQuery).WithArgs("", "users").WillReturnRows(
				unreadable("name", "fs", "ft", "fc", "ts", "tt", "tc", "delete", "position", "inbound"))
			_, err := conn.Relations(context.Background(), "", "users")
			return err
		}},
		{"indexes", func(conn *connection, mock sqlmock.Sqlmock) error {
			mock.ExpectQuery(indexesQuery).WithArgs("").WillReturnRows(
				unreadable("schema", "table", "name", "unique", "primary", "kind", "filter", "size"))
			_, err := conn.Indexes(context.Background(), "")
			return err
		}},
		{"index columns", func(conn *connection, mock sqlmock.Sqlmock) error {
			mock.ExpectQuery(indexesQuery).WithArgs("").WillReturnRows(indexRows())
			mock.ExpectQuery(indexColumnsQuery).WithArgs("").WillReturnRows(
				unreadable("schema", "table", "index", "column", "descending", "included"))
			_, err := conn.Indexes(context.Background(), "")
			return err
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			conn, mock := mocked(t, readOnlyConfig())
			if err := c.want(conn, mock); err == nil {
				t.Fatal("a row that cannot be read must reach the caller")
			}
		})
	}
}

// TestCountersThatCannotBeReadAreNotHalfCounted covers the counter queries that
// answer and then fail part way through: what they had is thrown away rather
// than reported as a measurement.
func TestCountersThatCannotBeReadAreNotHalfCounted(t *testing.T) {
	t.Run("tables", func(t *testing.T) {
		conn, mock := mocked(t, readOnlyConfig())
		mock.ExpectQuery(tablesQuery).WithArgs("").WillReturnRows(tableRows())
		mock.ExpectQuery(tableUsageQuery).WithArgs("").WillReturnRows(
			sqlmock.NewRows([]string{"schema", "name", "index", "sequential"}).
				AddRow(nil, nil, nil, nil))
		tables, err := conn.Tables(context.Background(), "")
		if err != nil {
			t.Fatalf("Tables() = %v", err)
		}
		if tables[0].Stats {
			t.Error("counters that could not be read are not counters")
		}
	})
	t.Run("indexes", func(t *testing.T) {
		conn, mock := mocked(t, readOnlyConfig())
		mock.ExpectQuery(indexesQuery).WithArgs("").WillReturnRows(indexRows())
		mock.ExpectQuery(indexColumnsQuery).WithArgs("").WillReturnRows(indexColumnRows())
		mock.ExpectQuery(indexUsageQuery).WithArgs("").WillReturnRows(
			sqlmock.NewRows([]string{"schema", "table", "index", "scans"}).AddRow(nil, nil, nil, nil))
		indexes, err := conn.Indexes(context.Background(), "")
		if err != nil {
			t.Fatalf("Indexes() = %v", err)
		}
		if indexes[0].Stats {
			t.Error("counters that could not be read are not counters")
		}
	})
}
