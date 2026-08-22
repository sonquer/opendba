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
	(SELECT setting::bigint * 1024 FROM pg_settings WHERE name = 'work_mem')`

	loadQuery = `SELECT
	(SELECT count(*) FROM pg_stat_activity WHERE datname = current_database()),
	(SELECT setting::bigint FROM pg_settings WHERE name = 'max_connections'),
	(SELECT count(*) FROM pg_stat_activity WHERE state = 'active'),
	(SELECT count(*) FROM pg_stat_activity WHERE state = 'idle in transaction'),
	(SELECT count(*) FROM pg_locks WHERE NOT granted),
	(SELECT coalesce(max(extract(epoch FROM now() - query_start)), 0)
		FROM pg_stat_activity WHERE state = 'active' AND backend_type = 'client backend'),
	(SELECT coalesce(sum(deadlocks), 0) FROM pg_stat_database WHERE datname = current_database()),
	(SELECT coalesce(round(100.0 * sum(xact_rollback) / nullif(sum(xact_commit) + sum(xact_rollback), 0), 1), 0.0)
		FROM pg_stat_database WHERE datname = current_database()),
	(SELECT coalesce(wait_event_type, '') FROM pg_stat_activity
		WHERE wait_event_type IS NOT NULL AND state = 'active'
		GROUP BY wait_event_type ORDER BY count(*) DESC LIMIT 1),
	current_setting('transaction_read_only') = 'on'`

	scansQuery = `SELECT
	(SELECT coalesce(sum(seq_scan), 0) FROM pg_stat_user_tables),
	(SELECT coalesce(sum(idx_scan), 0) FROM pg_stat_user_tables),
	(SELECT coalesce(sum(n_dead_tup), 0) FROM pg_stat_user_tables),
	(SELECT coalesce(sum(n_live_tup), 0) FROM pg_stat_user_tables),
	(SELECT coalesce(sum(pg_relation_size(indexrelid)), 0) FROM pg_stat_user_indexes WHERE idx_scan = 0),
	(SELECT coalesce(sum(pg_relation_size(indexrelid)), 0) FROM pg_stat_user_indexes),
	(SELECT count(*) FROM pg_stat_user_indexes WHERE idx_scan = 0)`

	storageQuery = `SELECT
	pg_database_size(current_database()),
	(SELECT age(datfrozenxid) FROM pg_database WHERE datname = current_database()),
	(SELECT setting::bigint FROM pg_settings WHERE name = 'autovacuum_freeze_max_age'),
	(SELECT count(*) FROM pg_replication_slots WHERE NOT active)`

	checkpointsQuery   = `SELECT num_timed, num_requested FROM pg_stat_checkpointer`
	oldCheckpointQuery = `SELECT checkpoints_timed, checkpoints_req FROM pg_stat_bgwriter`
)

// Snapshot is everything the server was willing to say about itself. A group
// the server refused is recorded in Refused rather than failing the report,
// because a locked down role should still see the rest.
type Snapshot struct {
	CacheHitRatio float64
	TempFiles     int64
	TempBytes     int64
	SharedBuffers int64
	WorkMem       int64

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

	SeqScans        int64
	IndexScans      int64
	DeadTuples      int64
	LiveTuples      int64
	UnusedIndexSize int64
	TotalIndexSize  int64
	UnusedIndexes   int64

	DatabaseSize      int64
	TransactionAge    int64
	FreezeMaxAge      int64
	InactiveSlots     int64
	TimedCheckpoints  int64
	ForcedCheckpoints int64

	Refused map[string]string
}

func (s Snapshot) refused(group string) (string, bool) {
	reason, ok := s.Refused[group]
	return reason, ok
}

func (c *connection) Health(ctx context.Context) ([]driver.Finding, error) {
	snapshot := Snapshot{Refused: map[string]string{}}
	if err := c.db.QueryRow(ctx, memoryQuery).Scan(&snapshot.CacheHitRatio, &snapshot.TempFiles,
		&snapshot.TempBytes, &snapshot.SharedBuffers, &snapshot.WorkMem); err != nil {
		snapshot.Refused[driver.GroupMemory] = err.Error()
	}
	if err := c.db.QueryRow(ctx, loadQuery).Scan(&snapshot.Connections, &snapshot.MaxConnections,
		&snapshot.Active, &snapshot.IdleInTx, &snapshot.WaitingLocks, &snapshot.LongestSeconds,
		&snapshot.Deadlocks, &snapshot.RollbackRatio, &snapshot.WaitingOn, &snapshot.ReadOnly); err != nil {
		snapshot.Refused[driver.GroupLoad] = err.Error()
	}
	if err := c.db.QueryRow(ctx, scansQuery).Scan(&snapshot.SeqScans, &snapshot.IndexScans,
		&snapshot.DeadTuples, &snapshot.LiveTuples, &snapshot.UnusedIndexSize,
		&snapshot.TotalIndexSize, &snapshot.UnusedIndexes); err != nil {
		snapshot.Refused[driver.GroupScans] = err.Error()
	}
	if err := c.db.QueryRow(ctx, storageQuery).Scan(&snapshot.DatabaseSize, &snapshot.TransactionAge,
		&snapshot.FreezeMaxAge, &snapshot.InactiveSlots); err != nil {
		snapshot.Refused[driver.GroupStorage] = err.Error()
	}
	c.checkpoints(ctx, &snapshot)
	if len(snapshot.Refused) == len(groups) {
		return nil, fmt.Errorf("read the server statistics: %s", snapshot.Refused[driver.GroupLoad])
	}
	return Findings(snapshot), nil
}

// checkpoints reads the counter that moved out of pg_stat_bgwriter in
// PostgreSQL 17. Asking the newer view first and falling back costs one failed
// query on an older server and no version bookkeeping.
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
	findings := make([]driver.Finding, 0, 16)
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
		spillFinding(snapshot),
		buffersFinding(snapshot),
	}
}

func loadFindings(snapshot Snapshot) []driver.Finding {
	return []driver.Finding{
		connectionsFinding(snapshot),
		waitingFinding(snapshot),
		longestFinding(snapshot),
		rollbacksFinding(snapshot),
		deadlocksFinding(snapshot),
		accessFinding(snapshot),
	}
}

func scanFindings(snapshot Snapshot) []driver.Finding {
	return []driver.Finding{
		fullScanFinding(snapshot),
		deadRowsFinding(snapshot),
		indexesFinding(snapshot),
	}
}

func storageFindings(snapshot Snapshot) []driver.Finding {
	return []driver.Finding{
		sizeFinding(snapshot),
		wraparoundFinding(snapshot),
		checkpointFinding(snapshot),
		replicationFinding(snapshot),
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
	longest := time.Duration(snapshot.LongestSeconds * float64(time.Second))
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
