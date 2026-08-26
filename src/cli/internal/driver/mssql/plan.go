package mssql

import (
	"context"
	"database/sql"
	sqldriver "database/sql/driver"
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/sonquer/opendba/src/cli/internal/driver"
)

// The statements that make this server answer with its plan instead of, or as
// well as, the rows. Both are settings of one connection and both have to be
// the only statement in their batch, which is why a plan is taken on a
// connection of its own.
const (
	showPlanOn    = "SET SHOWPLAN_XML ON"
	showPlanOff   = "SET SHOWPLAN_XML OFF"
	statisticsOn  = "SET STATISTICS XML ON"
	statisticsOff = "SET STATISTICS XML OFF"
)

// Explain asks the server what it would do with a statement. Asked for an
// estimate the statement is never run; asked to time it the statement runs, and
// the plan arrives after the rows.
func (c *connection) Explain(ctx context.Context, statement string, analyze bool) (driver.Plan, error) {
	document, err := c.showPlan(ctx, statement, analyze)
	if err != nil {
		return driver.Plan{}, err
	}
	plan, err := ParsePlan(document)
	if err != nil {
		return driver.Plan{}, err
	}
	return plan, nil
}

func (c *connection) showPlan(ctx context.Context, statement string, analyze bool) (string, error) {
	conn, err := c.db.Conn(ctx)
	if err != nil {
		return "", fmt.Errorf("explain the query: %w", err)
	}
	on, off := showPlanOn, showPlanOff
	if analyze {
		on, off = statisticsOn, statisticsOff
	}
	document, err := readPlan(ctx, conn, on, statement, analyze)
	if err != nil {
		discard(conn)
		return "", err
	}
	if _, err := conn.ExecContext(ctx, off); err != nil {
		discard(conn)
		return "", fmt.Errorf("stop explaining: %w", err)
	}
	if err := conn.Close(); err != nil {
		return "", fmt.Errorf("explain the query: %w", err)
	}
	return document, nil
}

// discard throws a connection away rather than returning it to the pool. A
// connection that was left explaining would answer every later statement with a
// plan instead of its rows, so one that cannot be put back the way it was found
// must not be put back at all.
func discard(conn *sql.Conn) {
	_ = conn.Raw(func(any) error { return sqldriver.ErrBadConn })
	_ = conn.Close()
}

func readPlan(ctx context.Context, conn *sql.Conn, on, statement string, analyze bool) (string, error) {
	if _, err := conn.ExecContext(ctx, on); err != nil {
		return "", fmt.Errorf("ask for the query plan: %w", err)
	}
	rows, err := conn.QueryContext(ctx, statement)
	if err != nil {
		return "", fmt.Errorf("explain the query: %w", err)
	}
	defer func() { _ = rows.Close() }()
	document, err := planDocument(rows, analyze)
	if err != nil {
		return "", err
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("read the query plan: %w", err)
	}
	return document, nil
}

// planDocument finds the plan among what the statement returned. An estimate is
// the only thing that comes back; a timed run returns the rows first and the
// plan in a result of its own after them.
func planDocument(rows *sql.Rows, analyze bool) (string, error) {
	for {
		if document, found := scanPlan(rows); found {
			return document, nil
		}
		if !analyze || !rows.NextResultSet() {
			return "", fmt.Errorf("the server returned no plan for this statement")
		}
	}
}

func scanPlan(rows *sql.Rows) (string, bool) {
	columns, err := rows.Columns()
	if err != nil || len(columns) != 1 {
		return "", false
	}
	if !rows.Next() {
		return "", false
	}
	var document sql.NullString
	if err := rows.Scan(&document); err != nil {
		return "", false
	}
	if !strings.Contains(document.String, "<ShowPlanXML") {
		return "", false
	}
	return document.String, true
}

// node is a plan operator while the document is being read. Children are held
// by pointer because a plan is built as it is walked, and a slice that grows
// moves what it holds.
type node struct {
	name     string
	logical  string
	object   string
	cost     float64
	rows     int64
	actual   int64
	timed    bool
	duration time.Duration
	children []*node
}

// ParsePlan reads a showplan document. The operators of a plan are not children
// of one another in the document: each is wrapped in an element named after
// what it physically does, and the operators it feeds from are inside that. So
// the document is walked rather than decoded into a shape.
func ParsePlan(document string) (driver.Plan, error) {
	decoder := xml.NewDecoder(strings.NewReader(document))
	var root *node
	var stack []*node
	total := 0.0
	for {
		token, err := decoder.Token()
		if err != nil {
			break
		}
		element, ok := token.(xml.StartElement)
		if ok {
			root, stack, total = enter(element, root, stack, total)
			continue
		}
		if end, closing := token.(xml.EndElement); closing && end.Name.Local == "RelOp" && len(stack) > 0 {
			stack = stack[:len(stack)-1]
		}
	}
	if root == nil {
		return driver.Plan{}, fmt.Errorf("read the query plan: it named no operation")
	}
	if total == 0 {
		total = root.cost
	}
	tree := convert(root, 0)
	return driver.Plan{Root: tree, Text: PlanText(tree), Total: total}, nil
}

func enter(element xml.StartElement, root *node, stack []*node, total float64) (*node, []*node, float64) {
	switch element.Name.Local {
	case "RelOp":
		operator := operator(element)
		if len(stack) > 0 {
			parent := stack[len(stack)-1]
			parent.children = append(parent.children, operator)
		} else if root == nil {
			root = operator
		}
		return root, append(stack, operator), total
	case "StmtSimple":
		if cost, ok := number(element, "StatementSubTreeCost"); ok {
			total = cost
		}
	case "Object":
		if len(stack) > 0 {
			reads(stack[len(stack)-1], element)
		}
	case "RunTimeCountersPerThread":
		if len(stack) > 0 {
			measure(stack[len(stack)-1], element)
		}
	}
	return root, stack, total
}

func operator(element xml.StartElement) *node {
	built := &node{name: attribute(element, "PhysicalOp"), logical: attribute(element, "LogicalOp")}
	if cost, ok := number(element, "EstimatedTotalSubtreeCost"); ok {
		built.cost = cost
	}
	if rows, ok := number(element, "EstimateRows"); ok {
		built.rows = int64(rows)
	}
	if built.name == "" {
		built.name = "operation"
	}
	return built
}

// reads records what an operator reads, which is the part of a plan a person
// looks for first.
func reads(built *node, element xml.StartElement) {
	if built.object != "" {
		return
	}
	table := bare(attribute(element, "Table"))
	schema := bare(attribute(element, "Schema"))
	index := bare(attribute(element, "Index"))
	named := strings.TrimPrefix(schema+"."+table, ".")
	if index != "" && named != "" {
		built.object = named + " using " + index
		return
	}
	built.object = named + index
}

// measure records what really happened, which a timed plan reports once per
// thread that did any of it.
func measure(built *node, element xml.StartElement) {
	built.timed = true
	if rows, ok := number(element, "ActualRows"); ok {
		built.actual += int64(rows)
	}
	if elapsed, ok := number(element, "ActualElapsedms"); ok {
		if spent := time.Duration(elapsed) * time.Millisecond; spent > built.duration {
			built.duration = spent
		}
	}
}

// bare removes the brackets this server writes every name of its own inside.
func bare(name string) string {
	return strings.TrimSuffix(strings.TrimPrefix(name, "["), "]")
}

func attribute(element xml.StartElement, name string) string {
	for _, found := range element.Attr {
		if found.Name.Local == name {
			return found.Value
		}
	}
	return ""
}

func number(element xml.StartElement, name string) (float64, bool) {
	value, err := strconv.ParseFloat(attribute(element, name), 64)
	if err != nil {
		return 0, false
	}
	return value, true
}

func convert(built *node, depth int) driver.PlanNode {
	converted := driver.PlanNode{
		Name:     built.name,
		Detail:   detail(built),
		Cost:     built.cost,
		Rows:     built.rows,
		Duration: built.duration,
		Depth:    depth,
	}
	if built.timed {
		converted.Rows = built.actual
	}
	for _, child := range built.children {
		converted.Children = append(converted.Children, convert(child, depth+1))
	}
	return converted
}

func detail(built *node) string {
	var parts []string
	if built.logical != "" && built.logical != built.name {
		parts = append(parts, built.logical)
	}
	if built.object != "" {
		parts = append(parts, "on "+built.object)
	}
	return strings.Join(parts, " ")
}

// PlanText writes the plan as the lines a person reads, one operation to a line
// and indented by what feeds what.
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
