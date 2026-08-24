package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/sonquer/opendba/src/cli/internal/driver"
)

const infoQuery = `SELECT current_setting('server_version'), current_database(), current_user,
	current_setting('transaction_read_only') = 'on',
	pg_has_role(current_user, 'pg_write_all_data', 'member') OR
		has_database_privilege(current_database(), 'CREATE'),
	usesuper
FROM pg_user WHERE usename = current_user`

const databasesQuery = `SELECT d.datname, d.datname = current_database(),
	coalesce(shobj_description(d.oid, 'pg_database'), '')
FROM pg_database d
WHERE d.datallowconn AND NOT d.datistemplate
	AND has_database_privilege(d.datname, 'CONNECT')
ORDER BY d.datname`

const schemasQuery = `SELECT n.nspname,
	count(c.oid) FILTER (WHERE c.relkind IN ('r','p','v','m','f')),
	n.nspname IN ('pg_catalog','information_schema') OR n.nspname LIKE 'pg_toast%'
FROM pg_namespace n
LEFT JOIN pg_class c ON c.relnamespace = n.oid
GROUP BY n.nspname
ORDER BY n.nspname`

const userSchemas = `n.nspname NOT IN ('pg_catalog','information_schema')
	AND n.nspname NOT LIKE 'pg\_toast%'`

const tablesQuery = `SELECT n.nspname, c.relname,
	CASE c.relkind WHEN 'r' THEN 'table' WHEN 'p' THEN 'partitioned table'
		WHEN 'v' THEN 'view' WHEN 'm' THEN 'materialized view' ELSE 'foreign table' END,
	coalesce(c.reltuples, 0)::bigint,
	pg_total_relation_size(c.oid),
	coalesce(obj_description(c.oid, 'pg_class'), ''),
	s.relid IS NOT NULL,
	coalesce(s.idx_scan, 0), coalesce(s.seq_scan, 0),
	coalesce(s.n_live_tup, 0), coalesce(s.n_dead_tup, 0),
	CASE WHEN coalesce(io.heap_blks_hit, 0) + coalesce(io.heap_blks_read, 0) > 0
		THEN coalesce(io.heap_blks_hit, 0)::float8
			/ (coalesce(io.heap_blks_hit, 0) + coalesce(io.heap_blks_read, 0))
		ELSE -1 END,
	coalesce(pg_indexes_size(c.oid), 0),
	coalesce(greatest(s.last_vacuum, s.last_autovacuum), to_timestamp(0))
FROM pg_class c
JOIN pg_namespace n ON n.oid = c.relnamespace
LEFT JOIN pg_stat_all_tables s ON s.relid = c.oid
LEFT JOIN pg_statio_all_tables io ON io.relid = c.oid
WHERE ($1 = '' OR n.nspname = $1) AND ` + userSchemas + `
	AND c.relkind IN ('r','p','v','m','f')
ORDER BY n.nspname, c.relname`

const columnsQuery = `SELECT a.attname,
	format_type(a.atttypid, a.atttypmod),
	NOT a.attnotnull,
	coalesce(pg_get_expr(d.adbin, d.adrelid), ''),
	coalesce(pk.is_primary, false),
	a.attnum,
	coalesce(col_description(a.attrelid, a.attnum), '')
FROM pg_attribute a
JOIN pg_class c ON c.oid = a.attrelid
JOIN pg_namespace n ON n.oid = c.relnamespace
LEFT JOIN pg_attrdef d ON d.adrelid = a.attrelid AND d.adnum = a.attnum
LEFT JOIN LATERAL (
	SELECT true AS is_primary FROM pg_constraint k
	WHERE k.conrelid = c.oid AND k.contype = 'p' AND a.attnum = ANY (k.conkey)
) pk ON true
WHERE ($1 = '' OR n.nspname = $1) AND c.relname = $2
	AND a.attnum > 0 AND NOT a.attisdropped
ORDER BY a.attnum`

const relationsQuery = `SELECT k.conname,
	sn.nspname, sc.relname,
	(SELECT array_agg(att.attname ORDER BY ordinality)
		FROM unnest(k.conkey) WITH ORDINALITY AS cols(attnum, ordinality)
		JOIN pg_attribute att ON att.attrelid = k.conrelid AND att.attnum = cols.attnum),
	tn.nspname, tc.relname,
	(SELECT array_agg(att.attname ORDER BY ordinality)
		FROM unnest(k.confkey) WITH ORDINALITY AS cols(attnum, ordinality)
		JOIN pg_attribute att ON att.attrelid = k.confrelid AND att.attnum = cols.attnum),
	k.confdeltype, k.condeferrable, pg_get_constraintdef(k.oid),
	tc.relname = $2 AND tn.nspname = $1
FROM pg_constraint k
JOIN pg_class sc ON sc.oid = k.conrelid
JOIN pg_namespace sn ON sn.oid = sc.relnamespace
JOIN pg_class tc ON tc.oid = k.confrelid
JOIN pg_namespace tn ON tn.oid = tc.relnamespace
WHERE k.contype = 'f'
	AND ((($1 = '' OR sn.nspname = $1) AND sc.relname = $2)
		OR (($1 = '' OR tn.nspname = $1) AND tc.relname = $2))
ORDER BY k.conname`

const indexesQuery = `SELECT n.nspname, t.relname, i.relname,
	pg_get_indexdef(x.indexrelid),
	pg_relation_size(x.indexrelid),
	coalesce(s.idx_scan, 0),
	coalesce(s.idx_tup_read, 0),
	x.indisunique, x.indisprimary,
	s.indexrelid IS NOT NULL
FROM pg_index x
JOIN pg_class i ON i.oid = x.indexrelid
JOIN pg_class t ON t.oid = x.indrelid
JOIN pg_namespace n ON n.oid = t.relnamespace
LEFT JOIN pg_stat_all_indexes s ON s.indexrelid = x.indexrelid
WHERE ($1 = '' OR n.nspname = $1) AND ` + userSchemas + `
	AND t.relkind IN ('r','p','m')
ORDER BY pg_relation_size(x.indexrelid) DESC, i.relname`

func (c *connection) Info(ctx context.Context) (driver.ServerInfo, error) {
	info := driver.ServerInfo{Driver: Name, ConnectedAt: time.Now(), ReadOnly: c.config.ReadOnly()}
	var sessionReadOnly bool
	err := c.db.QueryRow(ctx, infoQuery).Scan(
		&info.Version, &info.Database, &info.User, &sessionReadOnly, &info.CanWrite, &info.Superuser)
	if err != nil {
		return driver.ServerInfo{}, fmt.Errorf("read the server version: %w", err)
	}
	info.ReadOnly = sessionReadOnly
	return info, nil
}

func (c *connection) each(ctx context.Context, query string, scan func(pgx.Rows) error, args ...any) error {
	rows, err := c.db.Query(ctx, query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := scan(rows); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (c *connection) Databases(ctx context.Context) ([]driver.Database, error) {
	var databases []driver.Database
	err := c.each(ctx, databasesQuery, func(rows pgx.Rows) error {
		var database driver.Database
		if err := rows.Scan(&database.Name, &database.Current, &database.Comment); err != nil {
			return err
		}
		databases = append(databases, database)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list the databases: %w", err)
	}
	return databases, nil
}

func (c *connection) Schemas(ctx context.Context) ([]driver.Schema, error) {
	var schemas []driver.Schema
	err := c.each(ctx, schemasQuery, func(rows pgx.Rows) error {
		var schema driver.Schema
		if err := rows.Scan(&schema.Name, &schema.Tables, &schema.System); err != nil {
			return err
		}
		schemas = append(schemas, schema)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list the schemas: %w", err)
	}
	return schemas, nil
}

func (c *connection) Tables(ctx context.Context, schema string) ([]driver.Table, error) {
	var tables []driver.Table
	err := c.each(ctx, tablesQuery, func(rows pgx.Rows) error {
		var table driver.Table
		var hit float64
		var vacuumed time.Time
		if err := rows.Scan(&table.Schema, &table.Name, &table.Kind,
			&table.Rows, &table.Size, &table.Comment,
			&table.Stats, &table.IndexScans, &table.SeqScans,
			&table.LiveRows, &table.DeadRows, &hit, &table.IndexSize, &vacuumed); err != nil {
			return err
		}
		if hit >= 0 {
			table.CacheHit = hit
		}
		if table.Rows < 0 {
			table.Rows = table.LiveRows
		}
		if vacuumed.Unix() > 0 {
			table.LastVacuum = vacuumed
		}
		tables = append(tables, table)
		return nil
	}, schema)
	if err != nil {
		return nil, fmt.Errorf("list the tables in %s: %w", where(schema), err)
	}
	return tables, nil
}

func (c *connection) Columns(ctx context.Context, schema, table string) ([]driver.Column, error) {
	var columns []driver.Column
	err := c.each(ctx, columnsQuery, func(rows pgx.Rows) error {
		var column driver.Column
		if err := rows.Scan(&column.Name, &column.Type, &column.Nullable, &column.Default,
			&column.PrimaryKey, &column.Position, &column.Comment); err != nil {
			return err
		}
		columns = append(columns, column)
		return nil
	}, schema, table)
	if err != nil {
		return nil, fmt.Errorf("describe %s.%s: %w", schema, table, err)
	}
	relations, err := c.Relations(ctx, schema, table)
	if err != nil {
		return nil, err
	}
	markForeignKeys(columns, relations)
	return columns, nil
}

func markForeignKeys(columns []driver.Column, relations []driver.Relation) {
	for _, relation := range relations {
		if relation.Inbound {
			continue
		}
		for i, name := range relation.FromColumns {
			target := relation.ToTable
			if i < len(relation.ToColumns) {
				target += "." + relation.ToColumns[i]
			}
			for index := range columns {
				if columns[index].Name == name {
					columns[index].ForeignKey = target
				}
			}
		}
	}
}

func (c *connection) Relations(ctx context.Context, schema, table string) ([]driver.Relation, error) {
	var relations []driver.Relation
	err := c.each(ctx, relationsQuery, func(rows pgx.Rows) error {
		var relation driver.Relation
		var onDelete string
		if err := rows.Scan(&relation.Name, &relation.FromSchema, &relation.FromTable, &relation.FromColumns,
			&relation.ToSchema, &relation.ToTable, &relation.ToColumns,
			&onDelete, &relation.Deferrable, &relation.ConstraintDef, &relation.Inbound); err != nil {
			return err
		}
		relation.OnDelete = DeleteAction(onDelete)
		relations = append(relations, relation)
		return nil
	}, schema, table)
	if err != nil {
		return nil, fmt.Errorf("read the relations of %s.%s: %w", schema, table, err)
	}
	return relations, nil
}

func DeleteAction(code string) string {
	switch code {
	case "a":
		return "NO ACTION"
	case "r":
		return "RESTRICT"
	case "c":
		return "CASCADE"
	case "n":
		return "SET NULL"
	case "d":
		return "SET DEFAULT"
	default:
		return strings.ToUpper(code)
	}
}

func (c *connection) Indexes(ctx context.Context, schema string) ([]driver.Index, error) {
	var indexes []driver.Index
	err := c.each(ctx, indexesQuery, func(rows pgx.Rows) error {
		var index driver.Index
		if err := rows.Scan(&index.Schema, &index.Table, &index.Name, &index.Definition,
			&index.Size, &index.Scans, &index.Rows,
			&index.Unique, &index.Primary, &index.Stats); err != nil {
			return err
		}
		indexes = append(indexes, index)
		return nil
	}, schema)
	if err != nil {
		return nil, fmt.Errorf("list the indexes in %s: %w", where(schema), err)
	}
	return indexes, nil
}

// where names the place a listing came from. PostgreSQL takes an empty schema
// to mean every schema a user owns, which is what a fresh connection asks for.
func where(schema string) string {
	if strings.TrimSpace(schema) == "" {
		return "every schema"
	}
	return schema
}
