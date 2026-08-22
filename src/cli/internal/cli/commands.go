package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/sonquer/tui4db/src/cli/internal/config"
	"github.com/sonquer/tui4db/src/cli/internal/driver"
	"github.com/sonquer/tui4db/src/cli/internal/report"
	"github.com/sonquer/tui4db/src/cli/internal/ui"
	"github.com/sonquer/tui4db/src/cli/pkg/sqlguard"
)

func Timeouts(safety config.SafetySettings) driver.Timeouts {
	timeouts := driver.DefaultTimeouts()
	if statement, err := time.ParseDuration(safety.QueryTimeout); err == nil && statement > 0 {
		timeouts.Statement = statement
	}
	if lock, err := time.ParseDuration(safety.LockTimeout); err == nil && lock > 0 {
		timeouts.Lock = lock
	}
	return timeouts
}

func (a App) inspect(ctx context.Context, session Session, opts options) int {
	findings, err := session.Conn.Health(ctx)
	if err != nil {
		fmt.Fprintln(a.Stderr, err)
		return ExitFailure
	}
	document := report.New(session.Info, session.Connection.Name).WithFindings(findings)
	if opts.json {
		return a.writeJSON(document)
	}
	a.header(session)
	fmt.Fprintln(a.Stdout, session.Theme.FindingTable(findings))
	fmt.Fprintln(a.Stdout, session.Theme.Counts(document.Counts.Healthy, document.Counts.Warnings, document.Counts.Failing))
	if !document.Healthy() {
		return ExitFailure
	}
	return ExitOK
}

func (a App) schema(ctx context.Context, session Session, opts options) int {
	tables, err := session.Conn.Tables(ctx, opts.schema)
	if err != nil {
		fmt.Fprintln(a.Stderr, err)
		return ExitFailure
	}
	document := report.New(session.Info, session.Connection.Name).WithTables(tables)
	if opts.json {
		return a.writeJSON(document)
	}
	a.header(session)
	fmt.Fprintln(a.Stdout, session.Theme.TableList(tables, -1, ui.MaxTextWidth))
	return ExitOK
}

func (a App) indexes(ctx context.Context, session Session, opts options) int {
	indexes, err := session.Conn.Indexes(ctx, opts.schema)
	if err != nil {
		fmt.Fprintln(a.Stderr, err)
		return ExitFailure
	}
	document := report.New(session.Info, session.Connection.Name).WithIndexes(indexes)
	if opts.json {
		return a.writeJSON(document)
	}
	a.header(session)
	fmt.Fprintln(a.Stdout, session.Theme.IndexList(indexes, -1, ui.MaxTextWidth))
	return ExitOK
}

func (a App) query(ctx context.Context, session Session, opts options, statement string) int {
	verdict := session.Guard.Classify(statement, Mode(session.Connection.Mode))
	if !verdict.Allowed() {
		fmt.Fprintln(a.Stderr, session.Theme.Verdict(verdict, 0))
		return ExitBlocked
	}
	result, err := session.Conn.Query(ctx, statement)
	if err != nil {
		fmt.Fprintln(a.Stderr, err)
		return ExitFailure
	}
	defer func() { _ = result.Close() }()

	columns := result.Columns()
	var rows [][]string
	for result.Next() {
		rows = append(rows, ui.Strings(result.Values()))
	}
	if err := result.Err(); err != nil {
		fmt.Fprintln(a.Stderr, err)
		return ExitFailure
	}
	if opts.json {
		return a.writeRows(session, statement, columns, rows, result)
	}
	fmt.Fprint(a.Stdout, session.Theme.ResultTable(columns, rows))
	fmt.Fprintln(a.Stdout, session.Theme.ResultFooter(len(rows), result.Duration(), result.Truncated()))
	return ExitOK
}

func (a App) writeRows(session Session, statement string, columns []string, rows [][]string, result driver.ResultSet) int {
	document := map[string]any{
		"schema_version": report.SchemaVersion,
		"connection":     session.Connection.Name,
		"statement":      report.Normalize(statement),
		"columns":        columns,
		"rows":           rows,
		"row_count":      len(rows),
		"truncated":      result.Truncated(),
		"duration_ms":    result.Duration().Milliseconds(),
	}
	if err := writeJSONValue(a.Stdout, document); err != nil {
		fmt.Fprintln(a.Stderr, err)
		return ExitFailure
	}
	return ExitOK
}

func (a App) writeJSON(document report.Report) int {
	if err := report.WriteJSON(a.Stdout, document); err != nil {
		fmt.Fprintln(a.Stderr, err)
		return ExitFailure
	}
	if !document.Healthy() {
		return ExitFailure
	}
	return ExitOK
}

func (a App) header(session Session) {
	fmt.Fprintln(a.Stdout, session.Theme.ConnectionLine(
		session.Connection.Name,
		ui.EnvColor(session.Connection.Color),
		strings.ToLower(session.Info.Driver)+" "+session.Info.Version,
		modeLabel(session),
	))
}

func modeLabel(session Session) string {
	return sqlguard.Mode(session.Connection.Mode).Label()
}
