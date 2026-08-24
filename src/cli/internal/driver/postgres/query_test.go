package postgres

import (
	"context"
	sqldriver "database/sql/driver"
	"errors"
	"math/big"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/pashagolub/pgxmock/v5"

	"github.com/sonquer/opendba/src/cli/internal/driver"
	"github.com/sonquer/opendba/src/cli/pkg/sqlguard"
)

func TestQuery(t *testing.T) {
	conn, pool := mocked(t, readOnlyConfig())
	pool.ExpectBeginTx(pgx.TxOptions{AccessMode: pgx.ReadOnly})
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
	pool.ExpectBeginTx(pgx.TxOptions{AccessMode: pgx.ReadOnly})
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
		pool.ExpectBeginTx(pgx.TxOptions{AccessMode: pgx.ReadOnly}).WillReturnError(errors.New("connection reset"))
		if _, err := conn.Query(context.Background(), "SELECT 1"); err == nil {
			t.Fatal("want an error")
		}
	})
	t.Run("statement", func(t *testing.T) {
		conn, pool := mocked(t, readOnlyConfig())
		pool.ExpectBeginTx(pgx.TxOptions{AccessMode: pgx.ReadOnly})
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
		CacheHitRatio:    99.4,
		SharedBuffers:    134217728,
		WorkMem:          4194304,
		Connections:      42,
		MaxConnections:   100,
		Active:           2,
		ReadOnly:         true,
		IndexScans:       1000,
		SeqScans:         40,
		LiveTuples:       9000,
		DeadTuples:       100,
		TotalIndexSize:   4096,
		DatabaseSize:     104857600,
		TransactionAge:   1000,
		FreezeMaxAge:     200000000,
		TimedCheckpoints: 40,
		IndexHitRatio:    99.8,
		VacuumSeconds:    3600,
		ServerSize:       209715200,
		Databases:        3,
		WalSize:          67108864,
		MaxWalSize:       1073741824,
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
		{"long queries", func(s *Snapshot) { s.LongestSeconds = 90 }, "long_running", driver.SeverityWarn},
		{"stuck queries", func(s *Snapshot) { s.LongestSeconds = 600 }, "long_running", driver.SeverityCritical},
		{"spilled to disk", func(s *Snapshot) { s.TempFiles = 12; s.TempBytes = 1 << 30 }, "temp_files", driver.SeverityWarn},
		{"deadlocks", func(s *Snapshot) { s.Deadlocks = 3 }, "deadlocks", driver.SeverityWarn},
		{"full scans", func(s *Snapshot) { s.SeqScans = 400 }, "sequential_scans", driver.SeverityWarn},
		{"nothing but full scans", func(s *Snapshot) { s.SeqScans = 4000 }, "sequential_scans", driver.SeverityCritical},
		{"dead rows", func(s *Snapshot) { s.DeadTuples = 2000 }, "dead_tuples", driver.SeverityWarn},
		{"bloat", func(s *Snapshot) { s.DeadTuples = 9000 }, "dead_tuples", driver.SeverityCritical},
		{"freeze behind", func(s *Snapshot) { s.TransactionAge = 150000000 }, "transaction_age", driver.SeverityWarn},
		{"wraparound", func(s *Snapshot) { s.TransactionAge = 195000000 }, "transaction_age", driver.SeverityCritical},
		{"forced checkpoints", func(s *Snapshot) { s.ForcedCheckpoints = 30 }, "forced_checkpoints", driver.SeverityWarn},
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

func TestEveryFindingBelongsToAGroup(t *testing.T) {
	findings := Findings(healthySnapshot())
	if len(findings) < 12 {
		t.Fatalf("the report must cover the server, got %d findings", len(findings))
	}
	for _, finding := range findings {
		if finding.Group == "" {
			t.Errorf("a finding without a group cannot be laid out: %+v", finding)
		}
		if finding.Note == "" {
			t.Errorf("every reading must say what it means in plain words: %+v", finding)
		}
	}
}

// A role that cannot read one view still gets the rest of the report, with the
// refusal shown where the readings would have been.
func TestARefusedGroupDegradesAlone(t *testing.T) {
	snapshot := healthySnapshot()
	snapshot.Refused = map[string]string{driver.GroupScans: "permission denied for view pg_stat_user_tables"}
	findings := Findings(snapshot)

	refusal := findingByCode(findings, "unavailable")
	if refusal.Group != driver.GroupScans || refusal.Severity != driver.SeverityUnknown {
		t.Fatalf("refusal = %+v", refusal)
	}
	if !strings.Contains(refusal.Note, "permission denied") {
		t.Errorf("the reason belongs on the row: %+v", refusal)
	}
	if findingByCode(findings, "sequential_scans").Code != "" {
		t.Error("a refused group has no readings")
	}
	if findingByCode(findings, "cache_hit_ratio").Code == "" {
		t.Error("the other groups must survive")
	}
}

func TestHealth(t *testing.T) {
	conn, pool := mocked(t, readOnlyConfig())
	expectHealth(pool)

	findings, err := conn.Health(context.Background())
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	if len(findings) < 12 {
		t.Fatalf("findings = %+v", findings)
	}
	if findingByCode(findings, "cache_hit_ratio").Value != "99.4%" {
		t.Errorf("cache = %+v", findingByCode(findings, "cache_hit_ratio"))
	}
	if findingByCode(findings, "database_size").Value != "100.0 MiB" {
		t.Errorf("size = %+v", findingByCode(findings, "database_size"))
	}
}

// PostgreSQL 17 moved the checkpoint counters out of pg_stat_bgwriter. An older
// server refuses the newer view and answers the older one.
func TestHealthFallsBackToTheOlderCheckpointView(t *testing.T) {
	conn, pool := mocked(t, readOnlyConfig())
	pool.ExpectQuery("pg_stat_database").WillReturnRows(memoryRows())
	pool.ExpectQuery("pg_stat_activity").WillReturnRows(loadRows())
	pool.ExpectQuery("pg_stat_user_tables").WillReturnRows(scanRows())
	pool.ExpectQuery("pg_database_size").WillReturnRows(storageRows())
	pool.ExpectQuery("pg_stat_checkpointer").WillReturnError(errors.New("relation does not exist"))
	pool.ExpectQuery("pg_stat_bgwriter").WillReturnRows(
		pgxmock.NewRows([]string{"timed", "requested"}).AddRow(int64(10), int64(6)))

	findings, err := conn.Health(context.Background())
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	if got := findingByCode(findings, "forced_checkpoints").Value; got != "38%" {
		t.Errorf("the older counters must still be read: %q", got)
	}
}

func memoryRows() *pgxmock.Rows {
	return pgxmock.NewRows([]string{"cache", "temp_files", "temp_bytes", "shared_buffers",
		"work_mem", "index_cache"}).
		AddRow(99.4, int64(0), int64(0), int64(134217728), int64(4194304), 99.8)
}

func loadRows() *pgxmock.Rows {
	return pgxmock.NewRows([]string{"connections", "max", "active", "idle_tx", "locks",
		"longest", "deadlocks", "rollbacks", "waiting_on", "read_only", "idle_for"}).
		AddRow(int64(42), int64(100), int64(2), int64(0), int64(0), 0.0, int64(0), 0.4, "", true, 0.0)
}

func scanRows() *pgxmock.Rows {
	return pgxmock.NewRows([]string{"seq", "idx", "dead", "live", "unused_size", "index_size",
		"unused", "vacuum_age", "never_vacuumed"}).
		AddRow(int64(40), int64(1000), int64(100), int64(9000), int64(0), int64(4096), int64(0),
			3600.0, int64(0))
}

func storageRows() *pgxmock.Rows {
	return pgxmock.NewRows([]string{"size", "age", "freeze", "slots", "server", "databases",
		"wal", "max_wal"}).
		AddRow(int64(104857600), int64(1000), int64(200000000), int64(0), int64(209715200),
			int64(3), int64(67108864), int64(1073741824))
}

func expectHealth(pool pgxmock.PgxPoolIface) {
	pool.ExpectQuery("pg_stat_database").WillReturnRows(memoryRows())
	pool.ExpectQuery("pg_stat_activity").WillReturnRows(loadRows())
	pool.ExpectQuery("pg_stat_user_tables").WillReturnRows(scanRows())
	pool.ExpectQuery("pg_database_size").WillReturnRows(storageRows())
	pool.ExpectQuery("pg_stat_checkpointer").WillReturnRows(
		pgxmock.NewRows([]string{"timed", "requested"}).AddRow(int64(40), int64(0)))
}

// A server that refuses everything is a failure. A server that refuses one
// thing is a report with a gap in it, which is tested above.
func TestHealthReportsFailures(t *testing.T) {
	conn, pool := mocked(t, readOnlyConfig())
	for range 6 {
		pool.ExpectQuery(".*").WillReturnError(errors.New("permission denied"))
	}
	if _, err := conn.Health(context.Background()); err == nil {
		t.Fatal("want an error")
	}
}

func TestHealthSurvivesOneRefusal(t *testing.T) {
	conn, pool := mocked(t, readOnlyConfig())
	pool.ExpectQuery("pg_stat_database").WillReturnRows(memoryRows())
	pool.ExpectQuery("pg_stat_activity").WillReturnRows(loadRows())
	pool.ExpectQuery("pg_stat_user_tables").WillReturnError(errors.New("permission denied"))
	pool.ExpectQuery("pg_database_size").WillReturnRows(storageRows())
	pool.ExpectQuery("pg_stat_checkpointer").WillReturnRows(
		pgxmock.NewRows([]string{"timed", "requested"}).AddRow(int64(40), int64(0)))

	findings, err := conn.Health(context.Background())
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	if findingByCode(findings, "unavailable").Group != driver.GroupScans {
		t.Errorf("the refusal must be reported: %+v", findings)
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

// The measured readings of a group sit together, and the number that is always
// fine sits last, because a group is read from the top.
func TestTheOrderOfTheReadings(t *testing.T) {
	findings := Findings(healthySnapshot())
	order := map[string][]string{}
	for _, finding := range findings {
		order[finding.Group] = append(order[finding.Group], finding.Code)
	}
	want := map[string][]string{
		driver.GroupMemory: {"cache_hit_ratio", "index_hit_ratio", "temp_files", "shared_buffers"},
		driver.GroupLoad: {"connections", "waiting_locks", "rollback_ratio", "long_running",
			"idle_in_transaction", "deadlocks", "transaction_read_only"},
		driver.GroupScans: {"sequential_scans", "dead_tuples", "unused_indexes", "vacuum_age"},
		driver.GroupStorage: {"transaction_age", "forced_checkpoints", "wal_size",
			"inactive_slots", "database_size", "server_size"},
	}
	for group, codes := range want {
		if !reflect.DeepEqual(order[group], codes) {
			t.Errorf("%s = %v, want %v", group, order[group], codes)
		}
	}
}

func TestTheNewReadings(t *testing.T) {
	cases := []struct {
		name     string
		mutate   func(*Snapshot)
		code     string
		severity driver.Severity
		value    string
		note     string
	}{
		{"index cache from disk", func(s *Snapshot) { s.IndexHitRatio = 80 },
			"index_hit_ratio", driver.SeverityCritical, "80.0%", "before a row is even found"},
		{"index cache cooling", func(s *Snapshot) { s.IndexHitRatio = 95 },
			"index_hit_ratio", driver.SeverityWarn, "95.0%", "costs what the index was for"},
		{"someone walked away", func(s *Snapshot) { s.IdleInTx, s.IdleSeconds = 1, 30 },
			"idle_in_transaction", driver.SeverityWarn, "1", "keeping vacuum from finishing"},
		{"someone walked away for good", func(s *Snapshot) { s.IdleInTx, s.IdleSeconds = 2, 3600 },
			"idle_in_transaction", driver.SeverityCritical, "2", "a client that crashed"},
		{"never cleaned", func(s *Snapshot) { s.NeverVacuumed = 4 },
			"vacuum_age", driver.SeverityWarn, "4 never", "never been vacuumed"},
		{"cleaned a while ago", func(s *Snapshot) { s.VacuumSeconds = 10 * 24 * 3600 },
			"vacuum_age", driver.SeverityWarn, "10 days ago", "last cleaned"},
		{"not cleaned in a month", func(s *Snapshot) { s.VacuumSeconds = 60 * 24 * 3600 },
			"vacuum_age", driver.SeverityCritical, "60 days ago", "autovacuum is off"},
		{"the log is piling up", func(s *Snapshot) { s.WalSize = 4 * 1024 * 1024 * 1024 },
			"wal_size", driver.SeverityWarn, "4.0 GiB", "checkpoints are behind"},
		{"the log cannot be read", func(s *Snapshot) { s.WalSize = -1 },
			"wal_size", driver.SeverityUnknown, "n/a", "pg_monitor"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			snapshot := healthySnapshot()
			c.mutate(&snapshot)
			finding := findingByCode(Findings(snapshot), c.code)
			if finding.Severity != c.severity {
				t.Errorf("severity = %s, want %s", finding.Severity, c.severity)
			}
			if finding.Value != c.value {
				t.Errorf("value = %q, want %q", finding.Value, c.value)
			}
			if !strings.Contains(finding.Note, c.note) {
				t.Errorf("note = %q, want it to mention %q", finding.Note, c.note)
			}
		})
	}
}

// Free disk space is not something a SQL connection can see, so the reading
// that comes closest says so rather than leaving a gap.
func TestTheServerReadingSaysWhatItCannotSee(t *testing.T) {
	finding := findingByCode(Findings(healthySnapshot()), "server_size")
	if finding.Value != "200.0 MiB" {
		t.Errorf("value = %q", finding.Value)
	}
	for _, want := range []string{"3 databases", "Free space", "not something a SQL connection"} {
		if !strings.Contains(finding.Note, want) {
			t.Errorf("note = %q, want it to mention %q", finding.Note, want)
		}
	}
}

func TestAge(t *testing.T) {
	cases := map[time.Duration]string{
		2 * time.Second:     "just now",
		time.Minute:         "1 minute ago",
		9 * time.Minute:     "9 minutes ago",
		time.Hour:           "1 hour ago",
		5 * time.Hour:       "5 hours ago",
		73 * time.Hour:      "3 days ago",
		30 * 24 * time.Hour: "30 days ago",
	}
	for since, want := range cases {
		if got := driver.Age(since); got != want {
			t.Errorf("Age(%v) = %q, want %q", since, got, want)
		}
	}
}

// The read only transaction is a layer of its own, not a repetition of the
// session setting: it holds whether or not that setting took.
func TestAReadOnlyProfileOpensAReadOnlyTransaction(t *testing.T) {
	conn, pool := mocked(t, readOnlyConfig())
	pool.ExpectBeginTx(pgx.TxOptions{AccessMode: pgx.ReadOnly})
	pool.ExpectQuery("SELECT 1").WillReturnRows(pgxmock.NewRows([]string{"one"}).AddRow(int64(1)))
	if _, err := conn.Query(context.Background(), "SELECT 1"); err != nil {
		t.Fatalf("Query: %v", err)
	}
}

func TestAWritableProfileOpensAPlainTransaction(t *testing.T) {
	config := readOnlyConfig()
	config.Mode = sqlguard.ModeReadWrite
	conn, pool := mocked(t, config)
	pool.ExpectBeginTx(pgx.TxOptions{})
	pool.ExpectQuery("SELECT 1").WillReturnRows(pgxmock.NewRows([]string{"one"}).AddRow(int64(1)))
	if _, err := conn.Query(context.Background(), "SELECT 1"); err != nil {
		t.Fatalf("Query: %v", err)
	}
}

// A stream reads past the cap a drawn result stops at, and lifts the statement
// timeout the session pinned, or an export of a large table dies partway
// through for a reason nobody watching would understand.
func TestStreamLiftsTheRowLimitAndTheStatementTimeout(t *testing.T) {
	config := readOnlyConfig()
	config.RowLimit = 1
	conn, pool := mocked(t, config)
	pool.ExpectBeginTx(pgx.TxOptions{AccessMode: pgx.ReadOnly})
	pool.ExpectExec(NoStatementTimeout).WillReturnResult(pgxmock.NewResult("SET", 0))
	pool.ExpectQuery("SELECT").WillReturnRows(
		pgxmock.NewRows([]string{"id"}).
			AddRow(int64(1)).AddRow(int64(2)).AddRow(int64(3)))
	pool.ExpectRollback()

	result, err := conn.Stream(context.Background(), "SELECT id FROM users")
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	rows := 0
	for result.Next() {
		rows++
	}
	if rows != 3 {
		t.Fatalf("rows = %d, a stream reads every one of them", rows)
	}
	if result.Truncated() {
		t.Error("and nothing was left behind to be truncated")
	}
	if err := result.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := pool.ExpectationsWereMet(); err != nil {
		t.Errorf("the timeout must actually be lifted: %v", err)
	}
}

// A server that refuses to lift the timeout is a server the export cannot run
// against, and the transaction is given back rather than left open.
func TestStreamGivesUpWhenTheTimeoutWillNotLift(t *testing.T) {
	conn, pool := mocked(t, readOnlyConfig())
	pool.ExpectBeginTx(pgx.TxOptions{AccessMode: pgx.ReadOnly})
	pool.ExpectExec(NoStatementTimeout).WillReturnError(errors.New("permission denied"))
	pool.ExpectRollback()

	if _, err := conn.Stream(context.Background(), "SELECT 1"); err == nil {
		t.Fatal("want an error")
	}
	if err := pool.ExpectationsWereMet(); err != nil {
		t.Errorf("the transaction must be given back: %v", err)
	}
}

// A number the server sends is a number by the time a screen or a file sees it.
// pgx hands back a type of its own for anything Go has no name for, and a row
// printed with %v would show its fields.
func TestTheDriversOwnTypesComeOffTheRow(t *testing.T) {
	for _, want := range []struct {
		name  string
		value any
		held  any
	}{
		{"a fraction", pgtype.Numeric{Int: big.NewInt(59800), Exp: -4, Valid: true}, "5.9800"},
		{"a whole number", pgtype.Numeric{Int: big.NewInt(42), Valid: true}, "42"},
		{"a negative", pgtype.Numeric{Int: big.NewInt(-7), Exp: -1, Valid: true}, "-0.7"},
		{"nothing", pgtype.Numeric{}, nil},
		{"a string", "plain", "plain"},
		{"a whole number Go has a name for", int64(3), int64(3)},
		{"nothing at all", nil, nil},
	} {
		t.Run(want.name, func(t *testing.T) {
			if got := native(want.value); got != want.held {
				t.Errorf("native = %#v, want %#v", got, want.held)
			}
		})
	}
}

func TestAColumnOfValuesComesOffTheRowToo(t *testing.T) {
	got, ok := native([]any{pgtype.Numeric{Int: big.NewInt(5), Valid: true}, "b"}).([]any)
	if !ok || len(got) != 2 {
		t.Fatalf("native = %#v", got)
	}
	if got[0] != "5" || got[1] != "b" {
		t.Errorf("native = %#v, want [5 b]", got)
	}
}

// A value that cannot say what it is stays as it was, rather than becoming an
// error message in the middle of a result.
func TestAValueThatWillNotSayIsLeftAlone(t *testing.T) {
	held := refusing{}
	if got := native(held); got != any(held) {
		t.Errorf("native = %#v, want the value back", got)
	}
}

type refusing struct{}

func (refusing) Value() (sqldriver.Value, error) { return nil, errors.New("no") }
