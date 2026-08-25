package mssql

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/sonquer/opendba/src/cli/internal/driver"
)

// IndexColumn is one column of an index, and whether it is part of the key or
// only carried along with it.
type IndexColumn struct {
	Name       string
	Descending bool
	Included   bool
}

func (c *connection) Indexes(ctx context.Context, schema string) ([]driver.Index, error) {
	kinds := map[string]string{}
	filters := map[string]string{}
	var indexes []driver.Index
	err := c.each(ctx, indexesQuery, func(rows *sql.Rows) error {
		index := driver.Index{Scans: unmeasured, Rows: unmeasured}
		var kind, filter string
		if err := rows.Scan(&index.Schema, &index.Table, &index.Name,
			&index.Unique, &index.Primary, &kind, &filter, &index.Size); err != nil {
			return err
		}
		kinds[index.Qualified()+"."+index.Table] = kind
		filters[index.Qualified()+"."+index.Table] = filter
		indexes = append(indexes, index)
		return nil
	}, schema)
	if err != nil {
		return nil, fmt.Errorf("list the indexes in %s: %w", where(schema), err)
	}
	columns, err := c.indexColumns(ctx, schema)
	if err != nil {
		return nil, err
	}
	for i, index := range indexes {
		key := index.Qualified() + "." + index.Table
		indexes[i].Definition = IndexDefinition(index, kinds[key], filters[key], columns[key])
	}
	c.countIndexReads(ctx, schema, indexes)
	return indexes, nil
}

func (c *connection) indexColumns(ctx context.Context, schema string) (map[string][]IndexColumn, error) {
	columns := map[string][]IndexColumn{}
	err := c.each(ctx, indexColumnsQuery, func(rows *sql.Rows) error {
		var schemaName, table, index string
		var column IndexColumn
		if err := rows.Scan(&schemaName, &table, &index, &column.Name,
			&column.Descending, &column.Included); err != nil {
			return err
		}
		key := schemaName + "." + index + "." + table
		columns[key] = append(columns[key], column)
		return nil
	}, schema)
	if err != nil {
		return nil, fmt.Errorf("read the index columns in %s: %w", where(schema), err)
	}
	return columns, nil
}

// countIndexReads fills in how often each index has been used, and leaves them
// unmeasured when this login may not see the server's counters.
func (c *connection) countIndexReads(ctx context.Context, schema string, indexes []driver.Index) {
	counted := map[string]int64{}
	err := c.each(ctx, indexUsageQuery, func(rows *sql.Rows) error {
		var schemaName, table, index string
		var scans int64
		if err := rows.Scan(&schemaName, &table, &index, &scans); err != nil {
			return err
		}
		counted[schemaName+"."+index+"."+table] = scans
		return nil
	}, schema)
	if err != nil {
		return
	}
	for i, index := range indexes {
		indexes[i].Scans, indexes[i].Stats = counted[index.Qualified()+"."+index.Table], true
	}
}

// IndexDefinition writes an index as the statement that would create it. SQL
// Server keeps no text of that statement, so it is put back together from the
// catalog.
func IndexDefinition(index driver.Index, kind, filter string, columns []IndexColumn) string {
	var keys, included []string
	for _, column := range columns {
		if column.Included {
			included = append(included, Quote(column.Name))
			continue
		}
		order := " ASC"
		if column.Descending {
			order = " DESC"
		}
		keys = append(keys, Quote(column.Name)+order)
	}
	words := []string{"CREATE"}
	if index.Unique {
		words = append(words, "UNIQUE")
	}
	if shape := strings.ReplaceAll(kind, "_", " "); shape != "" {
		words = append(words, shape)
	}
	definition := strings.Join(words, " ") + " INDEX " + Quote(index.Name) +
		" ON " + Quote(index.Schema) + "." + Quote(index.Table) +
		" (" + strings.Join(keys, ", ") + ")"
	if len(included) > 0 {
		definition += " INCLUDE (" + strings.Join(included, ", ") + ")"
	}
	if filter != "" {
		definition += " WHERE " + filter
	}
	return definition
}
