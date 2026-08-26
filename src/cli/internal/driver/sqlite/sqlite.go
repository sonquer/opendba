package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/sonquer/opendba/src/cli/internal/driver"
)

const Name = "sqlite"

type Driver struct{}

func New() Driver { return Driver{} }

func (Driver) Name() string { return Name }

func (Driver) Title() string { return "SQLite" }

func (Driver) Capabilities() driver.Capabilities {
	return driver.Capabilities{
		Explain:         true,
		Relations:       true,
		Health:          true,
		ReadOnlySession: true,
		Cancel:          true,
		FileBased:       true,
	}
}

func (d Driver) Connect(ctx context.Context, config driver.Config) (driver.Conn, error) {
	dsn, err := DSN(config)
	if err != nil {
		return nil, err
	}
	database, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open the database file: %w", err)
	}
	database.SetMaxOpenConns(1)

	if err := database.PingContext(ctx); err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("open %s: %w", config.Target(), err)
	}
	conn := &connection{db: database, config: config}
	if err := conn.pin(ctx); err != nil {
		_ = database.Close()
		return nil, err
	}
	return conn, nil
}

func DSN(config driver.Config) (string, error) {
	file := strings.TrimSpace(config.File)
	if file == "" {
		return "", fmt.Errorf("a sqlite connection needs a database file")
	}
	query := url.Values{}
	if config.ReadOnly() {
		query.Set("mode", "ro")
		query.Add("_pragma", "query_only(1)")
	}
	query.Add("_pragma", "foreign_keys(1)")
	if timeout := config.Timeouts.Lock; timeout > 0 {
		query.Add("_pragma", fmt.Sprintf("busy_timeout(%d)", timeout.Milliseconds()))
	}
	if strings.HasPrefix(file, "file:") {
		return appendQuery(file, query), nil
	}
	return appendQuery("file:"+file, query), nil
}

func appendQuery(dsn string, query url.Values) string {
	if len(query) == 0 {
		return dsn
	}
	separator := "?"
	if strings.Contains(dsn, "?") {
		separator = "&"
	}
	return dsn + separator + query.Encode()
}

type connection struct {
	db     *sql.DB
	config driver.Config
}

func (c *connection) pin(ctx context.Context) error {
	statements := []string{"PRAGMA foreign_keys = ON"}
	if c.config.ReadOnly() {
		statements = append(statements, "PRAGMA query_only = ON")
	}
	for _, statement := range statements {
		if _, err := c.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("pin the session: %w", err)
		}
	}
	return nil
}

func (c *connection) Close() error {
	if err := c.db.Close(); err != nil {
		return fmt.Errorf("close the database: %w", err)
	}
	return nil
}

func (c *connection) Info(ctx context.Context) (driver.ServerInfo, error) {
	info := driver.ServerInfo{
		Driver:      Name,
		Database:    c.config.Target(),
		ReadOnly:    c.config.ReadOnly(),
		ConnectedAt: time.Now(),
	}
	if err := c.db.QueryRowContext(ctx, "SELECT sqlite_version()").Scan(&info.Version); err != nil {
		return driver.ServerInfo{}, fmt.Errorf("read the server version: %w", err)
	}
	var queryOnly int
	if err := c.db.QueryRowContext(ctx, "PRAGMA query_only").Scan(&queryOnly); err != nil {
		return driver.ServerInfo{}, fmt.Errorf("read the session mode: %w", err)
	}
	info.CanWrite = queryOnly == 0
	return info, nil
}

func (c *connection) each(ctx context.Context, query string, scan func(*sql.Rows) error) error {
	rows, err := c.db.QueryContext(ctx, query)
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

// Databases lists what this connection can reach. SQLite reaches one file,
// plus whatever was attached to it, so the schemas are the databases.
func (c *connection) Databases(ctx context.Context) ([]driver.Database, error) {
	var databases []driver.Database
	err := c.each(ctx, "PRAGMA database_list", func(rows *sql.Rows) error {
		var (
			sequence int
			name     string
			file     sql.NullString
		)
		if err := rows.Scan(&sequence, &name, &file); err != nil {
			return err
		}
		if name == "temp" {
			return nil
		}
		databases = append(databases, driver.Database{
			Name:    name,
			Current: name == "main",
			Comment: file.String,
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list the databases: %w", err)
	}
	return databases, nil
}

// Sessions is empty for a file: the only session is the one asking.
func (c *connection) Sessions(context.Context) ([]driver.Session, error) { return nil, nil }

// Stop has nothing to stop, since there is no other session to signal.
func (c *connection) Stop(context.Context, string, bool) error {
	return errors.New("sqlite has no sessions to stop")
}

func (c *connection) Schemas(ctx context.Context) ([]driver.Schema, error) {
	var schemas []driver.Schema
	err := c.each(ctx, "PRAGMA database_list", func(rows *sql.Rows) error {
		var (
			sequence int
			name     string
			file     sql.NullString
		)
		if err := rows.Scan(&sequence, &name, &file); err != nil {
			return err
		}
		schemas = append(schemas, driver.Schema{Name: name, System: name == "temp"})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list the schemas: %w", err)
	}
	for i, schema := range schemas {
		count, err := c.countTables(ctx, schema.Name)
		if err != nil {
			return nil, err
		}
		schemas[i].Tables = count
	}
	return schemas, nil
}

func (c *connection) countTables(ctx context.Context, schema string) (int, error) {
	query := fmt.Sprintf("SELECT count(*) FROM %s.sqlite_schema WHERE type IN ('table','view') AND name NOT LIKE 'sqlite_%%'", quote(schema))
	var count int
	if err := c.db.QueryRowContext(ctx, query).Scan(&count); err != nil {
		return 0, fmt.Errorf("count the tables in %s: %w", schema, err)
	}
	return count, nil
}

func (c *connection) Tables(ctx context.Context, schema string) ([]driver.Table, error) {
	schema = orMain(schema)
	query := fmt.Sprintf(`SELECT name, type FROM %s.sqlite_schema
WHERE type IN ('table','view') AND name NOT LIKE 'sqlite_%%'
ORDER BY name`, quote(schema))
	var tables []driver.Table
	err := c.each(ctx, query, func(rows *sql.Rows) error {
		var table driver.Table
		if err := rows.Scan(&table.Name, &table.Kind); err != nil {
			return err
		}
		table.Schema = schema
		tables = append(tables, table)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list the tables in %s: %w", schema, err)
	}
	return tables, nil
}

func (c *connection) Columns(ctx context.Context, schema, table string) ([]driver.Column, error) {
	schema = orMain(schema)
	var columns []driver.Column
	err := c.each(ctx, fmt.Sprintf("PRAGMA %s.table_info(%s)", quote(schema), quote(table)), func(rows *sql.Rows) error {
		var (
			position     int
			name         string
			columnType   string
			notNull      int
			defaultValue sql.NullString
			primaryKey   int
		)
		if err := rows.Scan(&position, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return err
		}
		columns = append(columns, driver.Column{
			Name:       name,
			Type:       strings.ToLower(columnType),
			Nullable:   notNull == 0,
			Default:    defaultValue.String,
			PrimaryKey: primaryKey > 0,
			Position:   position + 1,
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("describe %s: %w", table, err)
	}
	if err := c.markForeignKeys(ctx, schema, table, columns); err != nil {
		return nil, err
	}
	return columns, nil
}

func (c *connection) markForeignKeys(ctx context.Context, schema, table string, columns []driver.Column) error {
	relations, err := c.outbound(ctx, schema, table)
	if err != nil {
		return err
	}
	for _, relation := range relations {
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
	return nil
}

func (c *connection) Relations(ctx context.Context, schema, table string) ([]driver.Relation, error) {
	schema = orMain(schema)
	relations, err := c.outbound(ctx, schema, table)
	if err != nil {
		return nil, err
	}
	tables, err := c.Tables(ctx, schema)
	if err != nil {
		return nil, err
	}
	for _, candidate := range tables {
		if strings.EqualFold(candidate.Name, table) {
			continue
		}
		references, err := c.outbound(ctx, schema, candidate.Name)
		if err != nil {
			return nil, err
		}
		for _, reference := range references {
			if !strings.EqualFold(reference.ToTable, table) {
				continue
			}
			reference.Inbound = true
			relations = append(relations, reference)
		}
	}
	return relations, nil
}

func (c *connection) outbound(ctx context.Context, schema, table string) ([]driver.Relation, error) {
	byID := map[int]*driver.Relation{}
	var order []int
	err := c.each(ctx, fmt.Sprintf("PRAGMA %s.foreign_key_list(%s)", quote(schema), quote(table)), func(rows *sql.Rows) error {
		var (
			id       int
			sequence int
			target   string
			from     string
			to       sql.NullString
			onUpdate string
			onDelete string
			match    string
		)
		if err := rows.Scan(&id, &sequence, &target, &from, &to, &onUpdate, &onDelete, &match); err != nil {
			return err
		}
		relation, ok := byID[id]
		if !ok {
			relation = &driver.Relation{
				Name:       fmt.Sprintf("%s_fk_%d", table, id),
				FromSchema: schema,
				FromTable:  table,
				ToSchema:   schema,
				ToTable:    target,
				OnDelete:   onDelete,
			}
			byID[id] = relation
			order = append(order, id)
		}
		relation.FromColumns = append(relation.FromColumns, from)
		if to.Valid {
			relation.ToColumns = append(relation.ToColumns, to.String)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("read the foreign keys of %s: %w", table, err)
	}
	relations := make([]driver.Relation, 0, len(order))
	for _, id := range order {
		relations = append(relations, *byID[id])
	}
	return relations, nil
}

func orMain(schema string) string {
	if strings.TrimSpace(schema) == "" {
		return "main"
	}
	return schema
}

func quote(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}
