package mssql

import (
	"context"
	"fmt"
	"time"

	"github.com/sonquer/opendba/src/cli/internal/driver"
)

const memoryQuery = `SELECT
	ISNULL((SELECT TOP (1) CAST(a.cntr_value AS float) * 100.0 / NULLIF(b.cntr_value, 0)
		FROM sys.dm_os_performance_counters a
		JOIN sys.dm_os_performance_counters b
		  ON b.counter_name LIKE N'Buffer cache hit ratio base%'
		WHERE a.counter_name LIKE N'Buffer cache hit ratio%'
		  AND a.counter_name NOT LIKE N'%base%'), 100.0),
	ISNULL((SELECT TOP (1) cntr_value FROM sys.dm_os_performance_counters
		WHERE counter_name LIKE N'Page life expectancy%'), -1),
	ISNULL((SELECT TOP (1) cntr_value FROM sys.dm_os_performance_counters
		WHERE counter_name LIKE N'Memory Grants Pending%'), -1),
	ISNULL((SELECT TOP (1) cntr_value * 1024 FROM sys.dm_os_performance_counters
		WHERE counter_name LIKE N'Total Server Memory (KB)%'), -1),
	ISNULL((SELECT TOP (1) cntr_value * 1024 FROM sys.dm_os_performance_counters
		WHERE counter_name LIKE N'Target Server Memory (KB)%'), -1)`

const loadQuery = `SELECT
	(SELECT COUNT(*) FROM sys.dm_exec_sessions WHERE is_user_process = 1),
	CAST(@@MAX_CONNECTIONS AS bigint),
	(SELECT COUNT(*) FROM sys.dm_exec_requests r
		JOIN sys.dm_exec_sessions s ON s.session_id = r.session_id WHERE s.is_user_process = 1),
	(SELECT COUNT(*) FROM sys.dm_exec_requests WHERE blocking_session_id <> 0),
	ISNULL((SELECT MAX(DATEDIFF(second, r.start_time, SYSDATETIME())) FROM sys.dm_exec_requests r
		JOIN sys.dm_exec_sessions s ON s.session_id = r.session_id WHERE s.is_user_process = 1), 0),
	ISNULL((SELECT TOP (1) cntr_value FROM sys.dm_os_performance_counters
		WHERE counter_name LIKE N'Number of Deadlocks/sec%' AND instance_name = N'_Total'), -1),
	(SELECT COUNT(*) FROM sys.dm_exec_sessions
		WHERE is_user_process = 1 AND open_transaction_count > 0 AND status = 'sleeping'),
	ISNULL((SELECT TOP (1) wait_type FROM sys.dm_os_wait_stats
		WHERE wait_type NOT LIKE N'SLEEP%' AND wait_type NOT LIKE N'%QUEUE%'
		  AND wait_type NOT IN (N'CLR_AUTO_EVENT', N'XE_TIMER_EVENT', N'BROKER_TASK_STOP',
		                        N'WAITFOR', N'DIRTY_PAGE_POLL', N'HADR_FILESTREAM_IOMGR_IOCOMPLETION')
		ORDER BY wait_time_ms DESC), N'')`

// scansDynamicQuery reads how the indexes have been used, which lives in the
// dynamic management views and needs a permission a reporting login is often
// not given.
const scansDynamicQuery = `SELECT
	ISNULL((SELECT SUM(user_scans) FROM sys.dm_db_index_usage_stats u
		JOIN sys.indexes i ON i.object_id = u.object_id AND i.index_id = u.index_id
		WHERE u.database_id = DB_ID() AND i.index_id IN (0,1)), 0),
	ISNULL((SELECT SUM(user_seeks + user_lookups) FROM sys.dm_db_index_usage_stats
		WHERE database_id = DB_ID()), 0),
	(SELECT COUNT(*) FROM sys.indexes i
		JOIN sys.objects t ON t.object_id = i.object_id
		LEFT JOIN sys.dm_db_index_usage_stats u
			ON u.object_id = i.object_id AND u.index_id = i.index_id AND u.database_id = DB_ID()
		WHERE t.type = 'U' AND i.index_id > 1 AND i.is_unique = 0
		  AND ISNULL(u.user_seeks + u.user_scans + u.user_lookups, 0) = 0),
	ISNULL((SELECT SUM(a.used_pages) * 8192 FROM sys.indexes i
		JOIN sys.objects t ON t.object_id = i.object_id
		JOIN sys.partitions p ON p.object_id = i.object_id AND p.index_id = i.index_id
		JOIN sys.allocation_units a ON a.container_id = p.partition_id
		LEFT JOIN sys.dm_db_index_usage_stats u
			ON u.object_id = i.object_id AND u.index_id = i.index_id AND u.database_id = DB_ID()
		WHERE t.type = 'U' AND i.index_id > 1 AND i.is_unique = 0
		  AND ISNULL(u.user_seeks + u.user_scans + u.user_lookups, 0) = 0), 0),
	(SELECT COUNT(*) FROM sys.dm_db_missing_index_details WHERE database_id = DB_ID()),
	ISNULL((SELECT MAX(g.avg_user_impact) FROM sys.dm_db_missing_index_groups mg
		JOIN sys.dm_db_missing_index_group_stats g ON g.group_handle = mg.index_group_handle
		JOIN sys.dm_db_missing_index_details d ON d.index_handle = mg.index_handle
		WHERE d.database_id = DB_ID()), 0)`

// scansCatalogQuery reads when the optimiser last learned what is in the
// tables, which is in the catalog and needs no permission beyond seeing them.
const scansCatalogQuery = `SELECT
	ISNULL((SELECT DATEDIFF(minute, MIN(STATS_DATE(st.object_id, st.stats_id)), SYSDATETIME())
		FROM sys.stats st JOIN sys.objects t ON t.object_id = st.object_id
		WHERE t.type = 'U' AND STATS_DATE(st.object_id, st.stats_id) IS NOT NULL), 0),
	(SELECT COUNT(*) FROM sys.stats st JOIN sys.objects t ON t.object_id = st.object_id
		WHERE t.type = 'U' AND STATS_DATE(st.object_id, st.stats_id) IS NULL)`

// storageDynamicQuery reads how full the transaction log is, which is the one
// storage reading that lives in a dynamic management view.
const storageDynamicQuery = `SELECT
	ISNULL((SELECT TOP (1) used_log_space_in_percent FROM sys.dm_db_log_space_usage), -1.0)`

// storageCatalogQuery reads what is on disk, which is in the catalog.
const storageCatalogQuery = `SELECT
	ISNULL((SELECT SUM(CAST(size AS bigint)) * 8192 FROM sys.database_files), 0),
	ISNULL((SELECT SUM(CAST(size AS bigint)) * 8192 FROM sys.database_files WHERE type_desc = N'LOG'), 0),
	ISNULL((SELECT TOP (1) log_reuse_wait_desc FROM sys.databases WHERE database_id = DB_ID()), N''),
	(SELECT COUNT(*) FROM sys.database_files WHERE max_size <> -1 AND type_desc = N'ROWS'),
	(SELECT COUNT(*) FROM sys.database_files WHERE type_desc = N'ROWS'),
	ISNULL((SELECT SUM(CAST(size AS bigint)) * 8192 FROM sys.master_files), 0),
	ISNULL((SELECT TOP (1) recovery_model_desc FROM sys.databases WHERE database_id = DB_ID()), N'')`

// backupQuery reads when this database was last written somewhere else. It
// lives in another database that a login is often not given, so it is asked for
// on its own and its refusal costs nothing else.
const backupQuery = `SELECT ISNULL(DATEDIFF(minute,
	(SELECT MAX(backup_finish_date) FROM msdb.dbo.backupset
		WHERE database_name = DB_NAME() AND type = 'D'), SYSDATETIME()), -1)`

// The parts a report is gathered in. Two of the four groups are split, because
// half of what they say is in the catalog and half is in the dynamic management
// views, and only the second half is behind a permission.
const (
	partMemory         = "memory"
	partLoad           = "load"
	partScansDynamic   = "scans.dynamic"
	partScansCatalog   = "scans.catalog"
	partStorageDynamic = "storage.dynamic"
	partStorageCatalog = "storage.catalog"
)

// Snapshot is everything the server was willing to say about itself.
type Snapshot struct {
	CacheHitRatio float64
	PageLife      int64
	MemoryGrants  int64
	ServerMemory  int64
	TargetMemory  int64

	Connections    int64
	MaxConnections int64
	Active         int64
	Blocked        int64
	LongestSeconds int64
	Deadlocks      int64
	OpenIdle       int64
	WaitingOn      string

	SeqScans        int64
	IndexScans      int64
	UnusedIndexes   int64
	UnusedIndexSize int64
	MissingIndexes  int64
	MissingImpact   float64
	StatisticsAge   int64
	NeverAnalysed   int64

	DatabaseSize  int64
	LogSize       int64
	LogUsed       float64
	LogReuseWait  string
	BoundedFiles  int64
	DataFiles     int64
	ServerSize    int64
	RecoveryModel string
	BackupAge     int64

	Refused map[string]string
}

func (s Snapshot) refused(part string) (string, bool) {
	reason, ok := s.Refused[part]
	return reason, ok
}

// Health reads the server part by part, and never fails as a whole. On this
// server almost every measurement is behind VIEW SERVER STATE, which a login
// made for reading tables is rarely given, so a report saying which readings
// were refused is the normal answer rather than an error.
func (c *connection) Health(ctx context.Context) ([]driver.Finding, error) {
	snapshot := Snapshot{Refused: map[string]string{}, BackupAge: unmeasured}
	c.read(ctx, &snapshot, partMemory, memoryQuery, &snapshot.CacheHitRatio, &snapshot.PageLife,
		&snapshot.MemoryGrants, &snapshot.ServerMemory, &snapshot.TargetMemory)
	c.read(ctx, &snapshot, partLoad, loadQuery, &snapshot.Connections, &snapshot.MaxConnections,
		&snapshot.Active, &snapshot.Blocked, &snapshot.LongestSeconds, &snapshot.Deadlocks,
		&snapshot.OpenIdle, &snapshot.WaitingOn)
	c.read(ctx, &snapshot, partScansDynamic, scansDynamicQuery, &snapshot.SeqScans, &snapshot.IndexScans,
		&snapshot.UnusedIndexes, &snapshot.UnusedIndexSize, &snapshot.MissingIndexes, &snapshot.MissingImpact)
	c.read(ctx, &snapshot, partScansCatalog, scansCatalogQuery, &snapshot.StatisticsAge, &snapshot.NeverAnalysed)
	snapshot.StatisticsAge = seconds(snapshot.StatisticsAge)
	c.read(ctx, &snapshot, partStorageDynamic, storageDynamicQuery, &snapshot.LogUsed)
	c.read(ctx, &snapshot, partStorageCatalog, storageCatalogQuery, &snapshot.DatabaseSize, &snapshot.LogSize,
		&snapshot.LogReuseWait, &snapshot.BoundedFiles, &snapshot.DataFiles, &snapshot.ServerSize,
		&snapshot.RecoveryModel)
	if err := c.db.QueryRowContext(ctx, backupQuery).Scan(&snapshot.BackupAge); err != nil {
		snapshot.BackupAge = unmeasured
	}
	snapshot.BackupAge = seconds(snapshot.BackupAge)
	return Findings(snapshot, c.config.ReadOnly()), nil
}

// seconds turns a span the server counted in minutes into the seconds every
// span in a snapshot is held in. Minutes are what the server is asked for,
// because counting a span of decades in seconds overflows what it counts in.
func seconds(minutes int64) int64 {
	if minutes < 0 {
		return unmeasured
	}
	return minutes * 60
}

// read takes one part of the report, and records why it is missing when the
// server will not answer for it.
func (c *connection) read(ctx context.Context, snapshot *Snapshot, part, query string, into ...any) {
	if err := c.db.QueryRowContext(ctx, query).Scan(into...); err != nil {
		snapshot.Refused[part] = err.Error()
	}
}

// Findings is what the numbers mean. It is a function of a snapshot alone, so
// what the server said and what opendba concluded from it are separable.
func Findings(snapshot Snapshot, readOnly bool) []driver.Finding {
	var findings []driver.Finding
	for _, part := range parts {
		findings = append(findings, part.build(snapshot)...)
	}
	findings = append(findings, accessFinding(readOnly))
	return findings
}

// reading is one part of the report: which column it belongs to, which query
// fills it, and what it concludes.
type reading struct {
	group string
	part  string
	from  func(Snapshot) []driver.Finding
}

// build is the part's findings, or the one row that says why they are missing.
func (r reading) build(snapshot Snapshot) []driver.Finding {
	if reason, refused := snapshot.refused(r.part); refused {
		return []driver.Finding{{
			Group: r.group, Subsystem: "permissions", Code: "unavailable",
			Severity: driver.SeverityUnknown, Value: "n/a", Note: reason,
		}}
	}
	return r.from(snapshot)
}

var parts = []reading{
	{driver.GroupMemory, partMemory, memoryFindings},
	{driver.GroupLoad, partLoad, loadFindings},
	{driver.GroupScans, partScansDynamic, dynamicScanFindings},
	{driver.GroupScans, partScansCatalog, catalogScanFindings},
	{driver.GroupStorage, partStorageDynamic, dynamicStorageFindings},
	{driver.GroupStorage, partStorageCatalog, catalogStorageFindings},
}

func memoryFindings(s Snapshot) []driver.Finding {
	return []driver.Finding{
		cacheFinding(s), pageLifeFinding(s), grantsFinding(s), memoryFinding(s),
	}
}

func loadFindings(s Snapshot) []driver.Finding {
	return []driver.Finding{
		connectionsFinding(s), blockedFinding(s), longestFinding(s),
		deadlocksFinding(s), openIdleFinding(s), waitFinding(s),
	}
}

func dynamicScanFindings(s Snapshot) []driver.Finding {
	return []driver.Finding{
		accessPathFinding(s), unusedIndexFinding(s), missingIndexFinding(s),
	}
}

func catalogScanFindings(s Snapshot) []driver.Finding {
	return []driver.Finding{statisticsFinding(s)}
}

func dynamicStorageFindings(s Snapshot) []driver.Finding {
	return []driver.Finding{logFinding(s)}
}

func catalogStorageFindings(s Snapshot) []driver.Finding {
	return []driver.Finding{
		sizeFinding(s), logReuseFinding(s), fileSpaceFinding(s), backupFinding(s), serverFinding(s),
	}
}

// The lines the server is judged against. They are the points where the advice
// changes rather than thresholds the server enforces.
const (
	warmEnough      = 90.0
	coldCache       = 80.0
	shortLife       = 300
	briefLife       = 900
	busyConnections = 0.8
	tiredStatistics = 7 * 24 * 60 * 60
	staleStatistics = 30 * 24 * 60 * 60
	longStatement   = 60
	slowStatement   = 300
	fullLog         = 75.0
	brimmingLog     = 90.0
	mostlyIndexed   = 0.5
	oldBackup       = 24 * 60 * 60
	staleBackup     = 7 * 24 * 60 * 60
	worthAnIndex    = 70.0
)

func cacheFinding(s Snapshot) driver.Finding {
	finding := driver.Finding{
		Group: driver.GroupMemory, Subsystem: "buffer pool", Code: "cache_hit_ratio",
		Value: fmt.Sprintf("%.1f%%", s.CacheHitRatio),
		Note:  "share of pages read that were already in memory",
	}
	finding = finding.Measure(s.CacheHitRatio, 100)
	switch {
	case s.CacheHitRatio < coldCache:
		finding.Severity = driver.SeverityCritical
	case s.CacheHitRatio < warmEnough:
		finding.Severity = driver.SeverityWarn
	default:
		finding.Severity = driver.SeverityOK
	}
	return finding
}

func pageLifeFinding(s Snapshot) driver.Finding {
	finding := driver.Finding{
		Group: driver.GroupMemory, Subsystem: "buffer pool", Code: "page_life_expectancy",
		Value: driver.Duration(time.Duration(s.PageLife) * time.Second),
		Note:  "how long a page stays in memory before it is pushed out",
	}
	switch {
	case s.PageLife < 0:
		finding.Severity, finding.Value = driver.SeverityUnknown, "n/a"
	case s.PageLife < shortLife:
		finding.Severity = driver.SeverityCritical
	case s.PageLife < briefLife:
		finding.Severity = driver.SeverityWarn
	default:
		finding.Severity = driver.SeverityOK
	}
	return finding
}

func grantsFinding(s Snapshot) driver.Finding {
	finding := driver.Finding{
		Group: driver.GroupMemory, Subsystem: "memory grants", Code: "memory_grants_pending",
		Value: fmt.Sprintf("%d", s.MemoryGrants),
		Note:  "statements waiting for the memory they were promised",
	}
	switch {
	case s.MemoryGrants < 0:
		finding.Severity, finding.Value = driver.SeverityUnknown, "n/a"
	case s.MemoryGrants > 0:
		finding.Severity = driver.SeverityCritical
	default:
		finding.Severity = driver.SeverityOK
	}
	return finding
}

func memoryFinding(s Snapshot) driver.Finding {
	finding := driver.Finding{
		Group: driver.GroupMemory, Subsystem: "memory", Code: "server_memory",
		Value:    driver.ByteSize(s.ServerMemory),
		Note:     "memory the server holds, against what it wants",
		Severity: driver.SeverityInfo,
	}
	if s.TargetMemory > 0 {
		finding = finding.Measure(float64(s.ServerMemory), float64(s.TargetMemory))
		finding.Note = "memory the server holds, of " + driver.ByteSize(s.TargetMemory) + " it wants"
	}
	return finding
}

func connectionsFinding(s Snapshot) driver.Finding {
	finding := driver.Finding{
		Group: driver.GroupLoad, Subsystem: "sessions", Code: "connections",
		Value: fmt.Sprintf("%d of %d", s.Connections, s.MaxConnections),
		Note:  "sessions open, against what the server allows",
	}
	finding = finding.Measure(float64(s.Connections), float64(s.MaxConnections))
	if s.MaxConnections > 0 && float64(s.Connections) > float64(s.MaxConnections)*busyConnections {
		finding.Severity = driver.SeverityWarn
	} else {
		finding.Severity = driver.SeverityOK
	}
	return finding
}

func blockedFinding(s Snapshot) driver.Finding {
	finding := driver.Finding{
		Group: driver.GroupLoad, Subsystem: "locks", Code: "waiting_locks",
		Value: fmt.Sprintf("%d", s.Blocked),
		Note:  "statements waiting on a lock another session holds",
	}
	switch {
	case s.Blocked > 0:
		finding.Severity = driver.SeverityWarn
	default:
		finding.Severity = driver.SeverityOK
	}
	return finding
}

func longestFinding(s Snapshot) driver.Finding {
	finding := driver.Finding{
		Group: driver.GroupLoad, Subsystem: "sessions", Code: "long_running",
		Value: driver.Duration(time.Duration(s.LongestSeconds) * time.Second),
		Note:  "the statement that has been running longest",
	}
	switch {
	case s.LongestSeconds >= slowStatement:
		finding.Severity = driver.SeverityCritical
	case s.LongestSeconds >= longStatement:
		finding.Severity = driver.SeverityWarn
	default:
		finding.Severity = driver.SeverityOK
	}
	return finding
}

func deadlocksFinding(s Snapshot) driver.Finding {
	finding := driver.Finding{
		Group: driver.GroupLoad, Subsystem: "locks", Code: "deadlocks",
		Value: fmt.Sprintf("%d", s.Deadlocks),
		Note:  "deadlocks the server has broken since it started",
	}
	switch {
	case s.Deadlocks < 0:
		finding.Severity, finding.Value = driver.SeverityUnknown, "n/a"
	case s.Deadlocks > 0:
		finding.Severity = driver.SeverityWarn
	default:
		finding.Severity = driver.SeverityOK
	}
	return finding
}

func openIdleFinding(s Snapshot) driver.Finding {
	finding := driver.Finding{
		Group: driver.GroupLoad, Subsystem: "transactions", Code: "idle_in_transaction",
		Value: fmt.Sprintf("%d", s.OpenIdle),
		Note:  "sessions holding a transaction open with nothing to run in it",
	}
	switch {
	case s.OpenIdle > 0:
		finding.Severity = driver.SeverityWarn
	default:
		finding.Severity = driver.SeverityOK
	}
	return finding
}

func waitFinding(s Snapshot) driver.Finding {
	finding := driver.Finding{
		Group: driver.GroupLoad, Subsystem: "waits", Code: "top_wait",
		Value:    s.WaitingOn,
		Note:     "what the server has spent the most time waiting for",
		Severity: driver.SeverityInfo,
	}
	if s.WaitingOn == "" {
		finding.Severity, finding.Value = driver.SeverityUnknown, "n/a"
	}
	return finding
}

func accessPathFinding(s Snapshot) driver.Finding {
	total := s.SeqScans + s.IndexScans
	finding := driver.Finding{
		Group: driver.GroupScans, Subsystem: "access", Code: "sequential_scans",
		Note: "reads that walked a whole table rather than looking one row up",
	}
	if total == 0 {
		finding.Severity, finding.Value = driver.SeverityUnknown, "n/a"
		return finding
	}
	share := float64(s.SeqScans) / float64(total)
	finding.Value = fmt.Sprintf("%.1f%%", share*100)
	finding = finding.Measure(float64(s.SeqScans), float64(total))
	if share > mostlyIndexed {
		finding.Severity = driver.SeverityWarn
	} else {
		finding.Severity = driver.SeverityOK
	}
	return finding
}

func unusedIndexFinding(s Snapshot) driver.Finding {
	finding := driver.Finding{
		Group: driver.GroupScans, Subsystem: "indexes", Code: "unused_indexes",
		Value: fmt.Sprintf("%d, %s", s.UnusedIndexes, driver.ByteSize(s.UnusedIndexSize)),
		Note:  "indexes nothing has read, which cost disk and slow every write",
	}
	if s.UnusedIndexes > 0 {
		finding.Severity = driver.SeverityWarn
	} else {
		finding.Severity = driver.SeverityOK
	}
	return finding
}

func missingIndexFinding(s Snapshot) driver.Finding {
	finding := driver.Finding{
		Group: driver.GroupScans, Subsystem: "indexes", Code: "missing_indexes",
		Value: fmt.Sprintf("%d", s.MissingIndexes),
		Note:  "indexes the optimiser wished for while it was planning",
	}
	if s.MissingIndexes > 0 {
		finding.Note = fmt.Sprintf("indexes the optimiser wished for, the best worth %.0f%%", s.MissingImpact)
	}
	switch {
	case s.MissingIndexes > 0 && s.MissingImpact >= worthAnIndex:
		finding.Severity = driver.SeverityWarn
	default:
		finding.Severity = driver.SeverityOK
	}
	return finding
}

func statisticsFinding(s Snapshot) driver.Finding {
	finding := driver.Finding{
		Group: driver.GroupScans, Subsystem: "statistics", Code: "statistics_age",
		Value: driver.Age(time.Duration(s.StatisticsAge) * time.Second),
		Note:  "when the optimiser last learned what is in these tables",
	}
	if s.NeverAnalysed > 0 {
		finding.Note = fmt.Sprintf("%d sets of statistics have never been built", s.NeverAnalysed)
	}
	switch {
	case s.StatisticsAge >= staleStatistics || s.NeverAnalysed > 0:
		finding.Severity = driver.SeverityCritical
	case s.StatisticsAge >= tiredStatistics:
		finding.Severity = driver.SeverityWarn
	default:
		finding.Severity = driver.SeverityOK
	}
	return finding
}

func sizeFinding(s Snapshot) driver.Finding {
	return driver.Finding{
		Group: driver.GroupStorage, Subsystem: "database", Code: "database_size",
		Value:    driver.ByteSize(s.DatabaseSize),
		Note:     "what this database takes on disk, log included",
		Severity: driver.SeverityInfo,
	}
}

func logFinding(s Snapshot) driver.Finding {
	finding := driver.Finding{
		Group: driver.GroupStorage, Subsystem: "transaction log", Code: "log_space",
		Value: fmt.Sprintf("%.1f%% of %s", s.LogUsed, driver.ByteSize(s.LogSize)),
		Note:  "how full the transaction log is",
	}
	finding = finding.Measure(s.LogUsed, 100)
	switch {
	case s.LogUsed < 0:
		finding.Severity, finding.Value, finding.Measured = driver.SeverityUnknown, "n/a", false
	case s.LogUsed >= brimmingLog:
		finding.Severity = driver.SeverityCritical
	case s.LogUsed >= fullLog:
		finding.Severity = driver.SeverityWarn
	default:
		finding.Severity = driver.SeverityOK
	}
	return finding
}

func logReuseFinding(s Snapshot) driver.Finding {
	finding := driver.Finding{
		Group: driver.GroupStorage, Subsystem: "transaction log", Code: "log_reuse_wait",
		Value: s.LogReuseWait,
		Note:  "what is stopping the log from being reused",
	}
	switch s.LogReuseWait {
	case "", "NOTHING":
		finding.Severity, finding.Value = driver.SeverityOK, "nothing"
	default:
		finding.Severity = driver.SeverityWarn
	}
	return finding
}

func fileSpaceFinding(s Snapshot) driver.Finding {
	finding := driver.Finding{
		Group: driver.GroupStorage, Subsystem: "files", Code: "file_space",
		Value: fmt.Sprintf("%d of %d", s.BoundedFiles, s.DataFiles),
		Note:  "data files that will stop growing before the disk does",
	}
	finding = finding.Measure(float64(s.BoundedFiles), float64(s.DataFiles))
	if s.BoundedFiles > 0 {
		finding.Severity = driver.SeverityWarn
	} else {
		finding.Severity = driver.SeverityOK
	}
	return finding
}

func backupFinding(s Snapshot) driver.Finding {
	finding := driver.Finding{
		Group: driver.GroupStorage, Subsystem: "backup", Code: "last_backup",
		Value: driver.Age(time.Duration(s.BackupAge) * time.Second),
		Note:  "when this database was last written somewhere else",
	}
	if s.RecoveryModel != "" {
		finding.Note += ", under " + s.RecoveryModel + " recovery"
	}
	switch {
	case s.BackupAge < 0:
		finding.Severity, finding.Value = driver.SeverityUnknown, "n/a"
	case s.BackupAge >= staleBackup:
		finding.Severity = driver.SeverityCritical
	case s.BackupAge >= oldBackup:
		finding.Severity = driver.SeverityWarn
	default:
		finding.Severity = driver.SeverityOK
	}
	return finding
}

// serverFinding is what the whole server takes. A login that may not see the
// other databases is shown every file it may see, which is none of them, and
// nought bytes would be a lie rather than a measurement.
func serverFinding(s Snapshot) driver.Finding {
	finding := driver.Finding{
		Group: driver.GroupStorage, Subsystem: "server", Code: "server_size",
		Value:    driver.ByteSize(s.ServerSize),
		Note:     "what every database on this server takes on disk",
		Severity: driver.SeverityInfo,
	}
	if s.ServerSize <= 0 {
		finding.Value, finding.Severity = "n/a", driver.SeverityUnknown
		finding.Note = "this login may not see the files of other databases"
	}
	return finding
}

// accessFinding says what this connection is allowed to do. SQL Server has no
// session setting that refuses a write, so this is what opendba decided rather
// than what the server was told.
func accessFinding(readOnly bool) driver.Finding {
	finding := driver.Finding{
		Group: driver.GroupLoad, Subsystem: "access", Code: "access_mode",
		Value:    "read / write",
		Note:     "opendba refuses writes before they are sent; the server is not told to",
		Severity: driver.SeverityWarn,
	}
	if readOnly {
		finding.Value, finding.Severity = "read only", driver.SeverityOK
	}
	return finding
}
