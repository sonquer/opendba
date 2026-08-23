package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/sonquer/tui4db/src/cli/internal/driver"
)

const defaultRowLimit = 1000

func (c *connection) Indexes(ctx context.Context, schema string) ([]driver.Index, error) {
	schema = orMain(schema)
	tables, err := c.Tables(ctx, schema)
	if err != nil {
		return nil, err
	}
	definitions, err := c.indexDefinitions(ctx, schema)
	if err != nil {
		return nil, err
	}
	var indexes []driver.Index
	for _, table := range tables {
		listed, err := c.indexesOf(ctx, schema, table.Name, definitions)
		if err != nil {
			return nil, err
		}
		indexes = append(indexes, listed...)
	}
	return indexes, nil
}

func (c *connection) indexDefinitions(ctx context.Context, schema string) (map[string]string, error) {
	query := fmt.Sprintf("SELECT name, coalesce(sql, '') FROM %s.sqlite_schema WHERE type = 'index'", quote(schema))
	definitions := map[string]string{}
	err := c.each(ctx, query, func(rows *sql.Rows) error {
		var name, definition string
		if err := rows.Scan(&name, &definition); err != nil {
			return err
		}
		definitions[name] = definition
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("read the index definitions: %w", err)
	}
	return definitions, nil
}

func (c *connection) indexesOf(ctx context.Context, schema, table string, definitions map[string]string) ([]driver.Index, error) {
	var indexes []driver.Index
	err := c.each(ctx, fmt.Sprintf("PRAGMA %s.index_list(%s)", quote(schema), quote(table)), func(rows *sql.Rows) error {
		var (
			sequence int
			name     string
			unique   int
			origin   string
			partial  int
		)
		if err := rows.Scan(&sequence, &name, &unique, &origin, &partial); err != nil {
			return err
		}
		indexes = append(indexes, driver.Index{
			Schema:     schema,
			Table:      table,
			Name:       name,
			Definition: definitions[name],
			Unique:     unique == 1,
			Primary:    origin == "pk",
			Scans:      -1,
			Size:       -1,
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list the indexes of %s: %w", table, err)
	}
	return indexes, nil
}

type resultSet struct {
	rows      *sql.Rows
	tx        *sql.Tx
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
	targets := make([]any, len(r.columns))
	values := make([]any, len(r.columns))
	for i := range targets {
		targets[i] = &values[i]
	}
	if err := r.rows.Scan(targets...); err != nil {
		r.err = fmt.Errorf("read a row: %w", err)
		return false
	}
	r.values = values
	r.seen++
	return true
}

func (r *resultSet) Close() error {
	closeErr := r.rows.Close()
	if err := r.tx.Rollback(); err != nil && closeErr == nil {
		closeErr = err
	}
	if closeErr != nil {
		return fmt.Errorf("close the result: %w", closeErr)
	}
	return nil
}

func (c *connection) Query(ctx context.Context, statement string) (driver.ResultSet, error) {
	return c.open(ctx, statement, rowLimit(c.config.RowLimit))
}

// Stream reads every row. The cap Query applies is a cap on what is worth
// drawing, and nothing about what is worth writing to a file.
func (c *connection) Stream(ctx context.Context, statement string) (driver.ResultSet, error) {
	return c.open(ctx, statement, unlimited)
}

// unlimited is the row cap that is not one.
const unlimited = 0

func (c *connection) open(ctx context.Context, statement string, limit int) (driver.ResultSet, error) {
	started := time.Now()
	tx, err := c.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: c.config.ReadOnly()})
	if err != nil {
		return nil, fmt.Errorf("start a read-only transaction: %w", err)
	}
	rows, err := tx.QueryContext(ctx, statement)
	if err != nil {
		_ = tx.Rollback()
		return nil, fmt.Errorf("run the query: %w", err)
	}
	columns, err := rows.Columns()
	if err != nil {
		_ = rows.Close()
		_ = tx.Rollback()
		return nil, fmt.Errorf("read the result columns: %w", err)
	}
	return &resultSet{
		rows:     rows,
		tx:       tx,
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
	nodes := map[int]*driver.PlanNode{}
	root := &driver.PlanNode{Name: "query plan"}
	nodes[0] = root
	var lines []string
	err := c.each(ctx, "EXPLAIN QUERY PLAN "+statement, func(rows *sql.Rows) error {
		var (
			id     int
			parent int
			unused int
			detail string
		)
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			return err
		}
		node := driver.PlanNode{Name: planName(detail), Detail: detail}
		container, ok := nodes[parent]
		if !ok {
			container = root
		}
		node.Depth = container.Depth + 1
		container.Children = append(container.Children, node)
		nodes[id] = &container.Children[len(container.Children)-1]
		lines = append(lines, strings.Repeat("  ", node.Depth-1)+detail)
		return nil
	})
	if err != nil {
		return driver.Plan{}, fmt.Errorf("explain the query: %w", err)
	}
	return driver.Plan{Root: *root, Text: strings.Join(lines, "\n")}, nil
}

func planName(detail string) string {
	if index := strings.Index(detail, " "); index > 0 {
		return detail[:index]
	}
	return detail
}

// Preview reads a table. SQLite has one namespace per attached database, so the
// schema is the attachment name and is quoted the same way.
func (c *connection) Preview(schema, table string) string {
	if schema == "" {
		return "SELECT * FROM " + Quote(table)
	}
	return "SELECT * FROM " + Quote(schema) + "." + Quote(table)
}

// Quote writes an identifier the way SQLite reads one back: in double quotes,
// with any quote inside it doubled.
func Quote(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}
