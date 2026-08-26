package tool

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/sonquer/opendba/src/cli/internal/ai"
	"github.com/sonquer/opendba/src/cli/internal/driver"
	"github.com/sonquer/opendba/src/cli/pkg/sqldialect"
	"github.com/sonquer/opendba/src/cli/pkg/sqlguard"
)

type fakeResult struct {
	columns   []string
	rows      [][]any
	at        int
	err       error
	truncated bool
	closed    int
}

func (f *fakeResult) Columns() []string { return f.columns }

func (f *fakeResult) Next() bool {
	if f.at >= len(f.rows) {
		return false
	}
	f.at++
	return true
}

func (f *fakeResult) Values() []any { return f.rows[f.at-1] }

func (f *fakeResult) Err() error { return f.err }

func (f *fakeResult) Truncated() bool { return f.truncated }

func (f *fakeResult) Duration() time.Duration { return time.Millisecond }

func (f *fakeResult) Close() error {
	f.closed++
	return nil
}

type fakeDatabase struct {
	schemas   []driver.Schema
	tables    []driver.Table
	columns   []driver.Column
	relations []driver.Relation
	indexes   []driver.Index
	findings  []driver.Finding
	result    *fakeResult
	plan      driver.Plan

	err       error
	statement string
	asked     string
}

func (f *fakeDatabase) Schemas(context.Context) ([]driver.Schema, error) {
	return f.schemas, f.err
}

func (f *fakeDatabase) Tables(_ context.Context, schema string) ([]driver.Table, error) {
	f.asked = schema
	return f.tables, f.err
}

func (f *fakeDatabase) Columns(_ context.Context, schema, table string) ([]driver.Column, error) {
	f.asked = schema + "." + table
	return f.columns, f.err
}

func (f *fakeDatabase) Relations(context.Context, string, string) ([]driver.Relation, error) {
	return f.relations, f.err
}

func (f *fakeDatabase) Indexes(_ context.Context, schema string) ([]driver.Index, error) {
	f.asked = schema
	return f.indexes, f.err
}

func (f *fakeDatabase) Health(context.Context) ([]driver.Finding, error) {
	return f.findings, f.err
}

func (f *fakeDatabase) Query(_ context.Context, sql string) (driver.ResultSet, error) {
	f.statement = sql
	if f.err != nil {
		return nil, f.err
	}
	return f.result, nil
}

func (f *fakeDatabase) Explain(_ context.Context, sql string, _ bool) (driver.Plan, error) {
	f.statement = sql
	return f.plan, f.err
}

func everything() driver.Capabilities {
	return driver.Capabilities{Explain: true, Relations: true, Health: true}
}

func set(db Database, mode sqlguard.Mode, caps driver.Capabilities) *Set {
	return New(db, sqlguard.New(sqldialect.SQLite()), mode, 3, caps)
}

func call(name string, arguments map[string]any) ai.ToolCall {
	if arguments == nil {
		arguments = map[string]any{}
	}
	return ai.ToolCall{ID: "call_1", Name: name, Arguments: arguments}
}

// dataRows counts the lines of a rendered table that hold data rather than the
// header, the rule, or the note that follows it.
func dataRows(content string) int {
	body, _, _ := strings.Cut(content, "\n\n")
	lines := strings.Split(body, "\n")
	if len(lines) < 2 {
		return 0
	}
	return len(lines) - 2
}

func TestDefinitions(t *testing.T) {
	full := set(&fakeDatabase{}, sqlguard.ModeReadOnly, everything()).Definitions()
	names := map[string]bool{}
	for _, tool := range full {
		names[tool.Name] = true
	}
	for _, want := range []string{ListSchemas, ListTables, DescribeTable, ListIndexes, RunSelect, HealthFindings, ExplainQuery} {
		if !names[want] {
			t.Fatalf("the tool %q was not offered", want)
		}
	}

	bare := set(&fakeDatabase{}, sqlguard.ModeReadOnly, driver.Capabilities{}).Definitions()
	for _, tool := range bare {
		if tool.Name == ExplainQuery || tool.Name == HealthFindings {
			t.Fatalf("the tool %q was offered by a driver that cannot answer it", tool.Name)
		}
	}
	if len(bare) != len(full)-2 {
		t.Fatalf("offered %d tools, want two fewer than the full set", len(bare))
	}
}

func TestRunSelectIsGuarded(t *testing.T) {
	cases := map[string]struct {
		mode      sqlguard.Mode
		statement string
		ran       bool
	}{
		"a read runs": {
			mode:      sqlguard.ModeReadOnly,
			statement: "SELECT count(*) FROM orders",
			ran:       true,
		},
		"a delete is refused on a read only connection": {
			mode:      sqlguard.ModeReadOnly,
			statement: "DELETE FROM orders",
		},
		"a delete is refused even where writing is allowed": {
			mode:      sqlguard.ModeReadWrite,
			statement: "DELETE FROM orders",
		},
		"a write hidden in a common table expression is refused": {
			mode:      sqlguard.ModeReadWrite,
			statement: "WITH t AS (DELETE FROM orders RETURNING *) SELECT * FROM t",
		},
		"two statements are refused": {
			mode:      sqlguard.ModeReadOnly,
			statement: "SELECT 1; SELECT 2",
		},
		"nonsense is refused": {
			mode:      sqlguard.ModeReadOnly,
			statement: "this is not sql",
		},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			db := &fakeDatabase{result: &fakeResult{columns: []string{"count"}, rows: [][]any{{int64(4)}}}}
			result := set(db, test.mode, everything()).Call(context.Background(), call(RunSelect, map[string]any{"statement": test.statement}))

			if test.ran {
				if result.Failed {
					t.Fatalf("the statement was refused: %s", result.Content)
				}
				if db.statement != test.statement {
					t.Fatalf("the driver was sent %q", db.statement)
				}
				return
			}
			if !result.Failed {
				t.Fatalf("the statement was allowed through: %s", result.Content)
			}
			if db.statement != "" {
				t.Fatalf("a refused statement reached the driver as %q", db.statement)
			}
			if !strings.HasPrefix(result.Content, "refused:") {
				t.Fatalf("content = %q, want it to say it was refused and why", result.Content)
			}
		})
	}
}

func TestRunSelectLimitsTheRows(t *testing.T) {
	rows := [][]any{{1}, {2}, {3}, {4}, {5}}
	db := &fakeDatabase{result: &fakeResult{columns: []string{"n"}, rows: rows}}
	result := set(db, sqlguard.ModeReadOnly, everything()).Call(context.Background(),
		call(RunSelect, map[string]any{"statement": "SELECT n FROM numbers"}))

	if result.Failed {
		t.Fatalf("the statement failed: %s", result.Content)
	}
	if got := dataRows(result.Content); got != 3 {
		t.Fatalf("content = %q, want three rows", result.Content)
	}
	if !strings.Contains(result.Content, "there may be more") {
		t.Fatal("the model was not told the rows were cut short")
	}
	if db.result.closed != 1 {
		t.Fatalf("the result was closed %d times, want once", db.result.closed)
	}
}

func TestRunSelectHonoursASmallerLimit(t *testing.T) {
	db := &fakeDatabase{result: &fakeResult{columns: []string{"n"}, rows: [][]any{{1}, {2}, {3}}}}
	result := set(db, sqlguard.ModeReadOnly, everything()).Call(context.Background(),
		call(RunSelect, map[string]any{"statement": "SELECT n FROM numbers", "limit": float64(1)}))

	if got := dataRows(result.Content); got != 1 {
		t.Fatalf("content = %q, want one row", result.Content)
	}
}

func TestRunSelectReportsFailures(t *testing.T) {
	cases := map[string]struct {
		db   *fakeDatabase
		want string
	}{
		"no statement": {
			db:   &fakeDatabase{},
			want: "is required",
		},
		"the server refused": {
			db:   &fakeDatabase{err: errors.New("no such table")},
			want: "run the statement",
		},
		"the rows went wrong": {
			db:   &fakeDatabase{result: &fakeResult{columns: []string{"n"}, rows: [][]any{{1}}, err: errors.New("connection lost")}},
			want: "read the rows",
		},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			statement := "SELECT n FROM numbers"
			if test.want == "is required" {
				statement = "   "
			}
			result := set(test.db, sqlguard.ModeReadOnly, everything()).Call(context.Background(),
				call(RunSelect, map[string]any{"statement": statement}))
			if !result.Failed || !strings.Contains(result.Content, test.want) {
				t.Fatalf("result = %+v, want it to fail with %q", result, test.want)
			}
		})
	}
}

func TestListSchemas(t *testing.T) {
	db := &fakeDatabase{schemas: []driver.Schema{
		{Name: "main", Tables: 2},
		{Name: "pg_catalog", Tables: 60, System: true},
	}}
	result := set(db, sqlguard.ModeReadOnly, everything()).Call(context.Background(), call(ListSchemas, nil))

	for _, want := range []string{"schema | tables | system", "main | 2 | no", "pg_catalog | 60 | yes"} {
		if !strings.Contains(result.Content, want) {
			t.Fatalf("content = %q, want %q in it", result.Content, want)
		}
	}
}

func TestListTables(t *testing.T) {
	db := &fakeDatabase{tables: []driver.Table{
		{Schema: "main", Name: "orders", Kind: "table", Rows: 12, Size: 4096, Comment: "the orders"},
		{Schema: "main", Name: "users", Kind: "table", Rows: -1, Size: -1},
	}}
	result := set(db, sqlguard.ModeReadOnly, everything()).Call(context.Background(),
		call(ListTables, map[string]any{"schema": "main"}))

	if db.asked != "main" {
		t.Fatalf("the driver was asked for %q", db.asked)
	}
	if !strings.Contains(result.Content, "main | orders | table | 12 | 4 KiB | the orders") {
		t.Fatalf("content = %q", result.Content)
	}
	if !strings.Contains(result.Content, "| n/a | n/a |") {
		t.Fatal("a number the driver could not take was reported as zero rather than as unknown")
	}
}

func TestDescribeTable(t *testing.T) {
	db := &fakeDatabase{
		columns: []driver.Column{
			{Name: "id", Type: "integer", PrimaryKey: true},
			{Name: "customer", Type: "text", Nullable: true, ForeignKey: "main.users(id)"},
		},
		relations: []driver.Relation{{
			Name: "orders_customer_fkey", FromColumns: []string{"customer"},
			ToSchema: "main", ToTable: "users", ToColumns: []string{"id"},
		}},
	}
	result := set(db, sqlguard.ModeReadOnly, everything()).Call(context.Background(),
		call(DescribeTable, map[string]any{"schema": "main", "table": "orders"}))

	for _, want := range []string{"main.orders", "id | integer | no | yes", "orders_customer_fkey"} {
		if !strings.Contains(result.Content, want) {
			t.Fatalf("content = %q, want %q in it", result.Content, want)
		}
	}
}

func TestDescribeTableWithoutRelations(t *testing.T) {
	db := &fakeDatabase{
		columns:   []driver.Column{{Name: "id", Type: "integer"}},
		relations: []driver.Relation{{Name: "never asked for"}},
	}
	result := set(db, sqlguard.ModeReadOnly, driver.Capabilities{}).Call(context.Background(),
		call(DescribeTable, map[string]any{"table": "orders"}))

	if strings.Contains(result.Content, "never asked for") {
		t.Fatal("a driver that cannot report relations was asked for them anyway")
	}
	if !strings.Contains(result.Content, "orders") {
		t.Fatalf("content = %q", result.Content)
	}
}

func TestDescribeTableFailures(t *testing.T) {
	cases := map[string]struct {
		db    *fakeDatabase
		table string
		want  string
	}{
		"no table named": {db: &fakeDatabase{}, table: "", want: "is required"},
		"no such table":  {db: &fakeDatabase{}, table: "nope", want: "was found"},
		"unreadable":     {db: &fakeDatabase{err: errors.New("permission denied")}, table: "orders", want: "read the columns"},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			result := set(test.db, sqlguard.ModeReadOnly, everything()).Call(context.Background(),
				call(DescribeTable, map[string]any{"table": test.table}))
			if !result.Failed || !strings.Contains(result.Content, test.want) {
				t.Fatalf("result = %+v, want it to fail with %q", result, test.want)
			}
		})
	}
}

func TestListIndexesAndHealth(t *testing.T) {
	db := &fakeDatabase{
		indexes: []driver.Index{{
			Schema: "main", Table: "orders", Name: "orders_pkey",
			Size: 2048, Scans: 10, Unique: true, Primary: true,
		}},
		findings: []driver.Finding{{
			Group: "storage", Subsystem: "integrity", Code: "integrity_check",
			Severity: driver.SeverityOK, Value: "ok", Note: "nothing is wrong",
		}},
	}
	tools := set(db, sqlguard.ModeReadOnly, everything())

	indexes := tools.Call(context.Background(), call(ListIndexes, map[string]any{"schema": "main"}))
	if !strings.Contains(indexes.Content, "orders_pkey | 2 KiB | 10 | yes | yes") {
		t.Fatalf("content = %q", indexes.Content)
	}
	health := tools.Call(context.Background(), call(HealthFindings, nil))
	if !strings.Contains(health.Content, "integrity_check") {
		t.Fatalf("content = %q", health.Content)
	}
}

func TestExplain(t *testing.T) {
	db := &fakeDatabase{plan: driver.Plan{Root: driver.PlanNode{
		Name: "SCAN orders", Detail: "using index", Rows: 12,
		Children: []driver.PlanNode{{Name: "SEARCH users"}},
	}}}
	result := set(db, sqlguard.ModeReadOnly, everything()).Call(context.Background(),
		call(ExplainQuery, map[string]any{"statement": "SELECT * FROM orders"}))

	if result.Failed {
		t.Fatalf("explain failed: %s", result.Content)
	}
	if !strings.Contains(result.Content, "SCAN orders (using index) rows=12") {
		t.Fatalf("content = %q", result.Content)
	}
	if !strings.Contains(result.Content, "  SEARCH users") {
		t.Fatal("the plan was not laid out as a tree")
	}
}

func TestExplainIsGuardedToo(t *testing.T) {
	db := &fakeDatabase{}
	result := set(db, sqlguard.ModeReadOnly, everything()).Call(context.Background(),
		call(ExplainQuery, map[string]any{"statement": "DROP TABLE orders"}))

	if !result.Failed {
		t.Fatal("a statement that would change the database was explained rather than refused")
	}
	if db.statement != "" {
		t.Fatalf("a refused statement reached the driver as %q", db.statement)
	}
}

func TestExplainWithNothingToShow(t *testing.T) {
	db := &fakeDatabase{}
	result := set(db, sqlguard.ModeReadOnly, everything()).Call(context.Background(),
		call(ExplainQuery, map[string]any{"statement": "SELECT 1"}))
	if result.Content != "the server returned no plan" {
		t.Fatalf("content = %q", result.Content)
	}
}

func TestExplainReportsAFailure(t *testing.T) {
	db := &fakeDatabase{err: errors.New("not supported")}
	result := set(db, sqlguard.ModeReadOnly, everything()).Call(context.Background(),
		call(ExplainQuery, map[string]any{"statement": "SELECT 1"}))
	if !result.Failed || !strings.Contains(result.Content, "explain the statement") {
		t.Fatalf("result = %+v", result)
	}
}

func TestAToolThatDoesNotExist(t *testing.T) {
	result := set(&fakeDatabase{}, sqlguard.ModeReadOnly, everything()).Call(context.Background(), call("drop_everything", nil))
	if !result.Failed || !strings.Contains(result.Content, "no tool named") {
		t.Fatalf("result = %+v", result)
	}
}

func TestFailuresFromTheDriverAreReported(t *testing.T) {
	broken := &fakeDatabase{err: errors.New("connection lost")}
	tools := set(broken, sqlguard.ModeReadOnly, everything())
	for _, name := range []string{ListSchemas, ListTables, ListIndexes, HealthFindings} {
		t.Run(name, func(t *testing.T) {
			result := tools.Call(context.Background(), call(name, nil))
			if !result.Failed || !strings.Contains(result.Content, "connection lost") {
				t.Fatalf("result = %+v", result)
			}
		})
	}
}

func TestEmptyAnswers(t *testing.T) {
	result := set(&fakeDatabase{}, sqlguard.ModeReadOnly, everything()).Call(context.Background(), call(ListSchemas, nil))
	if result.Content != "nothing to show" {
		t.Fatalf("content = %q", result.Content)
	}
}

func TestCells(t *testing.T) {
	long := strings.Repeat("x", maxCell+10)
	db := &fakeDatabase{result: &fakeResult{
		columns: []string{"a", "b", "c"},
		rows:    [][]any{{nil, "two\nlines", long}, {1}},
	}}
	result := set(db, sqlguard.ModeReadOnly, everything()).Call(context.Background(),
		call(RunSelect, map[string]any{"statement": "SELECT a, b, c FROM t"}))

	if !strings.Contains(result.Content, "null | two lines") {
		t.Fatalf("content = %q, want a null named and a newline flattened", result.Content)
	}
	if !strings.Contains(result.Content, "…") {
		t.Fatal("a very long value was not cut short")
	}
	if !strings.Contains(result.Content, "1 |  | ") {
		t.Fatalf("content = %q, want a short row padded to the width of the header", result.Content)
	}
}

func TestTooManyColumnsAreCutBack(t *testing.T) {
	columns := make([]string, maxColumns+5)
	values := make([]any, maxColumns+5)
	for i := range columns {
		columns[i] = "c"
		values[i] = i
	}
	db := &fakeDatabase{result: &fakeResult{columns: columns, rows: [][]any{values}}}
	result := set(db, sqlguard.ModeReadOnly, everything()).Call(context.Background(),
		call(RunSelect, map[string]any{"statement": "SELECT * FROM wide"}))

	header := strings.Split(result.Content, "\n")[0]
	if got := strings.Count(header, "|"); got != maxColumns-1 {
		t.Fatalf("the header holds %d separators, want the columns cut back to %d", got, maxColumns)
	}
}

func TestTruncatedResultSaysSo(t *testing.T) {
	db := &fakeDatabase{result: &fakeResult{columns: []string{"n"}, rows: [][]any{{1}}, truncated: true}}
	result := set(db, sqlguard.ModeReadOnly, everything()).Call(context.Background(),
		call(RunSelect, map[string]any{"statement": "SELECT n FROM numbers"}))
	if !strings.Contains(result.Content, "there may be more") {
		t.Fatalf("content = %q", result.Content)
	}
}

func TestARowLimitIsAlwaysPositive(t *testing.T) {
	tools := New(&fakeDatabase{result: &fakeResult{columns: []string{"n"}}}, sqlguard.New(sqldialect.SQLite()), sqlguard.ModeReadOnly, 0, everything())
	if tools.rowLimit <= 0 {
		t.Fatalf("rowLimit = %d, want a floor", tools.rowLimit)
	}
}

func TestNumberArgument(t *testing.T) {
	cases := map[string]struct {
		value any
		want  int
	}{
		"json numbers are floats": {value: float64(5), want: 5},
		"a plain int":             {value: 5, want: 5},
		"a string is not a limit": {value: "5", want: 0},
		"nothing at all":          {value: nil, want: 0},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			if got := number(map[string]any{"limit": test.value}, "limit"); got != test.want {
				t.Fatalf("number() = %d, want %d", got, test.want)
			}
		})
	}
}

func TestBytes(t *testing.T) {
	cases := map[int64]string{
		-1:      "n/a",
		0:       "0 B",
		1023:    "1023 B",
		4096:    "4 KiB",
		1 << 20: "1 MiB",
		1 << 30: "1 GiB",
		1 << 40: "1 TiB",
		1 << 50: "1024 TiB",
	}
	for value, want := range cases {
		t.Run(want, func(t *testing.T) {
			if got := bytes(value); got != want {
				t.Fatalf("bytes(%d) = %q, want %q", value, got, want)
			}
		})
	}
}
