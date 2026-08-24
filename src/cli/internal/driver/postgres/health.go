package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/sonquer/tui4db/src/cli/internal/driver"
)

const (
	memoryQuery = `SELECT
	(SELECT coalesce(round(100.0 * sum(blks_hit) / nullif(sum(blks_hit) + sum(blks_read), 0), 1), 100.0)
		FROM pg_stat_database WHERE datname = current_database()),
	(SELECT coalesce(sum(temp_files), 0) FROM pg_stat_database WHERE datname = current_database()),
	(SELECT coalesce(sum(temp_bytes), 0) FROM pg_stat_database WHERE datname = current_database()),
	(SELECT setting::bigint * 8192 FROM pg_settings WHERE name = 'shared_buffers'),
	(SELECT setting::bigint * 1024 FROM pg_settings WHERE name = 'work_mem'),
	(SELECT coalesce(round(100.0 * sum(idx_blks_hit) / nullif(sum(idx_blks_hit) + sum(idx_blks_read), 0), 1), 100.0)
		FROM pg_statio_user_indexes)`

	loadQuery = `SELECT
	(SELECT count(*) FROM pg_stat_activity WHERE datname = current_database()),
	(SELECT setting::bigint FROM pg_settings WHERE name = 'max_connections'),
	(SELECT count(*) FROM pg_stat_activity WHERE state = 'active'),
	(SELECT count(*) FROM pg_stat_activity WHERE state = 'idle in transaction'),
	(SELECT count(*) FROM pg_locks WHERE NOT granted),
	(SELECT coalesce(max(extract(epoch FROM clock_timestamp() - query_start)), 0)
		FROM pg_stat_activity WHERE state = 'active' AND backend_type = 'client backend'),
	(SELECT coalesce(sum(deadlocks), 0) FROM pg_stat_database WHERE datname = current_database()),
	(SELECT coalesce(round(100.0 * sum(xact_rollback) / nullif(sum(xact_commit) + sum(xact_rollback), 0), 1), 0.0)
		FROM pg_stat_database WHERE datname = current_database()),
	coalesce((SELECT wait_event_type FROM pg_stat_activity
		WHERE state = 'active' AND wait_event_type NOT IN ('Client', 'Activity')
		GROUP BY wait_event_type ORDER BY count(*) DESC LIMIT 1), ''),
	current_setting('transaction_read_only') = 'on',
	(SELECT coalesce(max(extract(epoch FROM clock_timestamp() - state_change)), 0)
		FROM pg_stat_activity WHERE state = 'idle in transaction')`

	scansQuery = `SELECT
	(SELECT coalesce(sum(seq_scan), 0) FROM pg_stat_user_tables),
	(SELECT coalesce(sum(idx_scan), 0) FROM pg_stat_user_tables),
	(SELECT coalesce(sum(n_dead_tup), 0) FROM pg_stat_user_tables),
	(SELECT coalesce(sum(n_live_tup), 0) FROM pg_stat_user_tables),
	(SELECT coalesce(sum(pg_relation_size(indexrelid)), 0) FROM pg_stat_user_indexes WHERE idx_scan = 0),
	(SELECT coalesce(sum(pg_relation_size(indexrelid)), 0) FROM pg_stat_user_indexes),
	(SELECT count(*) FROM pg_stat_user_indexes WHERE idx_scan = 0),
	(SELECT coalesce(extract(epoch FROM now() - min(greatest(last_vacuum, last_autovacuum))), 0)
		FROM pg_stat_user_tables WHERE greatest(last_vacuum, last_autovacuum) IS NOT NULL),
	(SELECT count(*) FROM pg_stat_user_tables WHERE greatest(last_vacuum, last_autovacuum) IS NULL)`

	storageQuery = `SELECT
	pg_database_size(current_database()),
	(SELECT age(datfrozenxid) FROM pg_database WHERE datname = current_database()),
	(SELECT setting::bigint FROM pg_settings WHERE name = 'autovacuum_freeze_max_age'),
	(SELECT count(*) FROM pg_replication_slots WHERE NOT active),
	(SELECT coalesce(sum(pg_database_size(oid)), 0) FROM pg_database
		WHERE datallowconn AND has_database_privilege(oid, 'CONNECT')),
	(SELECT count(*) FROM pg_database WHERE datallowconn),
	CASE WHEN pg_has_role(current_user, 'pg_monitor', 'member')
		THEN (SELECT coalesce(sum(size), 0) FROM pg_ls_waldir()) ELSE -1 END,
	(SELECT setting::bigint * 1024 * 1024 FROM pg_settings WHERE name = 'max_wal_size')`

	checkpointsQuery   = `SELECT num_timed, num_requested FROM pg_stat_checkpointer`
	oldCheckpointQuery = `SELECT checkpoints_timed, checkpoints_req FROM pg_stat_bgwriter`
)

// Snapshot is everything the server was willing to say about itself.
type Snapshot struct {
	CacheHitRatio float64
	TempFiles     int64
	TempBytes     int64
	SharedBuffers int64
	WorkMem       int64
	IndexHitRatio float64

	Connections    int64
	MaxConnections int64
	Active         int64
	IdleInTx       int64
	WaitingLocks   int64
	LongestSeconds float64
	Deadlocks      int64
	RollbackRatio  float64
	WaitingOn      string
	ReadOnly       bool
	IdleSeconds    float64

	SeqScans        int64
	IndexScans      int64
	DeadTuples      int64
	LiveTuples      int64
	UnusedIndexSize int64
	TotalIndexSize  int64
	UnusedIndexes   int64
	VacuumSeconds   float64
	NeverVacuumed   int64

	DatabaseSize      int64
	TransactionAge    int64
	FreezeMaxAge      int64
	InactiveSlots     int64
	TimedCheckpoints  int64
	ForcedCheckpoints int64
	ServerSize        int64
	Databases         int64
	WalSize           int64
	MaxWalSize        int64

	Refused map[string]string
}

func (s Snapshot) refused(group string) (string, bool) {
	reason, ok := s.Refused[group]
	return reason, ok
}

func (c *connection) Health(ctx context.Context) ([]driver.Finding, error) {
	snapshot := Snapshot{Refused: map[string]string{}}
	if err := c.db.QueryRow(ctx, memoryQuery).Scan(&snapshot.CacheHitRatio, &snapshot.TempFiles,
		&snapshot.TempBytes, &snapshot.SharedBuffers, &snapshot.WorkMem,
		&snapshot.IndexHitRatio); err != nil {
		snapshot.Refused[driver.GroupMemory] = err.Error()
	}
	if err := c.db.QueryRow(ctx, loadQuery).Scan(&snapshot.Connections, &snapshot.MaxConnections,
		&snapshot.Active, &snapshot.IdleInTx, &snapshot.WaitingLocks, &snapshot.LongestSeconds,
		&snapshot.Deadlocks, &snapshot.RollbackRatio, &snapshot.WaitingOn, &snapshot.ReadOnly,
		&snapshot.IdleSeconds); err != nil {
		snapshot.Refused[driver.GroupLoad] = err.Error()
	}
	if err := c.db.QueryRow(ctx, scansQuery).Scan(&snapshot.SeqScans, &snapshot.IndexScans,
		&snapshot.DeadTuples, &snapshot.LiveTuples, &snapshot.UnusedIndexSize,
		&snapshot.TotalIndexSize, &snapshot.UnusedIndexes,
		&snapshot.VacuumSeconds, &snapshot.NeverVacuumed); err != nil {
		snapshot.Refused[driver.GroupScans] = err.Error()
	}
	if err := c.db.QueryRow(ctx, storageQuery).Scan(&snapshot.DatabaseSize, &snapshot.TransactionAge,
		&snapshot.FreezeMaxAge, &snapshot.InactiveSlots, &snapshot.ServerSize,
		&snapshot.Databases, &snapshot.WalSize, &snapshot.MaxWalSize); err != nil {
		snapshot.Refused[driver.GroupStorage] = err.Error()
	}
	c.checkpoints(ctx, &snapshot)
	if len(snapshot.Refused) == len(groups) {
		return nil, fmt.Errorf("read the server statistics: %s", snapshot.Refused[driver.GroupLoad])
	}
	return Findings(snapshot), nil
}

// checkpoints reads the counter that moved out of pg_stat_bgwriter in PostgreSQL
// 17.
func (c *connection) checkpoints(ctx context.Context, snapshot *Snapshot) {
	if err := c.db.QueryRow(ctx, checkpointsQuery).
		Scan(&snapshot.TimedCheckpoints, &snapshot.ForcedCheckpoints); err == nil {
		return
	}
	_ = c.db.QueryRow(ctx, oldCheckpointQuery).
		Scan(&snapshot.TimedCheckpoints, &snapshot.ForcedCheckpoints)
}

var groups = []string{driver.GroupMemory, driver.GroupLoad, driver.GroupScans, driver.GroupStorage}

func Findings(snapshot Snapshot) []driver.Finding {
	findings := make([]driver.Finding, 0, 21)
	for _, group := range groups {
		if reason, refused := snapshot.refused(group); refused {
			findings = append(findings, driver.Finding{
				Group:     group,
				Subsystem: group,
				Code:      "unavailable",
				Value:     "not available",
				Note:      reason,
				Severity:  driver.SeverityUnknown,
			})
			continue
		}
		findings = append(findings, byGroup[group](snapshot)...)
	}
	return findings
}

var byGroup = map[string]func(Snapshot) []driver.Finding{
	driver.GroupMemory:  memoryFindings,
	driver.GroupLoad:    loadFindings,
	driver.GroupScans:   scanFindings,
	driver.GroupStorage: storageFindings,
}

func memoryFindings(snapshot Snapshot) []driver.Finding {
	return []driver.Finding{
		cacheFinding(snapshot),
		indexCacheFinding(snapshot),
		spillFinding(snapshot),
		buffersFinding(snapshot),
	}
}

// The measured readings of a group sit together, because a row with no bar
// between two rows with one breaks the column of bars in half.
func loadFindings(snapshot Snapshot) []driver.Finding {
	return []driver.Finding{
		connectionsFinding(snapshot),
		waitingFinding(snapshot),
		rollbacksFinding(snapshot),
		longestFinding(snapshot),
		idleFinding(snapshot),
		deadlocksFinding(snapshot),
		accessFinding(snapshot),
	}
}

func scanFindings(snapshot Snapshot) []driver.Finding {
	return []driver.Finding{
		fullScanFinding(snapshot),
		deadRowsFinding(snapshot),
		indexesFinding(snapshot),
		vacuumFinding(snapshot),
	}
}

// size is last because it is the least actionable number here: it is always
// OK, and a group is read from the top.
func storageFindings(snapshot Snapshot) []driver.Finding {
	return []driver.Finding{
		wraparoundFinding(snapshot),
		checkpointFinding(snapshot),
		walFinding(snapshot),
		replicationFinding(snapshot),
		sizeFinding(snapshot),
		serverFinding(snapshot),
	}
}

// ByteSize and Duration read the way the driver package writes them.
func ByteSize(bytes int64) string { return driver.ByteSize(bytes) }

func Duration(d time.Duration) string { return driver.Duration(d) }

func cacheFinding(snapshot Snapshot) driver.Finding {
	finding := driver.Finding{
		Group:     driver.GroupMemory,
		Subsystem: "cache",
		Code:      "cache_hit_ratio",
		Value:     fmt.Sprintf("%.1f%%", snapshot.CacheHitRatio),
		Note:      "almost every read came from memory",
		Severity:  driver.SeverityOK,
	}
	switch {
	case snapshot.CacheHitRatio < 90:
		finding.Severity = driver.SeverityCritical
		finding.Note = "most reads are going to disk"
	case snapshot.CacheHitRatio < 99:
		finding.Severity = driver.SeverityWarn
		finding.Note = "one read in a hundred goes to disk, so the cache may be too small"
	}
	return finding.Measure(snapshot.CacheHitRatio, 100)
}

func spillFinding(snapshot Snapshot) driver.Finding {
	finding := driver.Finding{
		Group:     driver.GroupMemory,
		Subsystem: "spilled",
		Code:      "temp_files",
		Value:     ByteSize(snapshot.TempBytes),
		Note:      "no query has run out of memory",
		Severity:  driver.SeverityOK,
	}
	if snapshot.TempFiles > 0 {
		finding.Severity = driver.SeverityWarn
		finding.Note = fmt.Sprintf("%d times a query ran out of working memory and wrote to disk",
			snapshot.TempFiles)
	}
	return finding
}

func buffersFinding(snapshot Snapshot) driver.Finding {
	return driver.Finding{
		Group:     driver.GroupMemory,
		Subsystem: "buffers",
		Code:      "shared_buffers",
		Value:     ByteSize(snapshot.SharedBuffers),
		Note:      "memory held for pages, with " + ByteSize(snapshot.WorkMem) + " per sort",
		Severity:  driver.SeverityOK,
	}
}

func connectionsFinding(snapshot Snapshot) driver.Finding {
	finding := driver.Finding{
		Group:     driver.GroupLoad,
		Subsystem: "connections",
		Code:      "connections",
		Value:     fmt.Sprintf("%d/%d", snapshot.Connections, snapshot.MaxConnections),
		Note:      fmt.Sprintf("%d of them are working right now", snapshot.Active),
		Severity:  driver.SeverityOK,
	}
	if snapshot.IdleInTx > 0 {
		finding.Note = fmt.Sprintf("%d idle in transaction, holding locks for nothing", snapshot.IdleInTx)
		finding.Severity = driver.SeverityWarn
	}
	if snapshot.MaxConnections > 0 && float64(snapshot.Connections) > 0.9*float64(snapshot.MaxConnections) {
		finding.Severity = driver.SeverityCritical
		finding.Note = "almost every connection slot is taken"
	}
	return finding.Measure(float64(snapshot.Connections), float64(snapshot.MaxConnections))
}

func waitingFinding(snapshot Snapshot) driver.Finding {
	finding := driver.Finding{
		Group:     driver.GroupLoad,
		Subsystem: "waiting",
		Code:      "waiting_locks",
		Value:     fmt.Sprintf("%d", snapshot.WaitingLocks),
		Note:      "nothing is blocked",
		Severity:  driver.SeverityOK,
	}
	if snapshot.WaitingOn != "" {
		finding.Note = "the busiest wait is on " + snapshot.WaitingOn
	}
	if snapshot.WaitingLocks > 0 {
		finding.Severity = driver.SeverityCritical
		finding.Note = "something is blocked behind another transaction"
	}
	return finding.Measure(float64(snapshot.WaitingLocks), float64(snapshot.Active))
}

func longestFinding(snapshot Snapshot) driver.Finding {
	longest := time.Duration(max(snapshot.LongestSeconds, 0) * float64(time.Second))
	finding := driver.Finding{
		Group:     driver.GroupLoad,
		Subsystem: "longest",
		Code:      "long_running",
		Value:     Duration(longest),
		Note:      "nothing has been running for long",
		Severity:  driver.SeverityOK,
	}
	switch {
	case longest > 5*time.Minute:
		finding.Severity = driver.SeverityCritical
		finding.Note = "a statement has been running for minutes and may be stuck"
	case longest > time.Minute:
		finding.Severity = driver.SeverityWarn
		finding.Note = "a statement has been running for over a minute"
	}
	return finding
}

func rollbacksFinding(snapshot Snapshot) driver.Finding {
	finding := driver.Finding{
		Group:     driver.GroupLoad,
		Subsystem: "rollbacks",
		Code:      "rollback_ratio",
		Value:     fmt.Sprintf("%.1f%%", snapshot.RollbackRatio),
		Note:      "almost every transaction finishes",
		Severity:  driver.SeverityOK,
	}
	if snapshot.RollbackRatio > 5 {
		finding.Severity = driver.SeverityWarn
		finding.Note = "the application may be failing more often than it thinks"
	}
	return finding.Measure(snapshot.RollbackRatio, 100)
}

func deadlocksFinding(snapshot Snapshot) driver.Finding {
	finding := driver.Finding{
		Group:     driver.GroupLoad,
		Subsystem: "deadlocks",
		Code:      "deadlocks",
		Value:     fmt.Sprintf("%d", snapshot.Deadlocks),
		Note:      "no transaction has been killed to break a deadlock",
		Severity:  driver.SeverityOK,
	}
	if snapshot.Deadlocks > 0 {
		finding.Severity = driver.SeverityWarn
		finding.Note = "transactions have been killed to break deadlocks since the counters were reset"
	}
	return finding
}

func fullScanFinding(snapshot Snapshot) driver.Finding {
	total := snapshot.SeqScans + snapshot.IndexScans
	share := 0.0
	if total > 0 {
		share = 100 * float64(snapshot.SeqScans) / float64(total)
	}
	finding := driver.Finding{
		Group:     driver.GroupScans,
		Subsystem: "full scans",
		Code:      "sequential_scans",
		Value:     fmt.Sprintf("%.0f%%", share),
		Note:      "reads go through indexes",
		Severity:  driver.SeverityOK,
	}
	switch {
	case share > 50:
		finding.Severity = driver.SeverityCritical
		finding.Note = "most reads walk whole tables instead of using an index"
	case share > 20:
		finding.Severity = driver.SeverityWarn
		finding.Note = "one read in five walks a whole table"
	}
	return finding.Measure(share, 100)
}

func deadRowsFinding(snapshot Snapshot) driver.Finding {
	total := snapshot.DeadTuples + snapshot.LiveTuples
	share := 0.0
	if total > 0 {
		share = 100 * float64(snapshot.DeadTuples) / float64(total)
	}
	finding := driver.Finding{
		Group:     driver.GroupScans,
		Subsystem: "dead rows",
		Code:      "dead_tuples",
		Value:     fmt.Sprintf("%.0f%%", share),
		Note:      "the vacuum is keeping up",
		Severity:  driver.SeverityOK,
	}
	switch {
	case share > 30:
		finding.Severity = driver.SeverityCritical
		finding.Note = fmt.Sprintf("%d deleted rows are still on disk and slowing reads", snapshot.DeadTuples)
	case share > 10:
		finding.Severity = driver.SeverityWarn
		finding.Note = fmt.Sprintf("the vacuum is behind by %d rows", snapshot.DeadTuples)
	}
	return finding.Measure(share, 100)
}

func indexesFinding(snapshot Snapshot) driver.Finding {
	finding := driver.Finding{
		Group:     driver.GroupScans,
		Subsystem: "idle indexes",
		Code:      "unused_indexes",
		Value:     ByteSize(snapshot.UnusedIndexSize),
		Note:      "every index earns its keep",
		Severity:  driver.SeverityOK,
	}
	if snapshot.UnusedIndexes > 0 {
		finding.Note = fmt.Sprintf("%d indexes have never been used on this node", snapshot.UnusedIndexes)
		finding.Severity = driver.SeverityWarn
	}
	return finding.Measure(float64(snapshot.UnusedIndexSize), float64(snapshot.TotalIndexSize))
}

func sizeFinding(snapshot Snapshot) driver.Finding {
	return driver.Finding{
		Group:     driver.GroupStorage,
		Subsystem: "size",
		Code:      "database_size",
		Value:     ByteSize(snapshot.DatabaseSize),
		Note:      "everything this database holds",
		Severity:  driver.SeverityOK,
	}
}

func wraparoundFinding(snapshot Snapshot) driver.Finding {
	share := 0.0
	if snapshot.FreezeMaxAge > 0 {
		share = 100 * float64(snapshot.TransactionAge) / float64(snapshot.FreezeMaxAge)
	}
	finding := driver.Finding{
		Group:     driver.GroupStorage,
		Subsystem: "wraparound",
		Code:      "transaction_age",
		Value:     fmt.Sprintf("%.0f%%", share),
		Note:      "the freeze cycle is comfortable",
		Severity:  driver.SeverityOK,
	}
	switch {
	case share > 90:
		finding.Severity = driver.SeverityCritical
		finding.Note = "the database stops accepting writes when this reaches the limit"
	case share > 70:
		finding.Severity = driver.SeverityWarn
		finding.Note = "the freeze cycle is falling behind"
	}
	return finding.Measure(share, 100)
}

func checkpointFinding(snapshot Snapshot) driver.Finding {
	total := snapshot.TimedCheckpoints + snapshot.ForcedCheckpoints
	share := 0.0
	if total > 0 {
		share = 100 * float64(snapshot.ForcedCheckpoints) / float64(total)
	}
	finding := driver.Finding{
		Group:     driver.GroupStorage,
		Subsystem: "checkpoints",
		Code:      "forced_checkpoints",
		Value:     fmt.Sprintf("%.0f%%", share),
		Note:      "writes are spread out the way they were planned",
		Severity:  driver.SeverityOK,
	}
	if share > 30 {
		finding.Severity = driver.SeverityWarn
		finding.Note = "writes arrive faster than the planned checkpoints, so they are being forced"
	}
	return finding.Measure(share, 100)
}

func replicationFinding(snapshot Snapshot) driver.Finding {
	finding := driver.Finding{
		Group:     driver.GroupStorage,
		Subsystem: "replication",
		Code:      "inactive_slots",
		Value:     fmt.Sprintf("%d inactive", snapshot.InactiveSlots),
		Note:      "no slot is holding write ahead logs back",
		Severity:  driver.SeverityOK,
	}
	if snapshot.InactiveSlots > 0 {
		finding.Severity = driver.SeverityCritical
		finding.Note = "an inactive slot keeps write ahead logs until the disk fills"
	}
	return finding
}

func accessFinding(snapshot Snapshot) driver.Finding {
	if snapshot.ReadOnly {
		return driver.Finding{
			Group:     driver.GroupLoad,
			Subsystem: "access",
			Code:      "transaction_read_only",
			Value:     "read only",
			Note:      "this session cannot change anything",
			Severity:  driver.SeverityOK,
		}
	}
	return driver.Finding{
		Group:     driver.GroupLoad,
		Subsystem: "access",
		Code:      "transaction_read_only",
		Value:     "read / write",
		Note:      "this session may change data",
		Severity:  driver.SeverityWarn,
	}
}

// indexCacheFinding is the other half of the cache reading: a table can be in
// memory while the indexes used to find rows in it are not, and a lookup that
// goes to disk is a lookup that costs whatever an index was meant to save.
func indexCacheFinding(snapshot Snapshot) driver.Finding {
	finding := driver.Finding{
		Group:     driver.GroupMemory,
		Subsystem: "index cache",
		Code:      "index_hit_ratio",
		Value:     fmt.Sprintf("%.1f%%", snapshot.IndexHitRatio),
		Note:      "the lookups themselves are in memory too",
		Severity:  driver.SeverityOK,
	}
	switch {
	case snapshot.IndexHitRatio < 90:
		finding.Severity = driver.SeverityCritical
		finding.Note = "most index lookups are read from disk before a row is even found"
	case snapshot.IndexHitRatio < 99:
		finding.Severity = driver.SeverityWarn
		finding.Note = "some index lookups go to disk, which costs what the index was for"
	}
	return finding.Measure(snapshot.IndexHitRatio, 100)
}

// idleFinding is the session that opened a transaction and walked away.
func idleFinding(snapshot Snapshot) driver.Finding {
	idle := time.Duration(snapshot.IdleSeconds * float64(time.Second))
	finding := driver.Finding{
		Group:     driver.GroupLoad,
		Subsystem: "idle in transaction",
		Code:      "idle_in_transaction",
		Value:     fmt.Sprintf("%d", snapshot.IdleInTx),
		Note:      "nobody is holding a transaction open and doing nothing with it",
		Severity:  driver.SeverityOK,
	}
	if snapshot.IdleInTx == 0 {
		return finding
	}
	finding.Severity = driver.SeverityWarn
	finding.Note = "a transaction has been open and idle for " + Duration(idle) +
		", holding its locks and keeping vacuum from finishing"
	if idle > stuckTransaction {
		finding.Severity = driver.SeverityCritical
		finding.Note = "a transaction has been open and idle for " + Duration(idle) +
			", which is long enough to be a client that crashed or a session someone forgot"
	}
	return finding.Measure(float64(snapshot.IdleInTx), float64(snapshot.Connections))
}

const stuckTransaction = 5 * time.Minute

// vacuumFinding is whether the cleaner has been round.
func vacuumFinding(snapshot Snapshot) driver.Finding {
	since := time.Duration(snapshot.VacuumSeconds * float64(time.Second))
	finding := driver.Finding{
		Group:     driver.GroupScans,
		Subsystem: "vacuum",
		Code:      "vacuum_age",
		Value:     driver.Age(since),
		Note:      "every table has been cleaned recently",
		Severity:  driver.SeverityOK,
	}
	if snapshot.NeverVacuumed > 0 {
		finding.Severity = driver.SeverityWarn
		finding.Value = fmt.Sprintf("%d never", snapshot.NeverVacuumed)
		finding.Note = fmt.Sprintf("%d tables have never been vacuumed, so nothing has ever "+
			"reclaimed the rows they have replaced", snapshot.NeverVacuumed)
		return finding
	}
	switch {
	case since > staleVacuum:
		finding.Severity = driver.SeverityCritical
		finding.Note = "the oldest table was last cleaned " + driver.Age(since) +
			", which usually means autovacuum is off or something is blocking it"
	case since > tiredVacuum:
		finding.Severity = driver.SeverityWarn
		finding.Note = "the oldest table was last cleaned " + driver.Age(since)
	}
	return finding
}

const (
	tiredVacuum = 7 * 24 * time.Hour
	staleVacuum = 30 * 24 * time.Hour
)

// walFinding is how much write ahead log is on disk.
func walFinding(snapshot Snapshot) driver.Finding {
	if snapshot.WalSize < 0 {
		return driver.Finding{
			Group:     driver.GroupStorage,
			Subsystem: "wal",
			Code:      "wal_size",
			Value:     "n/a",
			Note:      "reading the write ahead log needs the pg_monitor role",
			Severity:  driver.SeverityUnknown,
		}
	}
	finding := driver.Finding{
		Group:     driver.GroupStorage,
		Subsystem: "wal",
		Code:      "wal_size",
		Value:     ByteSize(snapshot.WalSize),
		Note:      "the write ahead log is the size the server was configured for",
		Severity:  driver.SeverityOK,
	}
	room := snapshot.MaxWalSize * walRunway
	if room > 0 && snapshot.WalSize > room {
		finding.Severity = driver.SeverityWarn
		finding.Note = "the write ahead log is more than " + fmt.Sprintf("%d", walRunway) +
			" times max_wal_size, so either checkpoints are behind or a slot is holding it"
	}
	return finding.Measure(float64(snapshot.WalSize), float64(room))
}

// walRunway is how far past max_wal_size the log may sit before it is worth
// saying so. The directory routinely holds more than one checkpoint's worth.
const walRunway = 2

// serverFinding is everything the instance holds, not just this database.
func serverFinding(snapshot Snapshot) driver.Finding {
	return driver.Finding{
		Group:     driver.GroupStorage,
		Subsystem: "server",
		Code:      "server_size",
		Value:     ByteSize(snapshot.ServerSize),
		Note: fmt.Sprintf("%d databases on this server, this one included. Free space on the "+
			"machine is not something a SQL connection can see", snapshot.Databases),
		Severity: driver.SeverityOK,
	}
}
