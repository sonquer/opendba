// Package tool is what an agent may do to a database.
package tool

import (
	"context"
	"fmt"
	"strings"

	"github.com/sonquer/tui4db/src/cli/internal/ai"
	"github.com/sonquer/tui4db/src/cli/internal/driver"
	"github.com/sonquer/tui4db/src/cli/internal/export"
	"github.com/sonquer/tui4db/src/cli/pkg/sqlguard"
)

// Database is the part of a connection the tools read. It is narrower than the
// driver's own interface on purpose: what is not here cannot be called.
type Database interface {
	Schemas(ctx context.Context) ([]driver.Schema, error)
	Tables(ctx context.Context, schema string) ([]driver.Table, error)
	Columns(ctx context.Context, schema, table string) ([]driver.Column, error)
	Relations(ctx context.Context, schema, table string) ([]driver.Relation, error)
	Indexes(ctx context.Context, schema string) ([]driver.Index, error)
	Health(ctx context.Context) ([]driver.Finding, error)
	Query(ctx context.Context, sql string) (driver.ResultSet, error)
	Explain(ctx context.Context, sql string, analyze bool) (driver.Plan, error)
}

// Names of the tools, so that nothing refers to one by a string written twice.
const (
	ListSchemas    = "list_schemas"
	ListTables     = "list_tables"
	DescribeTable  = "describe_table"
	ListIndexes    = "list_indexes"
	HealthFindings = "health_findings"
	ExplainQuery   = "explain_query"
	RunSelect      = "run_select"
)

const (
	maxCell    = 120
	maxColumns = 24
)

// Set is the tools an agent may call, bound to one connection.
type Set struct {
	db       Database
	guard    sqlguard.Guard
	mode     sqlguard.Mode
	rowLimit int
	caps     driver.Capabilities
	approve  Approve
}

// Approve is asked before a statement the assistant wrote is run.
type Approve func(ctx context.Context, statement string) error

// WithApproval puts a person between the statement and the database.
func (s *Set) WithApproval(approve Approve) *Set {
	s.approve = approve
	return s
}

// New binds the tools to a connection.
func New(db Database, guard sqlguard.Guard, mode sqlguard.Mode, rowLimit int, caps driver.Capabilities) *Set {
	if rowLimit <= 0 {
		rowLimit = 100
	}
	return &Set{db: db, guard: guard, mode: mode, rowLimit: rowLimit, caps: caps}
}

// Definitions is what the model is told it can call. A tool the driver cannot
// answer is not offered, rather than offered and then refused.
func (s *Set) Definitions() []ai.Tool {
	tools := []ai.Tool{
		{
			Name:        ListSchemas,
			Description: "List the schemas of the database, with how many tables each holds.",
			Parameters:  ai.Schema{Type: "object"},
		},
		{
			Name:        ListTables,
			Description: "List the tables of a schema, with their row counts and sizes.",
			Parameters: ai.Schema{
				Type: "object",
				Properties: map[string]ai.Property{
					"schema": {Type: "string", Description: "the schema to look in; every schema when left out"},
				},
			},
		},
		{
			Name:        DescribeTable,
			Description: "Describe one table: its columns, their types, and the keys that point at it.",
			Parameters: ai.Schema{
				Type: "object",
				Properties: map[string]ai.Property{
					"schema": {Type: "string", Description: "the schema the table is in"},
					"table":  {Type: "string", Description: "the name of the table"},
				},
				Required: []string{"table"},
			},
		},
		{
			Name:        ListIndexes,
			Description: "List the indexes of a schema, with their sizes and how often they are read.",
			Parameters: ai.Schema{
				Type: "object",
				Properties: map[string]ai.Property{
					"schema": {Type: "string", Description: "the schema to look in; every schema when left out"},
				},
			},
		},
		{
			Name: RunSelect,
			Description: "Run a single read-only SQL statement and return its rows. " +
				"Anything that would change data is refused before it is sent.",
			Parameters: ai.Schema{
				Type: "object",
				Properties: map[string]ai.Property{
					"statement": {Type: "string", Description: "one SQL statement, with no trailing semicolon"},
					"limit":     {Type: "integer", Description: "how many rows to return at most"},
				},
				Required: []string{"statement"},
			},
		},
	}
	if s.caps.Health {
		tools = append(tools, ai.Tool{
			Name:        HealthFindings,
			Description: "Read what the server reports about its own health: the same readings the dashboard shows.",
			Parameters:  ai.Schema{Type: "object"},
		})
	}
	if s.caps.Explain {
		tools = append(tools, ai.Tool{
			Name:        ExplainQuery,
			Description: "Ask the server how it would run a statement, without running it.",
			Parameters: ai.Schema{
				Type: "object",
				Properties: map[string]ai.Property{
					"statement": {Type: "string", Description: "one SQL statement to explain"},
				},
				Required: []string{"statement"},
			},
		})
	}
	return tools
}

// Call runs one tool. A failure is not an error to the caller: it is an answer
// the model is given, so that it can put right what it asked for.
func (s *Set) Call(ctx context.Context, call ai.ToolCall) ai.ToolResult {
	content, err := s.run(ctx, call)
	if err != nil {
		return ai.ToolResult{ID: call.ID, Name: call.Name, Content: err.Error(), Failed: true}
	}
	return ai.ToolResult{ID: call.ID, Name: call.Name, Content: content}
}

func (s *Set) run(ctx context.Context, call ai.ToolCall) (string, error) {
	switch call.Name {
	case ListSchemas:
		return s.listSchemas(ctx)
	case ListTables:
		return s.listTables(ctx, text(call.Arguments, "schema"))
	case DescribeTable:
		return s.describeTable(ctx, text(call.Arguments, "schema"), text(call.Arguments, "table"))
	case ListIndexes:
		return s.listIndexes(ctx, text(call.Arguments, "schema"))
	case HealthFindings:
		return s.health(ctx)
	case ExplainQuery:
		return s.explain(ctx, text(call.Arguments, "statement"))
	case RunSelect:
		return s.runSelect(ctx, text(call.Arguments, "statement"), number(call.Arguments, "limit"))
	default:
		return "", fmt.Errorf("there is no tool named %q", call.Name)
	}
}

func (s *Set) listSchemas(ctx context.Context) (string, error) {
	schemas, err := s.db.Schemas(ctx)
	if err != nil {
		return "", fmt.Errorf("read the schemas: %w", err)
	}
	rows := make([][]string, 0, len(schemas))
	for _, schema := range schemas {
		rows = append(rows, []string{schema.Name, fmt.Sprint(schema.Tables), yesNo(schema.System)})
	}
	return table([]string{"schema", "tables", "system"}, rows), nil
}

func (s *Set) listTables(ctx context.Context, schema string) (string, error) {
	tables, err := s.db.Tables(ctx, schema)
	if err != nil {
		return "", fmt.Errorf("read the tables: %w", err)
	}
	rows := make([][]string, 0, len(tables))
	for _, entry := range tables {
		rows = append(rows, []string{
			entry.Schema, entry.Name, entry.Kind,
			measured(entry.Rows), bytes(entry.Size), entry.Comment,
		})
	}
	return table([]string{"schema", "table", "kind", "rows", "size", "comment"}, rows), nil
}

func (s *Set) describeTable(ctx context.Context, schema, name string) (string, error) {
	if strings.TrimSpace(name) == "" {
		return "", fmt.Errorf("argument %q is required", "table")
	}
	columns, err := s.db.Columns(ctx, schema, name)
	if err != nil {
		return "", fmt.Errorf("read the columns: %w", err)
	}
	if len(columns) == 0 {
		return "", fmt.Errorf("no table named %q was found", qualified(schema, name))
	}
	rows := make([][]string, 0, len(columns))
	for _, column := range columns {
		rows = append(rows, []string{
			column.Name, column.Type, yesNo(column.Nullable),
			yesNo(column.PrimaryKey), column.ForeignKey, column.Default, column.Comment,
		})
	}
	built := []string{
		qualified(schema, name),
		table([]string{"column", "type", "null", "key", "references", "default", "comment"}, rows),
	}
	if related := s.relations(ctx, schema, name); related != "" {
		built = append(built, related)
	}
	return strings.Join(built, "\n\n"), nil
}

// relations is best effort: a driver that cannot answer leaves the description
// of the table shorter rather than failing it.
func (s *Set) relations(ctx context.Context, schema, name string) string {
	if !s.caps.Relations {
		return ""
	}
	related, err := s.db.Relations(ctx, schema, name)
	if err != nil || len(related) == 0 {
		return ""
	}
	rows := make([][]string, 0, len(related))
	for _, relation := range related {
		rows = append(rows, []string{
			relation.Name,
			strings.Join(relation.FromColumns, ", "),
			qualified(relation.ToSchema, relation.ToTable),
			strings.Join(relation.ToColumns, ", "),
		})
	}
	return table([]string{"constraint", "columns", "references", "columns"}, rows)
}

func (s *Set) listIndexes(ctx context.Context, schema string) (string, error) {
	indexes, err := s.db.Indexes(ctx, schema)
	if err != nil {
		return "", fmt.Errorf("read the indexes: %w", err)
	}
	rows := make([][]string, 0, len(indexes))
	for _, index := range indexes {
		rows = append(rows, []string{
			index.Schema, index.Table, index.Name,
			bytes(index.Size), measured(index.Scans),
			yesNo(index.Unique), yesNo(index.Primary),
		})
	}
	return table([]string{"schema", "table", "index", "size", "scans", "unique", "primary"}, rows), nil
}

func (s *Set) health(ctx context.Context) (string, error) {
	findings, err := s.db.Health(ctx)
	if err != nil {
		return "", fmt.Errorf("read the health of the server: %w", err)
	}
	rows := make([][]string, 0, len(findings))
	for _, finding := range findings {
		rows = append(rows, []string{
			finding.Group, finding.Subsystem, finding.Code,
			string(finding.Severity), finding.Value, finding.Note,
		})
	}
	return table([]string{"group", "reading", "code", "severity", "value", "note"}, rows), nil
}

func (s *Set) explain(ctx context.Context, statement string) (string, error) {
	if err := s.allowed(statement); err != nil {
		return "", err
	}
	plan, err := s.db.Explain(ctx, statement, false)
	if err != nil {
		return "", fmt.Errorf("explain the statement: %w", err)
	}
	var built strings.Builder
	write(&built, plan.Root, 0)
	if built.Len() == 0 {
		return "the server returned no plan", nil
	}
	return built.String(), nil
}

func write(out *strings.Builder, node driver.PlanNode, depth int) {
	if node.Name == "" && len(node.Children) == 0 {
		return
	}
	fmt.Fprintf(out, "%s%s", strings.Repeat("  ", depth), node.Name)
	if node.Detail != "" {
		fmt.Fprintf(out, " (%s)", node.Detail)
	}
	if node.Rows > 0 {
		fmt.Fprintf(out, " rows=%d", node.Rows)
	}
	out.WriteString("\n")
	for _, child := range node.Children {
		write(out, child, depth+1)
	}
}

func (s *Set) runSelect(ctx context.Context, statement string, limit int) (string, error) {
	if err := s.allowed(statement); err != nil {
		return "", err
	}
	if s.approve != nil {
		if err := s.approve(ctx, statement); err != nil {
			return "", err
		}
	}
	if limit <= 0 || limit > s.rowLimit {
		limit = s.rowLimit
	}
	result, err := s.db.Query(ctx, statement)
	if err != nil {
		return "", fmt.Errorf("run the statement: %w", err)
	}
	defer func() { _ = result.Close() }()

	columns := result.Columns()
	if len(columns) > maxColumns {
		columns = columns[:maxColumns]
	}
	rows := [][]string{}
	for result.Next() && len(rows) < limit {
		rows = append(rows, cells(result.Values(), len(columns)))
	}
	if err := result.Err(); err != nil {
		return "", fmt.Errorf("read the rows: %w", err)
	}
	built := table(columns, rows)
	if result.Truncated() || len(rows) == limit {
		built += "\n\nthere may be more rows than these"
	}
	return built, nil
}

// allowed is the whole safety story of this package.
func (s *Set) allowed(statement string) error {
	if strings.TrimSpace(statement) == "" {
		return fmt.Errorf("argument %q is required", "statement")
	}
	result := s.guard.Classify(statement, s.mode)
	if result.Allowed() {
		return nil
	}
	return fmt.Errorf("refused: %s", result.Reason)
}

func text(arguments map[string]any, name string) string {
	value, ok := arguments[name].(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(value)
}

func number(arguments map[string]any, name string) int {
	switch value := arguments[name].(type) {
	case float64:
		return int(value)
	case int:
		return value
	default:
		return 0
	}
}

func qualified(schema, name string) string {
	if schema == "" {
		return name
	}
	return schema + "." + name
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

// measured renders a count a driver could not take as unknown rather than as
// zero, because zero means counted and empty.
func measured(value int64) string {
	if value < 0 {
		return "n/a"
	}
	return fmt.Sprint(value)
}

func bytes(value int64) string {
	if value < 0 {
		return "n/a"
	}
	units := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	size := float64(value)
	for _, unit := range units {
		if size < 1024 || unit == "TiB" {
			return fmt.Sprintf("%.0f %s", size, unit)
		}
		size /= 1024
	}
	return fmt.Sprint(value)
}

func cells(values []any, width int) []string {
	row := make([]string, 0, width)
	for i, value := range values {
		if i >= width {
			break
		}
		row = append(row, cell(value))
	}
	for len(row) < width {
		row = append(row, "")
	}
	return row
}

func cell(value any) string {
	if value == nil {
		return "null"
	}
	written := strings.ReplaceAll(export.Text(value), "\n", " ")
	if len(written) > maxCell {
		return written[:maxCell] + "…"
	}
	return written
}

// table renders a result the way a model reads best: a header, a rule, and one
// row per line.
func table(header []string, rows [][]string) string {
	if len(rows) == 0 {
		return "nothing to show"
	}
	built := make([]string, 0, len(rows)+2)
	built = append(built, strings.Join(header, " | "))
	built = append(built, strings.Repeat("-", 8))
	for _, row := range rows {
		built = append(built, strings.Join(row, " | "))
	}
	return strings.Join(built, "\n")
}
