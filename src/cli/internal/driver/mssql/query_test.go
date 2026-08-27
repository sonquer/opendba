package mssql

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/sonquer/opendba/src/cli/internal/driver"
)

func drain(result driver.ResultSet) [][]any {
	var rows [][]any
	for result.Next() {
		rows = append(rows, result.Values())
	}
	return rows
}

func threeRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "email"}).
		AddRow(int64(1), "a@example.com").
		AddRow(int64(2), "b@example.com").
		AddRow(int64(3), "c@example.com")
}

func TestQueryRunsInATransactionAndKeepsNothingOnAReadOnlyProfile(t *testing.T) {
	conn, mock := mocked(t, readOnlyConfig())
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id, email FROM users").WillReturnRows(threeRows())
	mock.ExpectRollback()

	result, err := conn.Query(context.Background(), "SELECT id, email FROM users")
	if err != nil {
		t.Fatalf("Query() = %v", err)
	}
	if got := result.Columns(); len(got) != 2 || got[0] != "id" {
		t.Errorf("Columns() = %v", got)
	}
	if rows := drain(result); len(rows) != 3 {
		t.Errorf("rows = %v", rows)
	}
	if result.Truncated() {
		t.Error("three rows under the cap is not truncated")
	}
	if result.Duration() < 0 {
		t.Error("a query cannot take less than no time")
	}
	if err := driver.Finish(result); err != nil {
		t.Errorf("Finish() = %v", err)
	}
}

func TestQueryKeepsItsWorkOnAWritableProfile(t *testing.T) {
	conn, mock := mocked(t, writableConfig())
	mock.ExpectBegin()
	mock.ExpectQuery("DELETE FROM users").WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectCommit()

	result, err := conn.Query(context.Background(), "DELETE FROM users")
	if err != nil {
		t.Fatalf("Query() = %v", err)
	}
	drain(result)
	if err := driver.Finish(result); err != nil {
		t.Errorf("Finish() = %v", err)
	}
}

func TestAResultKeepsItsWorkOnlyWhenEverythingWentWell(t *testing.T) {
	givenUpOn, cancel := context.WithCancel(context.Background())
	cancel()
	cases := map[string]struct {
		result resultSet
		want   bool
	}{
		"a writable profile that finished": {resultSet{writable: true, ctx: context.Background()}, true},
		"a read only profile":              {resultSet{writable: false, ctx: context.Background()}, false},
		"a statement that failed":          {resultSet{writable: true, ctx: context.Background(), err: errFailed}, false},
		"a run that was given up on":       {resultSet{writable: true, ctx: givenUpOn}, false},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			if got := test.result.commits(); got != test.want {
				t.Errorf("commits() = %v, want %v", got, test.want)
			}
		})
	}
}

var errFailed = errors.New("the statement failed")

func TestQueryStopsAtTheRowLimit(t *testing.T) {
	config := readOnlyConfig()
	config.RowLimit = 2
	conn, mock := mocked(t, config)
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id, email FROM users").WillReturnRows(threeRows())
	mock.ExpectRollback()

	result, err := conn.Query(context.Background(), "SELECT id, email FROM users")
	if err != nil {
		t.Fatalf("Query() = %v", err)
	}
	if rows := drain(result); len(rows) != 2 {
		t.Errorf("rows = %v, want the cap to hold", rows)
	}
	if !result.Truncated() {
		t.Error("a result that stopped early must say so")
	}
	if err := driver.Finish(result); err != nil {
		t.Errorf("Finish() = %v", err)
	}
}

func TestStreamReadsEveryRow(t *testing.T) {
	config := readOnlyConfig()
	config.RowLimit = 2
	conn, mock := mocked(t, config)
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id, email FROM users").WillReturnRows(threeRows())
	mock.ExpectRollback()

	result, err := conn.Stream(context.Background(), "SELECT id, email FROM users")
	if err != nil {
		t.Fatalf("Stream() = %v", err)
	}
	if rows := drain(result); len(rows) != 3 {
		t.Errorf("rows = %v, want no cap at all", rows)
	}
	if err := driver.Finish(result); err != nil {
		t.Errorf("Finish() = %v", err)
	}
}

func TestQueryReportsWhatWentWrong(t *testing.T) {
	t.Run("a transaction that will not start", func(t *testing.T) {
		conn, mock := mocked(t, readOnlyConfig())
		mock.ExpectBegin().WillReturnError(errRefused)
		if _, err := conn.Query(context.Background(), "SELECT 1"); err == nil {
			t.Fatal("want an error")
		}
	})
	t.Run("a statement the server refused", func(t *testing.T) {
		conn, mock := mocked(t, readOnlyConfig())
		mock.ExpectBegin()
		mock.ExpectQuery("SELECT 1").WillReturnError(errRefused)
		mock.ExpectRollback()
		if _, err := conn.Query(context.Background(), "SELECT 1"); err == nil {
			t.Fatal("want an error")
		}
	})
	t.Run("a row that will not be read", func(t *testing.T) {
		conn, mock := mocked(t, readOnlyConfig())
		rows := sqlmock.NewRows([]string{"id"}).AddRow(int64(1)).RowError(0, errRefused)
		mock.ExpectBegin()
		mock.ExpectQuery("SELECT id FROM users").WillReturnRows(rows)
		mock.ExpectRollback()
		result, err := conn.Query(context.Background(), "SELECT id FROM users")
		if err != nil {
			t.Fatalf("Query() = %v", err)
		}
		drain(result)
		if driver.Finish(result) == nil {
			t.Fatal("a row that could not be read must reach the caller")
		}
	})
	t.Run("a transaction that will not finish", func(t *testing.T) {
		conn, mock := mocked(t, writableConfig())
		mock.ExpectBegin()
		mock.ExpectQuery("SELECT 1").WillReturnRows(sqlmock.NewRows([]string{"a"}))
		mock.ExpectCommit().WillReturnError(errRefused)
		result, err := conn.Query(context.Background(), "SELECT 1")
		if err != nil {
			t.Fatalf("Query() = %v", err)
		}
		drain(result)
		if result.Close() == nil {
			t.Fatal("work that was not kept must reach the caller")
		}
	})
}

func TestAStatementIsGivenADeadlineOfItsOwn(t *testing.T) {
	config := readOnlyConfig()
	config.Timeouts.Statement = 50 * time.Millisecond
	conn, mock := mocked(t, config)
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT 1").WillReturnRows(sqlmock.NewRows([]string{"a"}).AddRow(int64(1)))
	mock.ExpectRollback()

	result, err := conn.Query(context.Background(), "SELECT 1")
	if err != nil {
		t.Fatalf("Query() = %v", err)
	}
	drain(result)
	if err := driver.Finish(result); err != nil {
		t.Errorf("Finish() = %v", err)
	}
}

func TestValuesLeaveTheDriverAsThingsAScreenKnows(t *testing.T) {
	identifier := []byte{0x21, 0x43, 0x65, 0x87, 0x21, 0x43, 0x21, 0x43, 0x89, 0xab, 0xcd, 0xef, 0x01, 0x23, 0x45, 0x67}
	cases := []struct {
		name  string
		value any
		kind  string
		want  any
	}{
		{"a number too exact for a float", []byte("12.34"), "DECIMAL", "12.34"},
		{"money", []byte("9.99"), "MONEY", "9.99"},
		{"a document", []byte("<a/>"), "XML", "<a/>"},
		{"a unique identifier", identifier, "UNIQUEIDENTIFIER", "87654321-4321-4321-89AB-CDEF01234567"},
		{"an identifier of the wrong length", []byte{0x01}, "UNIQUEIDENTIFIER", []byte{0x01}},
		{"bytes that are bytes", []byte{0x01, 0x02}, "VARBINARY", []byte{0x01, 0x02}},
		{"a number", int64(7), "INT", int64(7)},
		{"nothing at all", nil, "INT", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := native(c.value, c.kind)
			if text, ok := c.want.(string); ok {
				if got != text {
					t.Errorf("native() = %#v, want %q", got, text)
				}
				return
			}
			if bytes, ok := c.want.([]byte); ok {
				if string(got.([]byte)) != string(bytes) {
					t.Errorf("native() = %#v, want %#v", got, bytes)
				}
				return
			}
			if got != c.want {
				t.Errorf("native() = %#v, want %#v", got, c.want)
			}
		})
	}
	if got := natives([]any{int64(1)}, nil); len(got) != 1 || got[0] != int64(1) {
		t.Errorf("natives() without column types = %#v", got)
	}
}

func TestAResultCarriesTheTypesItsValuesWereSentAs(t *testing.T) {
	conn, mock := mocked(t, readOnlyConfig())
	rows := sqlmock.NewRowsWithColumnDefinition(
		sqlmock.NewColumn("total").OfType("DECIMAL", ""),
	).AddRow([]byte("10.50"))
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT total FROM orders").WillReturnRows(rows)
	mock.ExpectRollback()

	result, err := conn.Query(context.Background(), "SELECT total FROM orders")
	if err != nil {
		t.Fatalf("Query() = %v", err)
	}
	values := drain(result)
	if len(values) != 1 || values[0][0] != "10.50" {
		t.Errorf("values = %#v, want the digits that were sent", values)
	}
	if err := driver.Finish(result); err != nil {
		t.Errorf("Finish() = %v", err)
	}
}

func TestRowLimitFallsBackToTheDefault(t *testing.T) {
	if rowLimit(0) != defaultRowLimit || rowLimit(-1) != defaultRowLimit {
		t.Error("a profile with no cap of its own gets the default")
	}
	if rowLimit(25) != 25 {
		t.Error("a profile that names a cap keeps it")
	}
}

func TestPreviewQuotesTheName(t *testing.T) {
	conn, _ := mocked(t, readOnlyConfig())
	if got := conn.Preview("dbo", "users"); got != "SELECT * FROM [dbo].[users]" {
		t.Errorf("Preview() = %q", got)
	}
	if got := conn.Preview("", "users"); got != "SELECT * FROM [users]" {
		t.Errorf("Preview() = %q", got)
	}
}

func TestQuoteClosesWhatItOpens(t *testing.T) {
	if got := Quote("users"); got != "[users]" {
		t.Errorf("Quote() = %q", got)
	}
	if got := Quote("odd]name"); got != "[odd]]name]" {
		t.Errorf("Quote() = %q", got)
	}
}

func TestNextStopsAtTheFirstRowItCannotRead(t *testing.T) {
	conn, mock := mocked(t, readOnlyConfig())
	rows := sqlmock.NewRows([]string{"id"}).AddRow("not a number").AddRow(int64(2))
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id FROM users").WillReturnRows(rows)
	mock.ExpectRollback()

	result, err := conn.Query(context.Background(), "SELECT id FROM users")
	if err != nil {
		t.Fatalf("Query() = %v", err)
	}
	drain(result)
	if result.Next() {
		t.Error("a result that has already failed reads no further rows")
	}
	_ = result.Close()
}
