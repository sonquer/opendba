package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/sonquer/tui4db/src/cli/internal/driver"
)

func (c *connection) Health(ctx context.Context) ([]driver.Finding, error) {
	checks := []func(context.Context) (driver.Finding, error){
		c.integrity,
		c.foreignKeys,
		c.size,
		c.freeSpace,
		c.journal,
		c.sessionMode,
	}
	findings := make([]driver.Finding, 0, len(checks))
	for _, check := range checks {
		finding, err := check(ctx)
		if err != nil {
			return nil, err
		}
		findings = append(findings, finding)
	}
	return findings, nil
}

func (c *connection) integrity(ctx context.Context) (driver.Finding, error) {
	var result string
	if err := c.db.QueryRowContext(ctx, "PRAGMA integrity_check(1)").Scan(&result); err != nil {
		return driver.Finding{}, fmt.Errorf("check the database integrity: %w", err)
	}
	finding := driver.Finding{
		Group:     driver.GroupStorage,
		Subsystem: "integrity",
		Code:      "integrity_check",
		Value:     result,
		Note:      "every page reads back the way it was written",
		Severity:  driver.SeverityOK,
	}
	if !strings.EqualFold(result, "ok") {
		finding.Severity = driver.SeverityCritical
		finding.Note = "the database file is damaged"
	}
	return finding, nil
}

func (c *connection) foreignKeys(ctx context.Context) (driver.Finding, error) {
	violations := 0
	err := c.each(ctx, "PRAGMA foreign_key_check", func(*sql.Rows) error {
		violations++
		return nil
	})
	if err != nil {
		return driver.Finding{}, fmt.Errorf("check the foreign keys: %w", err)
	}
	finding := driver.Finding{
		Group:     driver.GroupScans,
		Subsystem: "foreign keys",
		Code:      "foreign_key_check",
		Note:      "every reference points at a row that exists",
		Value:     fmt.Sprintf("%d", violations),
		Severity:  driver.SeverityOK,
	}
	if violations > 0 {
		finding.Severity = driver.SeverityWarn
		finding.Note = "rows reference something that is not there"
	}
	return finding, nil
}

func (c *connection) size(ctx context.Context) (driver.Finding, error) {
	var pageCount, pageSize int64
	query := "SELECT (SELECT * FROM pragma_page_count()), (SELECT * FROM pragma_page_size())"
	if err := c.db.QueryRowContext(ctx, query).Scan(&pageCount, &pageSize); err != nil {
		return driver.Finding{}, fmt.Errorf("read the database size: %w", err)
	}
	return driver.Finding{
		Group:     driver.GroupStorage,
		Subsystem: "size",
		Code:      "database_size",
		Value:     ByteSize(pageCount * pageSize),
		Severity:  driver.SeverityInfo,
		Note:      fmt.Sprintf("%d pages of %s", pageCount, ByteSize(pageSize)),
	}, nil
}

func (c *connection) freeSpace(ctx context.Context) (driver.Finding, error) {
	var free, total int64
	query := "SELECT (SELECT * FROM pragma_freelist_count()), (SELECT * FROM pragma_page_count())"
	if err := c.db.QueryRowContext(ctx, query).Scan(&free, &total); err != nil {
		return driver.Finding{}, fmt.Errorf("read the free page count: %w", err)
	}
	finding := driver.Finding{
		Group:     driver.GroupMemory,
		Subsystem: "free pages",
		Code:      "freelist_count",
		Note:      "the file is as big as the data in it",
		Value:     fmt.Sprintf("%d", free),
		Severity:  driver.SeverityOK,
	}
	if total > 0 && free*4 > total {
		finding.Severity = driver.SeverityWarn
		finding.Note = "more than a quarter of the file is unused"
	}
	return finding.Measure(float64(free), float64(total)), nil
}

func (c *connection) journal(ctx context.Context) (driver.Finding, error) {
	var mode string
	if err := c.db.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode); err != nil {
		return driver.Finding{}, fmt.Errorf("read the journal mode: %w", err)
	}
	finding := driver.Finding{
		Group:     driver.GroupStorage,
		Subsystem: "journal",
		Code:      "journal_mode",
		Value:     mode,
		Severity:  driver.SeverityOK,
	}
	if !strings.EqualFold(mode, "wal") {
		finding.Severity = driver.SeverityInfo
		finding.Note = "wal keeps readers and writers out of each other's way"
	}
	return finding, nil
}

func (c *connection) sessionMode(ctx context.Context) (driver.Finding, error) {
	var queryOnly int64
	if err := c.db.QueryRowContext(ctx, "PRAGMA query_only").Scan(&queryOnly); err != nil {
		return driver.Finding{}, fmt.Errorf("read the session mode: %w", err)
	}
	finding := driver.Finding{
		Group:     driver.GroupLoad,
		Subsystem: "access",
		Code:      "query_only",
		Value:     "read / write",
		Severity:  driver.SeverityWarn,
		Note:      "this session may write",
	}
	if queryOnly == 1 {
		finding.Value = "read only"
		finding.Severity = driver.SeverityOK
		finding.Note = ""
	}
	return finding, nil
}

// ByteSize writes a size the way a person reads one.
func ByteSize(bytes int64) string { return driver.ByteSize(bytes) }
