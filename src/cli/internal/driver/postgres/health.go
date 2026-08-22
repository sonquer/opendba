package postgres

import (
	"context"
	"fmt"

	"github.com/sonquer/tui4db/src/cli/internal/driver"
)

const healthQuery = `SELECT
	(SELECT count(*) FROM pg_stat_activity WHERE datname = current_database()),
	(SELECT setting::bigint FROM pg_settings WHERE name = 'max_connections'),
	(SELECT count(*) FROM pg_stat_activity WHERE state = 'idle in transaction'),
	(SELECT coalesce(round(100.0 * sum(blks_hit) / nullif(sum(blks_hit) + sum(blks_read), 0), 1), 100.0)
		FROM pg_stat_database WHERE datname = current_database()),
	(SELECT count(*) FROM pg_locks WHERE NOT granted),
	(SELECT coalesce(round(100.0 * sum(xact_rollback) / nullif(sum(xact_commit) + sum(xact_rollback), 0), 1), 0.0)
		FROM pg_stat_database WHERE datname = current_database()),
	(SELECT coalesce(sum(pg_relation_size(indexrelid)), 0) FROM pg_stat_user_indexes WHERE idx_scan = 0),
	(SELECT count(*) FROM pg_stat_user_indexes WHERE idx_scan = 0),
	(SELECT count(*) FROM pg_stat_activity WHERE state = 'active'
		AND now() - query_start > interval '1 minute'),
	(SELECT count(*) FROM pg_replication_slots WHERE NOT active),
	current_setting('transaction_read_only') = 'on'`

type Snapshot struct {
	Connections     int64
	MaxConnections  int64
	IdleInTx        int64
	CacheHitRatio   float64
	WaitingLocks    int64
	RollbackRatio   float64
	UnusedIndexSize int64
	UnusedIndexes   int64
	LongQueries     int64
	InactiveSlots   int64
	ReadOnly        bool
}

func (c *connection) Health(ctx context.Context) ([]driver.Finding, error) {
	var snapshot Snapshot
	err := c.db.QueryRow(ctx, healthQuery).Scan(
		&snapshot.Connections, &snapshot.MaxConnections, &snapshot.IdleInTx,
		&snapshot.CacheHitRatio, &snapshot.WaitingLocks, &snapshot.RollbackRatio,
		&snapshot.UnusedIndexSize, &snapshot.UnusedIndexes, &snapshot.LongQueries,
		&snapshot.InactiveSlots, &snapshot.ReadOnly)
	if err != nil {
		return nil, fmt.Errorf("read the server statistics: %w", err)
	}
	return Findings(snapshot), nil
}

func Findings(snapshot Snapshot) []driver.Finding {
	return []driver.Finding{
		connectionsFinding(snapshot),
		cacheFinding(snapshot),
		locksFinding(snapshot),
		rollbacksFinding(snapshot),
		indexesFinding(snapshot),
		longQueriesFinding(snapshot),
		replicationFinding(snapshot),
		accessFinding(snapshot),
	}
}

func connectionsFinding(snapshot Snapshot) driver.Finding {
	finding := driver.Finding{
		Subsystem: "connections",
		Code:      "connections",
		Value:     fmt.Sprintf("%d/%d", snapshot.Connections, snapshot.MaxConnections),
		Severity:  driver.SeverityOK,
	}
	if snapshot.IdleInTx > 0 {
		finding.Note = fmt.Sprintf("%d idle in transaction", snapshot.IdleInTx)
		finding.Severity = driver.SeverityWarn
	}
	if snapshot.MaxConnections > 0 && float64(snapshot.Connections) > 0.9*float64(snapshot.MaxConnections) {
		finding.Severity = driver.SeverityCritical
		finding.Note = "almost every connection slot is taken"
	}
	return finding
}

func cacheFinding(snapshot Snapshot) driver.Finding {
	finding := driver.Finding{
		Subsystem: "cache",
		Code:      "cache_hit_ratio",
		Value:     fmt.Sprintf("%.1f%%", snapshot.CacheHitRatio),
		Severity:  driver.SeverityOK,
	}
	switch {
	case snapshot.CacheHitRatio < 90:
		finding.Severity = driver.SeverityCritical
		finding.Note = "most reads are going to disk"
	case snapshot.CacheHitRatio < 99:
		finding.Severity = driver.SeverityWarn
		finding.Note = "shared buffers may be too small"
	}
	return finding
}

func locksFinding(snapshot Snapshot) driver.Finding {
	finding := driver.Finding{
		Subsystem: "locks",
		Code:      "waiting_locks",
		Value:     fmt.Sprintf("%d waiting", snapshot.WaitingLocks),
		Severity:  driver.SeverityOK,
	}
	if snapshot.WaitingLocks > 0 {
		finding.Severity = driver.SeverityCritical
		finding.Note = "something is blocked behind another transaction"
	}
	return finding
}

func rollbacksFinding(snapshot Snapshot) driver.Finding {
	finding := driver.Finding{
		Subsystem: "rollbacks",
		Code:      "rollback_ratio",
		Value:     fmt.Sprintf("%.1f%%", snapshot.RollbackRatio),
		Severity:  driver.SeverityOK,
	}
	if snapshot.RollbackRatio > 5 {
		finding.Severity = driver.SeverityWarn
		finding.Note = "the application may be failing more often than it thinks"
	}
	return finding
}

func indexesFinding(snapshot Snapshot) driver.Finding {
	finding := driver.Finding{
		Subsystem: "indexes",
		Code:      "unused_indexes",
		Value:     ByteSize(snapshot.UnusedIndexSize),
		Severity:  driver.SeverityOK,
	}
	if snapshot.UnusedIndexes > 0 {
		finding.Note = fmt.Sprintf("%d with zero scans on this node", snapshot.UnusedIndexes)
		finding.Severity = driver.SeverityWarn
	}
	return finding
}

func longQueriesFinding(snapshot Snapshot) driver.Finding {
	finding := driver.Finding{
		Subsystem: "queries",
		Code:      "long_running",
		Value:     fmt.Sprintf("%d", snapshot.LongQueries),
		Severity:  driver.SeverityOK,
	}
	if snapshot.LongQueries > 0 {
		finding.Severity = driver.SeverityWarn
		finding.Note = "running for more than a minute"
	}
	return finding
}

func replicationFinding(snapshot Snapshot) driver.Finding {
	finding := driver.Finding{
		Subsystem: "replication",
		Code:      "inactive_slots",
		Value:     fmt.Sprintf("%d inactive", snapshot.InactiveSlots),
		Severity:  driver.SeverityOK,
	}
	if snapshot.InactiveSlots > 0 {
		finding.Severity = driver.SeverityCritical
		finding.Note = "an inactive slot holds write ahead log forever"
	}
	return finding
}

func accessFinding(snapshot Snapshot) driver.Finding {
	if snapshot.ReadOnly {
		return driver.Finding{
			Subsystem: "access",
			Code:      "transaction_read_only",
			Value:     "read only",
			Severity:  driver.SeverityOK,
		}
	}
	return driver.Finding{
		Subsystem: "access",
		Code:      "transaction_read_only",
		Value:     "read / write",
		Severity:  driver.SeverityWarn,
		Note:      "this session may write",
	}
}

func ByteSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	value, exponent := float64(bytes), 0
	for value >= unit && exponent < 4 {
		value /= unit
		exponent++
	}
	return fmt.Sprintf("%.1f %ciB", value, "KMGT"[exponent-1])
}
