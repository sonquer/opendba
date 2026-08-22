package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/pashagolub/pgxmock/v5"

	"github.com/sonquer/tui4db/src/cli/internal/driver"
)

func TestQuery(t *testing.T) {
	conn, pool := mocked(t, readOnlyConfig())
	pool.ExpectBegin()
	pool.ExpectQuery("SELECT id, email FROM users").WillReturnRows(
		pgxmock.NewRows([]string{"id", "email"}).
			AddRow(int64(1), "a@example.com").
			AddRow(int64(2), "b@example.com"))
	pool.ExpectRollback()

	result, err := conn.Query(context.Background(), "SELECT id, email FROM users")
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if got := result.Columns(); len(got) != 2 || got[0] != "id" {
		t.Fatalf("columns = %v", got)
	}
	rows := 0
	for result.Next() {
		if len(result.Values()) != 2 {
			t.Fatalf("values = %v", result.Values())
		}
		rows++
	}
	if err := result.Err(); err != nil {
		t.Fatalf("Err: %v", err)
	}
	if rows != 2 || result.Truncated() {
		t.Errorf("rows = %d truncated = %v", rows, result.Truncated())
	}
	if result.Duration() <= 0 {
		t.Error("the query must be timed")
	}
	if err := result.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestQueryStopsAtTheRowLimit(t *testing.T) {
	config := readOnlyConfig()
	config.RowLimit = 1
	conn, pool := mocked(t, config)
	pool.ExpectBegin()
	pool.ExpectQuery("SELECT").WillReturnRows(
		pgxmock.NewRows([]string{"id"}).AddRow(int64(1)).AddRow(int64(2)))
	pool.ExpectRollback()

	result, err := conn.Query(context.Background(), "SELECT id FROM users")
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	rows := 0
	for result.Next() {
		rows++
	}
	if rows != 1 || !result.Truncated() {
		t.Fatalf("rows = %d truncated = %v", rows, result.Truncated())
	}
	if err := result.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestQueryReportsFailures(t *testing.T) {
	t.Run("transaction", func(t *testing.T) {
		conn, pool := mocked(t, readOnlyConfig())
		pool.ExpectBegin().WillReturnError(errors.New("connection reset"))
		if _, err := conn.Query(context.Background(), "SELECT 1"); err == nil {
			t.Fatal("want an error")
		}
	})
	t.Run("statement", func(t *testing.T) {
		conn, pool := mocked(t, readOnlyConfig())
		pool.ExpectBegin()
		pool.ExpectQuery("SELECT").WillReturnError(errors.New("relation does not exist"))
		pool.ExpectRollback()
		if _, err := conn.Query(context.Background(), "SELECT * FROM missing"); err == nil {
			t.Fatal("want an error")
		}
	})
}

func TestResultStopsAfterAnError(t *testing.T) {
	result := &resultSet{limit: 10, err: context.Canceled}
	if result.Next() || result.Truncated() {
		t.Fatal("a failed result produces nothing")
	}
}

func TestExplainStatement(t *testing.T) {
	plain := ExplainStatement("SELECT 1", false)
	if !strings.HasPrefix(plain, "EXPLAIN (FORMAT JSON") || strings.Contains(plain, "ANALYZE") {
		t.Errorf("statement = %q", plain)
	}
	analyzed := ExplainStatement("SELECT 1", true)
	if !strings.Contains(analyzed, "ANALYZE") || !strings.Contains(analyzed, "BUFFERS") {
		t.Errorf("statement = %q", analyzed)
	}
}

const planJSON = `[{"Plan":{"Node Type":"Hash Join","Join Type":"Left","Total Cost":124.2,"Plan Rows":1200,
	"Plans":[
		{"Node Type":"Seq Scan","Relation Name":"users","Total Cost":18.2,"Plan Rows":800,"Actual Rows":790,"Actual Total Time":12.5},
		{"Node Type":"Index Scan","Relation Name":"orders","Index Name":"orders_user_id_idx","Total Cost":42.1,"Plan Rows":400}
	]}}]`

func TestParsePlan(t *testing.T) {
	plan, err := ParsePlan(planJSON)
	if err != nil {
		t.Fatalf("ParsePlan: %v", err)
	}
	if plan.Root.Name != "Hash Join" || plan.Total != 124.2 {
		t.Fatalf("root = %+v", plan.Root)
	}
	if !strings.Contains(plan.Root.Detail, "left join") {
		t.Errorf("detail = %q", plan.Root.Detail)
	}
	if len(plan.Root.Children) != 2 {
		t.Fatalf("children = %+v", plan.Root.Children)
	}
	scan := plan.Root.Children[0]
	if scan.Depth != 1 || scan.Rows != 790 {
		t.Errorf("the measured row count must win: %+v", scan)
	}
	if scan.Duration == 0 {
		t.Error("measured time must be kept")
	}
	index := plan.Root.Children[1]
	if !strings.Contains(index.Detail, "using orders_user_id_idx") || !strings.Contains(index.Detail, "on orders") {
		t.Errorf("detail = %q", index.Detail)
	}
	for _, want := range []string{"Hash Join", "  Seq Scan on users", "  Index Scan using orders_user_id_idx"} {
		if !strings.Contains(plan.Text, want) {
			t.Errorf("plan text missing %q:\n%s", want, plan.Text)
		}
	}
}

func TestParsePlanRejectsBrokenDocuments(t *testing.T) {
	if _, err := ParsePlan("not json"); err == nil {
		t.Error("want a parse error")
	}
	if _, err := ParsePlan("[]"); err == nil {
		t.Error("an empty plan must be reported")
	}
}

func TestExplain(t *testing.T) {
	conn, pool := mocked(t, readOnlyConfig())
	pool.ExpectQuery("EXPLAIN").WillReturnRows(pgxmock.NewRows([]string{"plan"}).AddRow(planJSON))

	plan, err := conn.Explain(context.Background(), "SELECT 1", false)
	if err != nil {
		t.Fatalf("Explain: %v", err)
	}
	if plan.Root.Name != "Hash Join" {
		t.Errorf("plan = %+v", plan.Root)
	}
}

func TestExplainReportsFailures(t *testing.T) {
	conn, pool := mocked(t, readOnlyConfig())
	pool.ExpectQuery("EXPLAIN").WillReturnError(errors.New("syntax error"))
	if _, err := conn.Explain(context.Background(), "SELECT", false); err == nil {
		t.Fatal("want an error")
	}
}

func TestRowLimitFallsBackToTheDefault(t *testing.T) {
	if rowLimit(0) != defaultRowLimit || rowLimit(-5) != defaultRowLimit {
		t.Error("an unset row limit must fall back to the default")
	}
	if rowLimit(10) != 10 {
		t.Error("a configured row limit must be used")
	}
}

func TestClose(t *testing.T) {
	conn, _ := mocked(t, readOnlyConfig())
	if err := conn.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func healthySnapshot() Snapshot {
	return Snapshot{
		Connections:    42,
		MaxConnections: 100,
		CacheHitRatio:  99.4,
		ReadOnly:       true,
	}
}

func findingByCode(findings []driver.Finding, code string) driver.Finding {
	for _, finding := range findings {
		if finding.Code == code {
			return finding
		}
	}
	return driver.Finding{}
}

func TestHealthyServerHasNoWarnings(t *testing.T) {
	for _, finding := range Findings(healthySnapshot()) {
		if finding.Severity != driver.SeverityOK {
			t.Errorf("a healthy server must not warn: %+v", finding)
		}
	}
}

func TestFindingThresholds(t *testing.T) {
	cases := []struct {
		name     string
		mutate   func(*Snapshot)
		code     string
		severity driver.Severity
	}{
		{"idle in transaction", func(s *Snapshot) { s.IdleInTx = 4 }, "connections", driver.SeverityWarn},
		{"connection slots", func(s *Snapshot) { s.Connections = 95 }, "connections", driver.SeverityCritical},
		{"cache misses", func(s *Snapshot) { s.CacheHitRatio = 95 }, "cache_hit_ratio", driver.SeverityWarn},
		{"cold cache", func(s *Snapshot) { s.CacheHitRatio = 60 }, "cache_hit_ratio", driver.SeverityCritical},
		{"blocked", func(s *Snapshot) { s.WaitingLocks = 3 }, "waiting_locks", driver.SeverityCritical},
		{"rollbacks", func(s *Snapshot) { s.RollbackRatio = 12 }, "rollback_ratio", driver.SeverityWarn},
		{"unused indexes", func(s *Snapshot) { s.UnusedIndexes = 27; s.UnusedIndexSize = 46170898432 }, "unused_indexes", driver.SeverityWarn},
		{"long queries", func(s *Snapshot) { s.LongQueries = 2 }, "long_running", driver.SeverityWarn},
		{"inactive slots", func(s *Snapshot) { s.InactiveSlots = 1 }, "inactive_slots", driver.SeverityCritical},
		{"writable session", func(s *Snapshot) { s.ReadOnly = false }, "transaction_read_only", driver.SeverityWarn},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			snapshot := healthySnapshot()
			c.mutate(&snapshot)
			finding := findingByCode(Findings(snapshot), c.code)
			if finding.Severity != c.severity {
				t.Fatalf("finding = %+v, want %v", finding, c.severity)
			}
			if finding.Severity != driver.SeverityOK && finding.Note == "" {
				t.Error("a finding that is not healthy must explain itself")
			}
		})
	}
}

func TestUnusedIndexSizeIsReadable(t *testing.T) {
	snapshot := healthySnapshot()
	snapshot.UnusedIndexes = 27
	snapshot.UnusedIndexSize = 46170898432
	finding := findingByCode(Findings(snapshot), "unused_indexes")
	if finding.Value != "43.0 GiB" {
		t.Errorf("value = %q", finding.Value)
	}
}

func TestHealth(t *testing.T) {
	conn, pool := mocked(t, readOnlyConfig())
	pool.ExpectQuery("pg_stat_activity").WillReturnRows(
		pgxmock.NewRows([]string{"connections", "max", "idle", "cache", "locks", "rollbacks", "index_size", "indexes", "long", "slots", "read_only"}).
			AddRow(int64(42), int64(100), int64(0), 99.4, int64(0), 0.4, int64(0), int64(0), int64(0), int64(0), true))

	findings, err := conn.Health(context.Background())
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	if len(findings) != 8 {
		t.Fatalf("findings = %+v", findings)
	}
	if findingByCode(findings, "cache_hit_ratio").Value != "99.4%" {
		t.Errorf("cache = %+v", findingByCode(findings, "cache_hit_ratio"))
	}
}

func TestHealthReportsFailures(t *testing.T) {
	conn, pool := mocked(t, readOnlyConfig())
	pool.ExpectQuery("pg_stat_activity").WillReturnError(errors.New("permission denied"))
	if _, err := conn.Health(context.Background()); err == nil {
		t.Fatal("want an error")
	}
}

func TestByteSize(t *testing.T) {
	cases := map[int64]string{0: "0 B", 900: "900 B", 2048: "2.0 KiB", 46170898432: "43.0 GiB"}
	for bytes, want := range cases {
		if got := ByteSize(bytes); got != want {
			t.Errorf("ByteSize(%d) = %q, want %q", bytes, got, want)
		}
	}
}
