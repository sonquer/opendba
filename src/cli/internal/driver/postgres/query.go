package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/sonquer/tui4db/src/cli/internal/driver"
)

const defaultRowLimit = 1000

type resultSet struct {
	rows      pgx.Rows
	tx        pgx.Tx
	ctx       context.Context
	columns   []string
	values    []any
	limit     int
	seen      int
	truncated bool
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
	values, err := r.rows.Values()
	if err != nil {
		r.err = fmt.Errorf("read a row: %w", err)
		return false
	}
	r.values = values
	r.seen++
	return true
}

func (r *resultSet) Close() error {
	r.rows.Close()
	if err := r.tx.Rollback(r.ctx); err != nil {
		return fmt.Errorf("close the result: %w", err)
	}
	return nil
}

// Query runs a statement inside a transaction of its own. On a read only
// profile the transaction is opened read only, which is a layer rather than a
// repetition: the session is already set that way, and this holds whether or
// not that setting took.
func (c *connection) Query(ctx context.Context, statement string) (driver.ResultSet, error) {
	return c.open(ctx, statement, rowLimit(c.config.RowLimit), false)
}

// Stream reads every row, and lifts the statement timeout for the length of its
// transaction. The session sets that timeout for the sake of a screen nobody
// wants to watch hang; a file being written is watched, is counted, and is
// stopped by whoever asked for it.
func (c *connection) Stream(ctx context.Context, statement string) (driver.ResultSet, error) {
	return c.open(ctx, statement, unlimited, true)
}

// unlimited is the row cap that is not one.
const unlimited = 0

// NoStatementTimeout lifts the session timeout for the rest of a transaction.
// It is LOCAL, so it is given back when the transaction ends whatever happens
// to the connection afterwards.
const NoStatementTimeout = "SET LOCAL statement_timeout = 0"

func (c *connection) open(ctx context.Context, statement string, limit int, patient bool) (driver.ResultSet, error) {
	started := time.Now()
	tx, err := c.db.BeginTx(ctx, c.access())
	if err != nil {
		return nil, fmt.Errorf("start a read-only transaction: %w", err)
	}
	if patient {
		if _, err := tx.Exec(ctx, NoStatementTimeout); err != nil {
			_ = tx.Rollback(ctx)
			return nil, fmt.Errorf("lift the statement timeout: %w", err)
		}
	}
	rows, err := tx.Query(ctx, statement)
	if err != nil {
		_ = tx.Rollback(ctx)
		return nil, fmt.Errorf("run the query: %w", err)
	}
	columns := make([]string, 0, len(rows.FieldDescriptions()))
	for _, field := range rows.FieldDescriptions() {
		columns = append(columns, field.Name)
	}
	return &resultSet{
		rows:     rows,
		tx:       tx,
		ctx:      ctx,
		columns:  columns,
		limit:    limit,
		duration: time.Since(started),
	}, nil
}

func rowLimit(configured int) int {
	if configured > 0 {
		return configured
	}
	return defaultRowLimit
}

func (c *connection) Explain(ctx context.Context, statement string, analyze bool) (driver.Plan, error) {
	var document string
	if err := c.db.QueryRow(ctx, ExplainStatement(statement, analyze)).Scan(&document); err != nil {
		return driver.Plan{}, fmt.Errorf("explain the query: %w", err)
	}
	return ParsePlan(document)
}

func ExplainStatement(statement string, analyze bool) string {
	options := "FORMAT JSON, COSTS ON, VERBOSE OFF"
	if analyze {
		options = "ANALYZE, BUFFERS, " + options
	}
	return "EXPLAIN (" + options + ") " + statement
}

type planDocument struct {
	Plan planEntry `json:"Plan"`
}

type planEntry struct {
	NodeType      string      `json:"Node Type"`
	RelationName  string      `json:"Relation Name"`
	IndexName     string      `json:"Index Name"`
	TotalCost     float64     `json:"Total Cost"`
	PlanRows      int64       `json:"Plan Rows"`
	ActualRows    int64       `json:"Actual Rows"`
	ActualTotal   float64     `json:"Actual Total Time"`
	Plans         []planEntry `json:"Plans"`
	JoinType      string      `json:"Join Type"`
	ScanDirection string      `json:"Scan Direction"`
}

func ParsePlan(document string) (driver.Plan, error) {
	var documents []planDocument
	if err := json.Unmarshal([]byte(document), &documents); err != nil {
		return driver.Plan{}, fmt.Errorf("read the query plan: %w", err)
	}
	if len(documents) == 0 {
		return driver.Plan{}, fmt.Errorf("the server returned an empty query plan")
	}
	root := convertPlan(documents[0].Plan, 0)
	return driver.Plan{Root: root, Total: root.Cost, Text: PlanText(root)}, nil
}

func convertPlan(entry planEntry, depth int) driver.PlanNode {
	node := driver.PlanNode{
		Name:     entry.NodeType,
		Detail:   planDetail(entry),
		Cost:     entry.TotalCost,
		Rows:     entry.PlanRows,
		Depth:    depth,
		Duration: time.Duration(entry.ActualTotal * float64(time.Millisecond)),
	}
	if entry.ActualRows > 0 {
		node.Rows = entry.ActualRows
	}
	for _, child := range entry.Plans {
		node.Children = append(node.Children, convertPlan(child, depth+1))
	}
	return node
}

func planDetail(entry planEntry) string {
	parts := make([]string, 0, 3)
	if entry.IndexName != "" {
		parts = append(parts, "using "+entry.IndexName)
	}
	if entry.RelationName != "" {
		parts = append(parts, "on "+entry.RelationName)
	}
	if entry.JoinType != "" {
		parts = append(parts, strings.ToLower(entry.JoinType)+" join")
	}
	return strings.Join(parts, " ")
}

func PlanText(node driver.PlanNode) string {
	var lines []string
	var walk func(driver.PlanNode)
	walk = func(current driver.PlanNode) {
		line := strings.Repeat("  ", current.Depth) + current.Name
		if current.Detail != "" {
			line += " " + current.Detail
		}
		lines = append(lines, line)
		for _, child := range current.Children {
			walk(child)
		}
	}
	walk(node)
	return strings.Join(lines, "\n")
}

func (c *connection) access() pgx.TxOptions {
	if c.config.ReadOnly() {
		return pgx.TxOptions{AccessMode: pgx.ReadOnly}
	}
	return pgx.TxOptions{}
}

// Preview reads a table. PostgreSQL folds an unquoted name to lower case, so a
// table created as "Orders" is only reachable quoted.
func (c *connection) Preview(schema, table string) string {
	return "SELECT * FROM " + Quote(schema) + "." + Quote(table)
}

// Quote writes an identifier the way PostgreSQL reads one back: in double
// quotes, with any quote inside it doubled.
func Quote(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}
