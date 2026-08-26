package mssql

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/sonquer/opendba/src/cli/internal/driver"
)

func indexRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{"schema", "table", "name", "unique", "primary", "kind", "filter", "size"}).
		AddRow("dbo", "users", "PK_users", true, true, "CLUSTERED", "", int64(8192)).
		AddRow("dbo", "users", "IX_users_email", false, false, "NONCLUSTERED", "([email] IS NOT NULL)", int64(4096))
}

func indexColumnRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{"schema", "table", "index", "column", "descending", "included"}).
		AddRow("dbo", "users", "PK_users", "id", false, false).
		AddRow("dbo", "users", "IX_users_email", "email", false, false).
		AddRow("dbo", "users", "IX_users_email", "created_at", true, false).
		AddRow("dbo", "users", "IX_users_email", "name", false, true)
}

func TestIndexes(t *testing.T) {
	conn, mock := mocked(t, readOnlyConfig())
	mock.ExpectQuery(indexesQuery).WithArgs("dbo").WillReturnRows(indexRows())
	mock.ExpectQuery(indexColumnsQuery).WithArgs("dbo").WillReturnRows(indexColumnRows())
	mock.ExpectQuery(indexUsageQuery).WithArgs("dbo").WillReturnRows(
		sqlmock.NewRows([]string{"schema", "table", "index", "scans"}).
			AddRow("dbo", "users", "PK_users", int64(4200)))

	indexes, err := conn.Indexes(context.Background(), "dbo")
	if err != nil {
		t.Fatalf("Indexes() = %v", err)
	}
	if len(indexes) != 2 || indexes[0].Name != "PK_users" || !indexes[0].Primary {
		t.Errorf("indexes = %+v", indexes)
	}
	if indexes[0].Size != 8192 || indexes[0].Scans != 4200 || !indexes[0].Stats {
		t.Errorf("index = %+v", indexes[0])
	}
	if !indexes[1].Idle() {
		t.Error("an index nothing has read is idle")
	}
	want := "CREATE NONCLUSTERED INDEX [IX_users_email] ON [dbo].[users] ([email] ASC, [created_at] DESC) " +
		"INCLUDE ([name]) WHERE ([email] IS NOT NULL)"
	if indexes[1].Definition != want {
		t.Errorf("definition = %q,\nwant %q", indexes[1].Definition, want)
	}
	if indexes[0].Definition != "CREATE UNIQUE CLUSTERED INDEX [PK_users] ON [dbo].[users] ([id] ASC)" {
		t.Errorf("definition = %q", indexes[0].Definition)
	}
}

func TestIndexesLeaveTheCountersUnmeasuredWhenTheyAreRefused(t *testing.T) {
	conn, mock := mocked(t, readOnlyConfig())
	mock.ExpectQuery(indexesQuery).WithArgs("").WillReturnRows(indexRows())
	mock.ExpectQuery(indexColumnsQuery).WithArgs("").WillReturnRows(indexColumnRows())
	mock.ExpectQuery(indexUsageQuery).WithArgs("").WillReturnError(errRefused)

	indexes, err := conn.Indexes(context.Background(), "")
	if err != nil {
		t.Fatalf("a refused counter must not fail the listing: %v", err)
	}
	if indexes[0].Stats || indexes[0].Scans != unmeasured || indexes[0].Rows != unmeasured {
		t.Errorf("what was not measured is negative, never zero: %+v", indexes[0])
	}
	if indexes[1].Idle() {
		t.Error("an index whose reads were never counted is not known to be idle")
	}
}

func TestIndexesReportsWhatItCannotRead(t *testing.T) {
	t.Run("the indexes", func(t *testing.T) {
		conn, mock := mocked(t, readOnlyConfig())
		mock.ExpectQuery(indexesQuery).WithArgs("dbo").WillReturnError(errRefused)
		if _, err := conn.Indexes(context.Background(), "dbo"); err == nil {
			t.Fatal("want an error")
		}
	})
	t.Run("their columns", func(t *testing.T) {
		conn, mock := mocked(t, readOnlyConfig())
		mock.ExpectQuery(indexesQuery).WithArgs("dbo").WillReturnRows(indexRows())
		mock.ExpectQuery(indexColumnsQuery).WithArgs("dbo").WillReturnError(errRefused)
		if _, err := conn.Indexes(context.Background(), "dbo"); err == nil {
			t.Fatal("want an error")
		}
	})
}

func TestIndexDefinitionOfAnIndexWithNothingToSay(t *testing.T) {
	got := IndexDefinition(driver.Index{Schema: "dbo", Table: "t", Name: "ix"}, "", "", nil)
	if got != "CREATE INDEX [ix] ON [dbo].[t] ()" {
		t.Errorf("definition = %q", got)
	}
}
