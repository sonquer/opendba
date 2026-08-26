package mssql

import (
	"context"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/sonquer/opendba/src/cli/internal/driver"
)

const defaultRowLimit = 1000

// unlimited is the row cap that is not one.
const unlimited = 0

type resultSet struct {
	rows      *sql.Rows
	tx        *sql.Tx
	ctx       context.Context
	release   context.CancelFunc
	columns   []string
	kinds     []string
	values    []any
	limit     int
	seen      int
	truncated bool
	writable  bool
	err       error
	duration  time.Duration
}

func (r *resultSet) Columns() []string { return r.columns }

func (r *resultSet) Values() []any { return r.values }

func (r *resultSet) Err() error { return r.err }

func (r *resultSet) Truncated() bool { return r.truncated }

func (r *resultSet) Duration() time.Duration { return r.duration }

func (r *resultSet) Next() bool {
	if r.err != nil || (r.limit > 0 && r.seen >= r.limit) {
		r.truncated = r.truncated || (r.err == nil && r.rows.Next())
		return false
	}
	if !r.rows.Next() {
		r.err = r.rows.Err()
		return false
	}
	targets := make([]any, len(r.columns))
	values := make([]any, len(r.columns))
	for i := range targets {
		targets[i] = &values[i]
	}
	if err := r.rows.Scan(targets...); err != nil {
		r.err = fmt.Errorf("read a row: %w", err)
		return false
	}
	r.values = natives(values, r.kinds)
	r.seen++
	return true
}

// Close finishes the transaction the statement ran in: a writable profile keeps
// its work, a read only profile throws it away.
func (r *resultSet) Close() error {
	closeErr := r.rows.Close()
	finish, wrap := r.tx.Rollback, "close the result: %w"
	if r.commits() {
		finish, wrap = r.tx.Commit, "commit the result: %w"
	}
	if err := finish(); err != nil && closeErr == nil {
		closeErr = err
	}
	r.release()
	if closeErr != nil {
		return fmt.Errorf(wrap, closeErr)
	}
	return nil
}

// commits reports whether the work this result did is worth keeping: a writable
// profile keeps it, unless the statement failed or the run was given up on.
func (r *resultSet) commits() bool {
	return r.writable && r.err == nil && r.ctx.Err() == nil
}

func (c *connection) Query(ctx context.Context, statement string) (driver.ResultSet, error) {
	return c.open(ctx, statement, rowLimit(c.config.RowLimit), c.config.Timeouts.Statement)
}

// Stream reads every row. The cap Query applies is a cap on what is worth
// drawing, and nothing about what is worth writing to a file, and neither is
// the deadline a screen is willing to wait.
func (c *connection) Stream(ctx context.Context, statement string) (driver.ResultSet, error) {
	return c.open(ctx, statement, unlimited, 0)
}

// open runs a statement inside a transaction. The transaction is never asked to
// be read only, because this server's driver refuses that outright; a read only
// profile is kept by throwing the transaction away instead.
func (c *connection) open(ctx context.Context, statement string, limit int, deadline time.Duration) (driver.ResultSet, error) {
	started := time.Now()
	ctx, release := watched(ctx, deadline)
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		release()
		return nil, fmt.Errorf("start a transaction: %w", err)
	}
	rows, err := tx.QueryContext(ctx, statement)
	if err != nil {
		_ = tx.Rollback()
		release()
		return nil, fmt.Errorf("run the query: %w", err)
	}
	columns, kinds, err := describe(rows)
	if err != nil {
		_ = rows.Close()
		_ = tx.Rollback()
		release()
		return nil, err
	}
	return &resultSet{
		rows:     rows,
		tx:       tx,
		ctx:      ctx,
		release:  release,
		columns:  columns,
		kinds:    kinds,
		limit:    limit,
		writable: !c.config.ReadOnly(),
		duration: time.Since(started),
	}, nil
}

// watched puts a deadline on a statement, which on this server is the only place
// one can be put. The deadline covers reading the rows as well as running the
// statement, because the rows arrive from the server as they are read.
func watched(ctx context.Context, deadline time.Duration) (context.Context, context.CancelFunc) {
	if deadline <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, deadline)
}

func describe(rows *sql.Rows) ([]string, []string, error) {
	types, err := rows.ColumnTypes()
	if err != nil {
		return nil, nil, fmt.Errorf("read the result columns: %w", err)
	}
	columns := make([]string, len(types))
	kinds := make([]string, len(types))
	for i, column := range types {
		columns[i] = column.Name()
		kinds[i] = strings.ToUpper(column.DatabaseTypeName())
	}
	return columns, kinds, nil
}

func rowLimit(configured int) int {
	if configured > 0 {
		return configured
	}
	return defaultRowLimit
}

func natives(values []any, kinds []string) []any {
	written := make([]any, len(values))
	for i, value := range values {
		kind := ""
		if i < len(kinds) {
			kind = kinds[i]
		}
		written[i] = native(value, kind)
	}
	return written
}

// exact are the types this server sends as raw bytes because no floating point
// number could hold them, and which a person reads as the digits they were
// written as.
var exact = map[string]bool{
	"DECIMAL": true, "NUMERIC": true, "MONEY": true, "SMALLMONEY": true,
}

// text are the types this server sends as bytes that are already characters.
var text = map[string]bool{"XML": true, "NTEXT": true, "TEXT": true}

// native turns what the server sent into what every screen and every exporter
// already knows how to draw.
func native(value any, kind string) any {
	held, ok := value.([]byte)
	if !ok {
		return value
	}
	switch {
	case kind == "UNIQUEIDENTIFIER":
		return guid(held)
	case exact[kind] || text[kind]:
		return string(held)
	default:
		return held
	}
}

// guid writes the sixteen bytes of a unique identifier the way SQL Server prints
// them, which reverses the first three groups.
func guid(held []byte) any {
	if len(held) != 16 {
		return held
	}
	ordered := []byte{
		held[3], held[2], held[1], held[0],
		held[5], held[4],
		held[7], held[6],
	}
	ordered = append(ordered, held[8:]...)
	written := hex.EncodeToString(ordered)
	return strings.ToUpper(strings.Join([]string{
		written[0:8], written[8:12], written[12:16], written[16:20], written[20:32],
	}, "-"))
}

// Preview is the statement that reads a table, written the way this server
// quotes a name.
func (c *connection) Preview(schema, table string) string {
	if schema == "" {
		return "SELECT * FROM " + Quote(table)
	}
	return "SELECT * FROM " + Quote(schema) + "." + Quote(table)
}

// Quote writes an identifier the way SQL Server reads one back: in brackets,
// with any closing bracket inside it doubled. Brackets are used rather than
// double quotes because they mean the same thing whether or not the session set
// QUOTED_IDENTIFIER.
func Quote(name string) string {
	return "[" + strings.ReplaceAll(name, "]", "]]") + "]"
}
