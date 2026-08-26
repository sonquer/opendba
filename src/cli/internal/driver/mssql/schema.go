package mssql

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/sonquer/opendba/src/cli/internal/driver"
)

// infoQuery is what the server says about itself and about what this login may
// do with it.
const infoQuery = `SELECT
	CAST(SERVERPROPERTY('ProductVersion') AS nvarchar(128)),
	CAST(SERVERPROPERTY('Edition') AS nvarchar(128)),
	DB_NAME(),
	SUSER_SNAME(),
	CAST(CASE WHEN DATABASEPROPERTYEX(DB_NAME(), 'Updateability') = N'READ_ONLY' THEN 1 ELSE 0 END AS bit),
	CAST(CASE WHEN ISNULL(IS_MEMBER('db_owner'), 0) = 1
	            OR ISNULL(IS_MEMBER('db_datawriter'), 0) = 1
	            OR ISNULL(IS_SRVROLEMEMBER('sysadmin'), 0) = 1
	          THEN 1 ELSE 0 END AS bit),
	CAST(ISNULL(IS_SRVROLEMEMBER('sysadmin'), 0) AS bit)`

// databasesQuery lists the databases this login can actually open. HAS_DBACCESS
// answers nothing for a database that is offline or being restored, which is
// why the test is for one rather than for not zero.
const databasesQuery = `SELECT d.name,
	CAST(CASE WHEN d.database_id = DB_ID() THEN 1 ELSE 0 END AS bit),
	d.state_desc + N' - ' + d.recovery_model_desc
FROM sys.databases d
WHERE HAS_DBACCESS(d.name) = 1
ORDER BY d.name`

// systemSchemas are the schemas SQL Server puts in every database: its own
// catalog, and one for each fixed database role.
const systemSchemas = `(N'sys', N'INFORMATION_SCHEMA', N'guest', N'db_owner', N'db_accessadmin',
	N'db_securityadmin', N'db_ddladmin', N'db_backupoperator', N'db_datareader',
	N'db_datawriter', N'db_denydatareader', N'db_denydatawriter')`

const schemasQuery = `SELECT s.name,
	(SELECT COUNT(*) FROM sys.objects o WHERE o.schema_id = s.schema_id AND o.type IN ('U','V')),
	CAST(CASE WHEN s.name IN ` + systemSchemas + ` THEN 1 ELSE 0 END AS bit)
FROM sys.schemas s
ORDER BY s.name`

// tablesQuery reads what the catalog knows, which is everything except how the
// tables are being used. Page counts are eight kilobytes each.
const tablesQuery = `SELECT s.name, t.name,
	CASE t.type WHEN 'V' THEN 'view' ELSE 'table' END,
	ISNULL(p.rows, 0),
	ISNULL(a.total_bytes, 0),
	ISNULL(CAST(ep.value AS nvarchar(4000)), N''),
	ISNULL(x.index_bytes, 0),
	d.refreshed
FROM sys.objects t
JOIN sys.schemas s ON s.schema_id = t.schema_id
OUTER APPLY (SELECT SUM(pt.rows) AS rows FROM sys.partitions pt
	WHERE pt.object_id = t.object_id AND pt.index_id IN (0,1)) p
OUTER APPLY (SELECT SUM(au.total_pages) * 8192 AS total_bytes FROM sys.partitions pt
	JOIN sys.allocation_units au ON au.container_id = pt.partition_id
	WHERE pt.object_id = t.object_id) a
OUTER APPLY (SELECT SUM(au.used_pages) * 8192 AS index_bytes FROM sys.partitions pt
	JOIN sys.allocation_units au ON au.container_id = pt.partition_id
	WHERE pt.object_id = t.object_id AND pt.index_id > 1) x
OUTER APPLY (SELECT MAX(STATS_DATE(st.object_id, st.stats_id)) AS refreshed
	FROM sys.stats st WHERE st.object_id = t.object_id) d
LEFT JOIN sys.extended_properties ep ON ep.major_id = t.object_id AND ep.minor_id = 0
	AND ep.class = 1 AND ep.name = N'MS_Description'
WHERE t.type IN ('U','V') AND (@p1 = N'' OR s.name = @p1)
ORDER BY s.name, t.name`

// tableUsageQuery reads how the tables have been reached since the server
// started. It needs VIEW SERVER STATE, which a reporting login is often not
// given, so a failure here leaves the counters unmeasured rather than failing
// the listing.
const tableUsageQuery = `SELECT s.name, t.name,
	SUM(u.user_seeks + u.user_lookups),
	SUM(CASE WHEN u.index_id IN (0,1) THEN u.user_scans ELSE 0 END)
FROM sys.dm_db_index_usage_stats u
JOIN sys.objects t ON t.object_id = u.object_id
JOIN sys.schemas s ON s.schema_id = t.schema_id
WHERE u.database_id = DB_ID() AND t.type IN ('U','V') AND (@p1 = N'' OR s.name = @p1)
GROUP BY s.name, t.name`

// typeExpression writes a column type the way it was declared, which is the
// spelling a person recognises. A length of minus one is what this server calls
// max, and the character types count bytes rather than characters.
const typeExpression = `LOWER(ty.name) + CASE
	WHEN ty.name IN ('nvarchar','nchar')
		THEN CASE WHEN c.max_length = -1 THEN '(max)'
		          ELSE '(' + CAST(c.max_length / 2 AS varchar(11)) + ')' END
	WHEN ty.name IN ('varchar','char','varbinary','binary')
		THEN CASE WHEN c.max_length = -1 THEN '(max)'
		          ELSE '(' + CAST(c.max_length AS varchar(11)) + ')' END
	WHEN ty.name IN ('decimal','numeric')
		THEN '(' + CAST(c.precision AS varchar(11)) + ',' + CAST(c.scale AS varchar(11)) + ')'
	WHEN ty.name IN ('datetime2','time','datetimeoffset')
		THEN '(' + CAST(c.scale AS varchar(11)) + ')'
	ELSE '' END`

const columnsQuery = `SELECT c.name, ` + typeExpression + `,
	c.is_nullable,
	ISNULL(dc.definition, N''),
	CAST(CASE WHEN pk.column_id IS NULL THEN 0 ELSE 1 END AS bit),
	c.column_id,
	ISNULL(CAST(ep.value AS nvarchar(4000)), N'')
FROM sys.columns c
JOIN sys.objects t ON t.object_id = c.object_id
JOIN sys.schemas s ON s.schema_id = t.schema_id
JOIN sys.types ty ON ty.user_type_id = c.user_type_id
LEFT JOIN sys.default_constraints dc ON dc.object_id = c.default_object_id
LEFT JOIN (SELECT ic.object_id, ic.column_id FROM sys.index_columns ic
	JOIN sys.indexes i ON i.object_id = ic.object_id AND i.index_id = ic.index_id
	WHERE i.is_primary_key = 1) pk ON pk.object_id = c.object_id AND pk.column_id = c.column_id
LEFT JOIN sys.extended_properties ep ON ep.major_id = c.object_id AND ep.minor_id = c.column_id
	AND ep.class = 1 AND ep.name = N'MS_Description'
WHERE (@p1 = N'' OR s.name = @p1) AND t.name = @p2
ORDER BY c.column_id`

// relationsQuery reads the foreign keys in both directions at once, one row per
// column, because this server has no way to hand back a list in one value that
// this client could read.
const relationsQuery = `SELECT fk.name,
	ps.name, pt.name, pc.name,
	rs.name, rt.name, rc.name,
	fk.delete_referential_action_desc,
	fkc.constraint_column_id,
	CAST(CASE WHEN rt.name = @p2 AND (@p1 = N'' OR rs.name = @p1) THEN 1 ELSE 0 END AS bit)
FROM sys.foreign_keys fk
JOIN sys.foreign_key_columns fkc ON fkc.constraint_object_id = fk.object_id
JOIN sys.objects pt ON pt.object_id = fk.parent_object_id
JOIN sys.schemas ps ON ps.schema_id = pt.schema_id
JOIN sys.columns pc ON pc.object_id = fkc.parent_object_id AND pc.column_id = fkc.parent_column_id
JOIN sys.objects rt ON rt.object_id = fk.referenced_object_id
JOIN sys.schemas rs ON rs.schema_id = rt.schema_id
JOIN sys.columns rc ON rc.object_id = fkc.referenced_object_id AND rc.column_id = fkc.referenced_column_id
WHERE (pt.name = @p2 AND (@p1 = N'' OR ps.name = @p1))
   OR (rt.name = @p2 AND (@p1 = N'' OR rs.name = @p1))
ORDER BY fk.name, fkc.constraint_column_id`

const indexesQuery = `SELECT s.name, t.name, i.name,
	i.is_unique, i.is_primary_key, i.type_desc,
	ISNULL(i.filter_definition, N''),
	ISNULL(a.index_bytes, 0)
FROM sys.indexes i
JOIN sys.objects t ON t.object_id = i.object_id
JOIN sys.schemas s ON s.schema_id = t.schema_id
OUTER APPLY (SELECT SUM(au.used_pages) * 8192 AS index_bytes FROM sys.partitions pt
	JOIN sys.allocation_units au ON au.container_id = pt.partition_id
	WHERE pt.object_id = i.object_id AND pt.index_id = i.index_id) a
WHERE t.type IN ('U','V') AND i.index_id > 0 AND i.name IS NOT NULL
	AND (@p1 = N'' OR s.name = @p1)
ORDER BY s.name, t.name, i.name`

// indexColumnsQuery reads the columns of every index, which is what a
// definition is written from: this server keeps no text of the statement that
// created one.
const indexColumnsQuery = `SELECT s.name, t.name, i.name, c.name, ic.is_descending_key, ic.is_included_column
FROM sys.index_columns ic
JOIN sys.indexes i ON i.object_id = ic.object_id AND i.index_id = ic.index_id
JOIN sys.objects t ON t.object_id = i.object_id
JOIN sys.schemas s ON s.schema_id = t.schema_id
JOIN sys.columns c ON c.object_id = ic.object_id AND c.column_id = ic.column_id
WHERE t.type IN ('U','V') AND i.index_id > 0 AND i.name IS NOT NULL
	AND (@p1 = N'' OR s.name = @p1)
ORDER BY s.name, t.name, i.name, ic.is_included_column, ic.key_ordinal`

const indexUsageQuery = `SELECT s.name, t.name, i.name,
	u.user_seeks + u.user_scans + u.user_lookups
FROM sys.dm_db_index_usage_stats u
JOIN sys.indexes i ON i.object_id = u.object_id AND i.index_id = u.index_id
JOIN sys.objects t ON t.object_id = i.object_id
JOIN sys.schemas s ON s.schema_id = t.schema_id
WHERE u.database_id = DB_ID() AND i.name IS NOT NULL AND (@p1 = N'' OR s.name = @p1)`

func (c *connection) each(ctx context.Context, query string, scan func(*sql.Rows) error, args ...any) error {
	rows, err := c.db.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		if err := scan(rows); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (c *connection) Info(ctx context.Context) (driver.ServerInfo, error) {
	info := driver.ServerInfo{Driver: Name, ConnectedAt: time.Now()}
	var edition string
	var readOnly bool
	err := c.db.QueryRowContext(ctx, infoQuery).Scan(&info.Version, &edition,
		&info.Database, &info.User, &readOnly, &info.CanWrite, &info.Superuser)
	if err != nil {
		return driver.ServerInfo{}, fmt.Errorf("read the server version: %w", err)
	}
	info.Version = strings.TrimSpace(info.Version + " " + edition)
	info.ReadOnly = readOnly || c.config.ReadOnly()
	return info, nil
}

func (c *connection) Databases(ctx context.Context) ([]driver.Database, error) {
	var databases []driver.Database
	err := c.each(ctx, databasesQuery, func(rows *sql.Rows) error {
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
	err := c.each(ctx, schemasQuery, func(rows *sql.Rows) error {
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

// unmeasured is what a driver reports for a number the server would not tell it.
const unmeasured = -1

func (c *connection) Tables(ctx context.Context, schema string) ([]driver.Table, error) {
	var tables []driver.Table
	err := c.each(ctx, tablesQuery, func(rows *sql.Rows) error {
		table := driver.Table{IndexScans: unmeasured, SeqScans: unmeasured, DeadRows: unmeasured, LiveRows: unmeasured}
		var refreshed sql.NullTime
		if err := rows.Scan(&table.Schema, &table.Name, &table.Kind, &table.Rows,
			&table.Size, &table.Comment, &table.IndexSize, &refreshed); err != nil {
			return err
		}
		if refreshed.Valid {
			table.LastVacuum = refreshed.Time
		}
		tables = append(tables, table)
		return nil
	}, schema)
	if err != nil {
		return nil, fmt.Errorf("list the tables in %s: %w", where(schema), err)
	}
	c.countReads(ctx, schema, tables)
	return tables, nil
}

// countReads fills in how the tables have been reached, and leaves them
// unmeasured when this login may not see the server's counters. A refused
// permission is not an error: the listing is still true, it is only less
// complete.
func (c *connection) countReads(ctx context.Context, schema string, tables []driver.Table) {
	type reads struct{ index, sequential int64 }
	counted := map[string]reads{}
	err := c.each(ctx, tableUsageQuery, func(rows *sql.Rows) error {
		var qualified, table string
		var found reads
		if err := rows.Scan(&qualified, &table, &found.index, &found.sequential); err != nil {
			return err
		}
		counted[qualified+"."+table] = found
		return nil
	}, schema)
	if err != nil {
		return
	}
	for i := range tables {
		found, ok := counted[tables[i].Qualified()]
		if !ok {
			found = reads{}
		}
		tables[i].IndexScans, tables[i].SeqScans, tables[i].Stats = found.index, found.sequential, true
	}
}

func (c *connection) Columns(ctx context.Context, schema, table string) ([]driver.Column, error) {
	var columns []driver.Column
	err := c.each(ctx, columnsQuery, func(rows *sql.Rows) error {
		var column driver.Column
		if err := rows.Scan(&column.Name, &column.Type, &column.Nullable, &column.Default,
			&column.PrimaryKey, &column.Position, &column.Comment); err != nil {
			return err
		}
		columns = append(columns, column)
		return nil
	}, schema, table)
	if err != nil {
		return nil, fmt.Errorf("describe %s: %w", table, err)
	}
	relations, err := c.Relations(ctx, schema, table)
	if err != nil {
		return nil, err
	}
	markForeignKeys(relations, columns)
	return columns, nil
}

func markForeignKeys(relations []driver.Relation, columns []driver.Column) {
	for _, relation := range relations {
		if relation.Inbound {
			continue
		}
		for i, column := range relation.FromColumns {
			target := relation.ToTable
			if i < len(relation.ToColumns) {
				target += "." + relation.ToColumns[i]
			}
			for index := range columns {
				if columns[index].Name == column {
					columns[index].ForeignKey = target
				}
			}
		}
	}
}

func (c *connection) Relations(ctx context.Context, schema, table string) ([]driver.Relation, error) {
	byName := map[string]*driver.Relation{}
	var order []string
	err := c.each(ctx, relationsQuery, func(rows *sql.Rows) error {
		var (
			name, fromSchema, fromTable, fromColumn string
			toSchema, toTable, toColumn, onDelete   string
			position                                int
			inbound                                 bool
		)
		if err := rows.Scan(&name, &fromSchema, &fromTable, &fromColumn,
			&toSchema, &toTable, &toColumn, &onDelete, &position, &inbound); err != nil {
			return err
		}
		relation, ok := byName[name]
		if !ok {
			relation = &driver.Relation{
				Name:       name,
				FromSchema: fromSchema,
				FromTable:  fromTable,
				ToSchema:   toSchema,
				ToTable:    toTable,
				OnDelete:   DeleteAction(onDelete),
				Inbound:    inbound,
			}
			byName[name] = relation
			order = append(order, name)
		}
		relation.FromColumns = append(relation.FromColumns, fromColumn)
		relation.ToColumns = append(relation.ToColumns, toColumn)
		return nil
	}, schema, table)
	if err != nil {
		return nil, fmt.Errorf("read the relations of %s: %w", table, err)
	}
	relations := make([]driver.Relation, 0, len(order))
	for _, name := range order {
		relation := byName[name]
		relation.ConstraintDef = ConstraintDefinition(*relation)
		relations = append(relations, *relation)
	}
	return relations, nil
}

// DeleteAction writes what happens to a referring row when the row it refers to
// is deleted, in the words the other drivers use for the same thing.
func DeleteAction(described string) string {
	switch described {
	case "CASCADE":
		return "CASCADE"
	case "SET_NULL":
		return "SET NULL"
	case "SET_DEFAULT":
		return "SET DEFAULT"
	default:
		return "NO ACTION"
	}
}

// ConstraintDefinition writes a foreign key as the statement that would create
// it, which this server keeps no text of.
func ConstraintDefinition(relation driver.Relation) string {
	definition := "FOREIGN KEY (" + quoteAll(relation.FromColumns) + ") REFERENCES " +
		Quote(relation.ToSchema) + "." + Quote(relation.ToTable) +
		" (" + quoteAll(relation.ToColumns) + ")"
	if action := relation.OnDelete; action != "" && action != "NO ACTION" {
		definition += " ON DELETE " + action
	}
	return definition
}

func quoteAll(names []string) string {
	quoted := make([]string, len(names))
	for i, name := range names {
		quoted[i] = Quote(name)
	}
	return strings.Join(quoted, ", ")
}

// where writes which schemas a listing covered, for an error a person reads.
func where(schema string) string {
	if strings.TrimSpace(schema) == "" {
		return "every schema"
	}
	return schema
}
