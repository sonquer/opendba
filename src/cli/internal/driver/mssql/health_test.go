package mssql

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/sonquer/opendba/src/cli/internal/driver"
)

// healthy is a server with nothing wrong with it, which every case below
// changes one thing about.
func healthy() Snapshot {
	return Snapshot{
		CacheHitRatio: 99.4, PageLife: 3600, MemoryGrants: 0,
		ServerMemory: 8 << 30, TargetMemory: 8 << 30,

		Connections: 12, MaxConnections: 32767, Active: 2, Blocked: 0,
		LongestSeconds: 3, Deadlocks: 0, OpenIdle: 0, WaitingOn: "CXPACKET",

		SeqScans: 100, IndexScans: 9900, UnusedIndexes: 0, UnusedIndexSize: 0,
		MissingIndexes: 0, MissingImpact: 0, StatisticsAge: 3600, NeverAnalysed: 0,

		DatabaseSize: 1 << 30, LogSize: 1 << 28, LogUsed: 12.5, LogReuseWait: "NOTHING",
		BoundedFiles: 0, DataFiles: 2, ServerSize: 4 << 30,
		RecoveryModel: "FULL", BackupAge: 3600,

		Refused: map[string]string{},
	}
}

func found(findings []driver.Finding, code string) (driver.Finding, bool) {
	for _, finding := range findings {
		if finding.Code == code {
			return finding, true
		}
	}
	return driver.Finding{}, false
}

func severityOf(t *testing.T, findings []driver.Finding, code string) driver.Severity {
	t.Helper()
	finding, ok := found(findings, code)
	if !ok {
		t.Fatalf("there is no finding called %q", code)
	}
	return finding.Severity
}

func TestAHealthyServerHasNothingWrongWithIt(t *testing.T) {
	findings := Findings(healthy(), true)
	if len(findings) < 20 {
		t.Fatalf("a full report is more than %d findings", len(findings))
	}
	for _, finding := range findings {
		if finding.Severity == driver.SeverityWarn || finding.Severity == driver.SeverityCritical {
			t.Errorf("%s is %s on a healthy server: %s", finding.Code, finding.Severity, finding.Note)
		}
		if finding.Group == "" || finding.Code == "" || finding.Value == "" {
			t.Errorf("every finding says what and where: %+v", finding)
		}
	}
}

func TestWhatEachMeasurementMeans(t *testing.T) {
	cases := []struct {
		name   string
		change func(*Snapshot)
		code   string
		want   driver.Severity
	}{
		{"a cache that misses sometimes", func(s *Snapshot) { s.CacheHitRatio = 85 }, "cache_hit_ratio", driver.SeverityWarn},
		{"a cache that misses often", func(s *Snapshot) { s.CacheHitRatio = 60 }, "cache_hit_ratio", driver.SeverityCritical},
		{"pages pushed out within the hour", func(s *Snapshot) { s.PageLife = 600 }, "page_life_expectancy", driver.SeverityWarn},
		{"pages pushed out at once", func(s *Snapshot) { s.PageLife = 30 }, "page_life_expectancy", driver.SeverityCritical},
		{"a counter the login may not read", func(s *Snapshot) { s.PageLife = -1 }, "page_life_expectancy", driver.SeverityUnknown},
		{"statements waiting for memory", func(s *Snapshot) { s.MemoryGrants = 3 }, "memory_grants_pending", driver.SeverityCritical},
		{"grants that were not counted", func(s *Snapshot) { s.MemoryGrants = -1 }, "memory_grants_pending", driver.SeverityUnknown},
		{"a server nearly out of connections", func(s *Snapshot) { s.Connections = 30000 }, "connections", driver.SeverityWarn},
		{"a session waiting on another", func(s *Snapshot) { s.Blocked = 1 }, "waiting_locks", driver.SeverityWarn},
		{"a statement that has run a while", func(s *Snapshot) { s.LongestSeconds = 120 }, "long_running", driver.SeverityWarn},
		{"a statement that has run far too long", func(s *Snapshot) { s.LongestSeconds = 600 }, "long_running", driver.SeverityCritical},
		{"deadlocks the server had to break", func(s *Snapshot) { s.Deadlocks = 4 }, "deadlocks", driver.SeverityWarn},
		{"deadlocks that were not counted", func(s *Snapshot) { s.Deadlocks = -1 }, "deadlocks", driver.SeverityUnknown},
		{"a transaction left open", func(s *Snapshot) { s.OpenIdle = 1 }, "idle_in_transaction", driver.SeverityWarn},
		{"a server that has waited on nothing", func(s *Snapshot) { s.WaitingOn = "" }, "top_wait", driver.SeverityUnknown},
		{"most reads walking whole tables", func(s *Snapshot) { s.SeqScans, s.IndexScans = 900, 100 }, "sequential_scans", driver.SeverityWarn},
		{"nothing read at all yet", func(s *Snapshot) { s.SeqScans, s.IndexScans = 0, 0 }, "sequential_scans", driver.SeverityUnknown},
		{"an index nothing reads", func(s *Snapshot) { s.UnusedIndexes, s.UnusedIndexSize = 2, 1<<20 }, "unused_indexes", driver.SeverityWarn},
		{"an index worth having", func(s *Snapshot) { s.MissingIndexes, s.MissingImpact = 3, 95 }, "missing_indexes", driver.SeverityWarn},
		{"an index barely worth having", func(s *Snapshot) { s.MissingIndexes, s.MissingImpact = 3, 10 }, "missing_indexes", driver.SeverityOK},
		{"statistics a week old", func(s *Snapshot) { s.StatisticsAge = 10 * 24 * 60 * 60 }, "statistics_age", driver.SeverityWarn},
		{"statistics a month old", func(s *Snapshot) { s.StatisticsAge = 60 * 24 * 60 * 60 }, "statistics_age", driver.SeverityCritical},
		{"statistics never built", func(s *Snapshot) { s.NeverAnalysed = 5 }, "statistics_age", driver.SeverityCritical},
		{"a log filling up", func(s *Snapshot) { s.LogUsed = 80 }, "log_space", driver.SeverityWarn},
		{"a log nearly full", func(s *Snapshot) { s.LogUsed = 95 }, "log_space", driver.SeverityCritical},
		{"a log that was not measured", func(s *Snapshot) { s.LogUsed = -1 }, "log_space", driver.SeverityUnknown},
		{"something holding the log", func(s *Snapshot) { s.LogReuseWait = "ACTIVE_TRANSACTION" }, "log_reuse_wait", driver.SeverityWarn},
		{"a log with nothing said about it", func(s *Snapshot) { s.LogReuseWait = "" }, "log_reuse_wait", driver.SeverityOK},
		{"a file that will stop growing", func(s *Snapshot) { s.BoundedFiles = 1 }, "file_space", driver.SeverityWarn},
		{"a backup taken yesterday", func(s *Snapshot) { s.BackupAge = 2 * 24 * 60 * 60 }, "last_backup", driver.SeverityWarn},
		{"a backup taken last month", func(s *Snapshot) { s.BackupAge = 40 * 24 * 60 * 60 }, "last_backup", driver.SeverityCritical},
		{"a backup nobody may ask about", func(s *Snapshot) { s.BackupAge = -1 }, "last_backup", driver.SeverityUnknown},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			snapshot := healthy()
			c.change(&snapshot)
			if got := severityOf(t, Findings(snapshot, true), c.code); got != c.want {
				t.Errorf("%s = %s, want %s", c.code, got, c.want)
			}
		})
	}
}

func TestARefusedPermissionCostsOnlyWhatItGuards(t *testing.T) {
	snapshot := healthy()
	snapshot.Refused[partScansDynamic] = "the login may not read the server state"
	findings := Findings(snapshot, true)

	unavailable, ok := found(findings, "unavailable")
	if !ok || unavailable.Group != driver.GroupScans || unavailable.Severity != driver.SeverityUnknown {
		t.Fatalf("a refused part must say so: %+v", unavailable)
	}
	if _, present := found(findings, "unused_indexes"); present {
		t.Error("a refused part reports nothing of its own")
	}
	if _, present := found(findings, "statistics_age"); !present {
		t.Error("what the catalog knows survives a refused dynamic view, in the same column")
	}
	if _, present := found(findings, "cache_hit_ratio"); !present {
		t.Error("the other groups are unaffected")
	}
}

func TestALoginThatMayReadNoServerStateStillGetsAReport(t *testing.T) {
	snapshot := healthy()
	for _, part := range []string{partMemory, partLoad, partScansDynamic, partStorageDynamic} {
		snapshot.Refused[part] = "VIEW SERVER STATE permission was denied"
	}
	findings := Findings(snapshot, true)
	for _, code := range []string{"database_size", "statistics_age", "file_space", "access_mode"} {
		if _, present := found(findings, code); !present {
			t.Errorf("%s needs no permission and must survive: %+v", code, findings)
		}
	}
	for _, code := range []string{"cache_hit_ratio", "connections", "unused_indexes", "log_space"} {
		if _, present := found(findings, code); present {
			t.Errorf("%s is behind the refused permission and must not be guessed at", code)
		}
	}
}

func TestTheAccessModeIsWhatOpendbaDecidedRatherThanWhatTheServerWasTold(t *testing.T) {
	readOnly := severityOf(t, Findings(healthy(), true), "access_mode")
	if readOnly != driver.SeverityOK {
		t.Errorf("a read only profile is the safe one: %s", readOnly)
	}
	writable, ok := found(Findings(healthy(), false), "access_mode")
	if !ok || writable.Severity != driver.SeverityWarn || writable.Value != "read / write" {
		t.Errorf("a writable profile has no server side net: %+v", writable)
	}
}

func TestServerMemoryReportsWhatItWasNotToldToWant(t *testing.T) {
	snapshot := healthy()
	snapshot.TargetMemory = -1
	finding, ok := found(Findings(snapshot, true), "server_memory")
	if !ok || finding.Measured {
		t.Errorf("a target nobody reported is not a proportion of anything: %+v", finding)
	}
}

func memoryRow() *sqlmock.Rows {
	s := healthy()
	return sqlmock.NewRows([]string{"cache", "life", "grants", "memory", "target"}).
		AddRow(s.CacheHitRatio, s.PageLife, s.MemoryGrants, s.ServerMemory, s.TargetMemory)
}

func loadRow() *sqlmock.Rows {
	s := healthy()
	return sqlmock.NewRows([]string{"conns", "max", "active", "blocked", "longest", "deadlocks", "idle", "wait"}).
		AddRow(s.Connections, s.MaxConnections, s.Active, s.Blocked, s.LongestSeconds, s.Deadlocks, s.OpenIdle, s.WaitingOn)
}

func scanRow() *sqlmock.Rows {
	s := healthy()
	return sqlmock.NewRows([]string{"seq", "idx", "unused", "unusedsize", "missing", "impact"}).
		AddRow(s.SeqScans, s.IndexScans, s.UnusedIndexes, s.UnusedIndexSize, s.MissingIndexes, s.MissingImpact)
}

func scanCatalogRow() *sqlmock.Rows {
	s := healthy()
	return sqlmock.NewRows([]string{"age", "never"}).AddRow(s.StatisticsAge/60, s.NeverAnalysed)
}

func logRow() *sqlmock.Rows {
	return sqlmock.NewRows([]string{"used"}).AddRow(healthy().LogUsed)
}

func storageRow() *sqlmock.Rows {
	s := healthy()
	return sqlmock.NewRows([]string{"size", "log", "reuse", "bounded", "files", "server", "recovery"}).
		AddRow(s.DatabaseSize, s.LogSize, s.LogReuseWait, s.BoundedFiles, s.DataFiles, s.ServerSize, s.RecoveryModel)
}

func expectHealth(mock sqlmock.Sqlmock) {
	mock.ExpectQuery(memoryQuery).WillReturnRows(memoryRow())
	mock.ExpectQuery(loadQuery).WillReturnRows(loadRow())
	mock.ExpectQuery(scansDynamicQuery).WillReturnRows(scanRow())
	mock.ExpectQuery(scansCatalogQuery).WillReturnRows(scanCatalogRow())
	mock.ExpectQuery(storageDynamicQuery).WillReturnRows(logRow())
	mock.ExpectQuery(storageCatalogQuery).WillReturnRows(storageRow())
}

func TestHealthReadsEachPartOnItsOwn(t *testing.T) {
	conn, mock := mocked(t, readOnlyConfig())
	expectHealth(mock)
	mock.ExpectQuery(backupQuery).WillReturnRows(sqlmock.NewRows([]string{"age"}).AddRow(int64(60)))

	findings, err := conn.Health(context.Background())
	if err != nil {
		t.Fatalf("Health() = %v", err)
	}
	if severityOf(t, findings, "last_backup") != driver.SeverityOK {
		t.Error("a backup taken an hour ago is a backup")
	}
	if severityOf(t, findings, "log_space") != driver.SeverityOK {
		t.Error("a log an eighth full is a healthy log")
	}
}

func TestHealthKeepsGoingWhenOnePartIsRefused(t *testing.T) {
	conn, mock := mocked(t, readOnlyConfig())
	mock.ExpectQuery(memoryQuery).WillReturnError(errRefused)
	mock.ExpectQuery(loadQuery).WillReturnRows(loadRow())
	mock.ExpectQuery(scansDynamicQuery).WillReturnRows(scanRow())
	mock.ExpectQuery(scansCatalogQuery).WillReturnRows(scanCatalogRow())
	mock.ExpectQuery(storageDynamicQuery).WillReturnRows(logRow())
	mock.ExpectQuery(storageCatalogQuery).WillReturnRows(storageRow())
	mock.ExpectQuery(backupQuery).WillReturnError(errRefused)

	findings, err := conn.Health(context.Background())
	if err != nil {
		t.Fatalf("Health() = %v", err)
	}
	if _, ok := found(findings, "unavailable"); !ok {
		t.Error("the refused part must say so")
	}
	if severityOf(t, findings, "last_backup") != driver.SeverityUnknown {
		t.Error("a backup nobody may ask about is not a missing backup")
	}
}

// TestHealthOfAServerThatSaysNothingIsAReportAboutThat is what a login made for
// reading tables gets, and it must not be an empty screen: every reading on
// this server is behind a permission such a login is rarely given.
func TestHealthOfAServerThatSaysNothingIsAReportAboutThat(t *testing.T) {
	conn, mock := mocked(t, readOnlyConfig())
	for _, query := range []string{memoryQuery, loadQuery, scansDynamicQuery,
		scansCatalogQuery, storageDynamicQuery, storageCatalogQuery, backupQuery} {
		mock.ExpectQuery(query).WillReturnError(errRefused)
	}
	findings, err := conn.Health(context.Background())
	if err != nil {
		t.Fatalf("a refused permission is not a failure: %v", err)
	}
	refused := 0
	for _, finding := range findings {
		if finding.Code == "unavailable" {
			refused++
		}
	}
	if refused != 6 {
		t.Errorf("every refused part says so, got %d of 6: %+v", refused, findings)
	}
	if _, ok := found(findings, "access_mode"); !ok {
		t.Error("what opendba decided for itself needs no permission at all")
	}
}

// TestSpansArriveInMinutesAndAreHeldInSeconds pins the unit the server is asked
// for. Asking for a span of decades in seconds overflows the integer it counts
// in, and the server answers with an error rather than a number.
func TestSpansArriveInMinutesAndAreHeldInSeconds(t *testing.T) {
	conn, mock := mocked(t, readOnlyConfig())
	mock.ExpectQuery(memoryQuery).WillReturnRows(memoryRow())
	mock.ExpectQuery(loadQuery).WillReturnRows(loadRow())
	mock.ExpectQuery(scansDynamicQuery).WillReturnRows(scanRow())
	mock.ExpectQuery(scansCatalogQuery).WillReturnRows(
		sqlmock.NewRows([]string{"age", "never"}).AddRow(int64(60*24*40), int64(0)))
	mock.ExpectQuery(storageDynamicQuery).WillReturnRows(logRow())
	mock.ExpectQuery(storageCatalogQuery).WillReturnRows(storageRow())
	mock.ExpectQuery(backupQuery).WillReturnRows(
		sqlmock.NewRows([]string{"age"}).AddRow(int64(60 * 24 * 40)))

	findings, err := conn.Health(context.Background())
	if err != nil {
		t.Fatalf("Health() = %v", err)
	}
	if severityOf(t, findings, "statistics_age") != driver.SeverityCritical {
		t.Error("statistics forty days old are forty days old, not forty days of minutes")
	}
	age, _ := found(findings, "last_backup")
	if age.Value != "40 days ago" {
		t.Errorf("last_backup = %q, want 40 days ago", age.Value)
	}
}

func TestASpanTheServerWouldNotCountStaysUnmeasured(t *testing.T) {
	conn, mock := mocked(t, readOnlyConfig())
	expectHealth(mock)
	mock.ExpectQuery(backupQuery).WillReturnRows(sqlmock.NewRows([]string{"age"}).AddRow(int64(-1)))
	findings, err := conn.Health(context.Background())
	if err != nil {
		t.Fatalf("Health() = %v", err)
	}
	if severityOf(t, findings, "last_backup") != driver.SeverityUnknown {
		t.Error("a backup this login may not ask about is unknown, not ancient")
	}
}
