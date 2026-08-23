package app

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/sonquer/tui4db/src/cli/internal/driver"
	"github.com/sonquer/tui4db/src/cli/internal/ui"
)

func workbench(t *testing.T) Model {
	t.Helper()
	conn := healthy()
	conn.tables = []driver.Table{
		{Schema: "app", Name: "users", Kind: "table", Rows: 2},
		{Schema: "app", Name: "orders", Kind: "table", Rows: 9},
		{Schema: "billing", Name: "invoices", Kind: "table"},
	}
	conn.fields = map[string][]driver.Column{
		"users": {{Name: "id", Type: "bigint"}, {Name: "email", Type: "text"}},
	}
	m := loadedWith(t, conn, workspaceWith(t))
	m.width, m.height = 110, 32
	editing, _ := press(t, m, "e")
	return editing
}

func TestTheSchemaSitsBesideTheEditor(t *testing.T) {
	view := plain(workbench(t).content())
	for _, want := range []string{"TABLES", "app", "users", "orders", "billing", "invoices"} {
		if !strings.Contains(view, want) {
			t.Errorf("the sidebar must show %q:\n%s", want, view)
		}
	}
	if !strings.Contains(view, "│") {
		t.Error("the panes must be separated")
	}
}

func TestTheSidebarTakesTheCursorPastTheSchema(t *testing.T) {
	m := workbench(t)
	side, _ := press(t, m, "tab")
	if side.focus != focusSidebar {
		t.Fatalf("focus = %v", side.focus)
	}
	item, ok := side.sidebar.selected()
	if !ok || item.depth == 0 {
		t.Fatalf("the cursor must land on something worth opening: %+v", item)
	}
	down, _ := press(t, side, "down")
	if next, _ := down.sidebar.selected(); next.label == item.label {
		t.Error("down must move")
	}
	wrapped := down
	for range len(down.sidebar.rows) {
		wrapped, _ = press(t, wrapped, "down")
	}
	if wrapped.sidebar.cursor != down.sidebar.cursor {
		t.Errorf("a full turn must come back to the same row: %d", wrapped.sidebar.cursor)
	}
	up, _ := press(t, down, "up")
	if up.sidebar.cursor != side.sidebar.cursor {
		t.Errorf("up must go back: %d", up.sidebar.cursor)
	}
}

// Enter on a table in the tree opens its rows in a tab of their own, with the
// statement that read them written into it rather than sent behind it.
func TestATableOpensInATabOfItsOwn(t *testing.T) {
	m := workbench(t)
	side, _ := press(t, m, "tab")
	opened, cmd := press(t, side, "enter")
	if len(opened.sheets) != 2 || opened.sheet != 1 {
		t.Fatalf("enter on a table must open a tab: %d tabs, on %d", len(opened.sheets), opened.sheet)
	}
	if opened.kind != sheetTable || opened.title != "users" {
		t.Errorf("the tab must know what it is: %v %q", opened.kind, opened.title)
	}
	if opened.statement() != `SELECT * FROM "app"."users"` {
		t.Errorf("statement = %q, it must be the one the driver quotes", opened.statement())
	}
	shown := settle(t, opened, cmd)
	view := plain(shown.content())
	for _, want := range []string{"query 1", "users", `SELECT * FROM "app"."users"`} {
		if !strings.Contains(view, want) {
			t.Errorf("the screen must show %q:\n%s", want, view)
		}
	}
}

func TestATableGoesIntoTheStatement(t *testing.T) {
	m := workbench(t)
	side, _ := press(t, m, "tab")
	inserted, _ := press(t, side, "i")
	if inserted.focus != focusEditor || !inserted.editor.Focused() {
		t.Fatal("inserting must give the editor its focus back")
	}
	if inserted.statement() != "app.users" {
		t.Errorf("statement = %q", inserted.statement())
	}
	if inserted.focus != focusEditor {
		t.Error("the cursor belongs in the editor after an insert")
	}
}

func TestASchemaHeadingInsertsNothing(t *testing.T) {
	m := workbench(t)
	side, _ := press(t, m, "tab")
	side.sidebar.cursor = 0
	same, cmd := press(t, side, "i")
	if cmd != nil || same.statement() != "" {
		t.Errorf("a schema is not a table: %q", same.statement())
	}
}

func TestATableOpensItsColumns(t *testing.T) {
	m := workbench(t)
	side, _ := press(t, m, "tab")
	opened, cmd := press(t, side, "space")
	if cmd == nil {
		t.Fatal("opening a table must read its columns")
	}
	answered, _ := opened.Update(runFirst(t, cmd))
	shown := answered.(Model)
	if !strings.Contains(plain(shown.content()), "email") {
		t.Errorf("the columns must be listed:\n%s", plain(shown.content()))
	}
	closed, _ := press(t, shown, "space")
	if strings.Contains(plain(closed.content()), "email") {
		t.Error("space must close it again")
	}
}

func TestTheSidebarCanBePutAway(t *testing.T) {
	m := workbench(t)
	hidden, _ := press(t, m, "ctrl+b")
	if !hidden.sidebar.hidden {
		t.Fatal("ctrl+b must hide the schema")
	}
	if strings.Contains(plain(hidden.content()), "TABLES") {
		t.Error("a hidden sidebar is not drawn")
	}
	if hidden.paneWidth() <= m.paneWidth() {
		t.Error("hiding it must give the editor the room")
	}
	shown, _ := press(t, hidden, "ctrl+b")
	if shown.sidebar.hidden {
		t.Error("ctrl+b must bring it back")
	}

	onSide, _ := press(t, m, "tab")
	away, _ := press(t, onSide, "ctrl+b")
	if away.focus != focusEditor || !away.editor.Focused() {
		t.Error("hiding the pane the cursor is in must move the cursor")
	}
}

func TestZoomGivesTheResultTheWindow(t *testing.T) {
	m := typeInto(t, workbench(t), "SELECT 1")
	ran, cmd := press(t, m, "ctrl+r")
	shown := settle(t, ran, cmd)
	onResults, _ := press(t, shown, "tab")
	if onResults.focus != focusResults {
		t.Fatalf("focus = %v", onResults.focus)
	}
	zoomed, _ := press(t, onResults, "z")
	if !zoomed.zoomed {
		t.Fatal("z must zoom")
	}
	view := plain(zoomed.content())
	if strings.Contains(view, "TABLES") {
		t.Error("a zoomed result has the window to itself")
	}
	if !strings.Contains(view, "SELECT 1") {
		t.Errorf("the statement that ran must be shown:\n%s", view)
	}
	if !strings.Contains(plain(zoomed.footer(0)), "zoom") {
		t.Errorf("footer = %s", plain(zoomed.footer(0)))
	}
	back, _ := press(t, zoomed, "z")
	if back.zoomed {
		t.Error("z must give the editor back")
	}
	again, _ := press(t, zoomed, "esc")
	if again.zoomed || again.view != viewQuery {
		t.Error("esc must leave the zoom before it leaves the editor")
	}
}

func TestAnEmptySchemaSaysSo(t *testing.T) {
	conn := healthy()
	conn.tables = nil
	m := loadedWith(t, conn, workspaceWith(t))
	editing, _ := press(t, m, "e")
	if !strings.Contains(plain(editing.content()), "nothing here") {
		t.Errorf("content = %s", plain(editing.content()))
	}
	side, _ := press(t, editing, "tab")
	if side.focus == focusSidebar {
		t.Error("an empty sidebar is not worth focusing")
	}
}

func TestTheSidebarScrollsWithTheCursor(t *testing.T) {
	conn := healthy()
	conn.tables = nil
	for i := range 40 {
		conn.tables = append(conn.tables, driver.Table{
			Schema: "app",
			Name:   fmt.Sprintf("table_%02d", i),
			Kind:   "table",
		})
	}
	m := loadedWith(t, conn, workspaceWith(t))
	m.width, m.height = 110, 24
	editing, _ := press(t, m, "e")
	side, _ := press(t, editing, "tab")

	first := plain(side.content())
	if !strings.Contains(first, "table_00") {
		t.Fatalf("the list starts at the top:\n%s", first)
	}
	if !strings.Contains(first, "more") {
		t.Errorf("a list taller than the pane must say so:\n%s", first)
	}

	far := side
	for range 30 {
		far, _ = press(t, far, "down")
	}
	shown := plain(far.content())
	if !strings.Contains(shown, "table_30") {
		t.Errorf("the cursor must stay on screen:\n%s", shown)
	}
	if strings.Contains(shown, "table_00") {
		t.Error("the top must scroll away")
	}
	for _, line := range strings.Split(shown, "\n") {
		if lipgloss.Width(line) > 110 {
			t.Fatalf("the pane must not grow: %q", line)
		}
	}
}

func TestASidebarThatFitsDoesNotScroll(t *testing.T) {
	m := workbench(t)
	m.height = 40
	side, _ := press(t, m, "tab")
	if strings.Contains(plain(side.content()), "more") {
		t.Error("a list that fits says nothing about scrolling")
	}
}

func TestTheSidebarWithoutASchemaName(t *testing.T) {
	conn := healthy()
	conn.tables = []driver.Table{{Name: "users", Kind: "table"}}
	m := loadedWith(t, conn, workspaceWith(t))
	editing, _ := press(t, m, "e")
	if !strings.Contains(plain(editing.content()), "users") {
		t.Errorf("a table without a schema is still a table:\n%s", plain(editing.content()))
	}
	side, _ := press(t, editing, "tab")
	inserted, _ := press(t, side, "i")
	if inserted.statement() != "users" {
		t.Errorf("statement = %q", inserted.statement())
	}
}

func TestTheSidebarWithNothingUnderTheCursor(t *testing.T) {
	empty := newExplorer(ui.Default())
	if moved := empty.move(1); moved.cursor != 0 {
		t.Error("an empty tree does not move")
	}
	if _, ok := empty.selected(); ok {
		t.Error("an empty tree has nothing selected")
	}
	if _, ok := empty.table(); ok {
		t.Error("an empty tree points at no table")
	}
	if same := empty.toggle(); len(same.open) != 0 {
		t.Error("there is nothing to open")
	}
	if same := empty.onTable(); same.cursor != 0 {
		t.Error("there is no table to land on")
	}

	schemas := newExplorer(ui.Default()).withTables(
		[]driver.Table{{Schema: "app", Name: "users"}}, nil)
	schemas.cursor = 0
	if _, ok := schemas.table(); ok {
		t.Error("a schema heading is not a table")
	}
	if same := schemas.toggle(); len(same.open) != 0 {
		t.Error("a schema heading opens nothing")
	}
}

func TestAColumnPointsAtItsTable(t *testing.T) {
	side := newExplorer(ui.Default())
	side.open = map[string]bool{"app.users": true}
	side = side.withTables([]driver.Table{{Schema: "app", Name: "users"}},
		map[string][]driver.Column{"users": {{Name: "email", Type: "text"}}})
	side.cursor = len(side.rows) - 1
	name, ok := side.table()
	if !ok || name != "app.users" {
		t.Errorf("a column belongs to its table, got %q", name)
	}
	if closed := side.toggle(); closed.open["app.users"] {
		t.Error("toggling from a column closes its table")
	}
}

func TestTheSidebarKeepsAFloorAndACeiling(t *testing.T) {
	side := newExplorer(ui.Default())
	if got := side.width(400); got != sidebarWidth {
		t.Errorf("width = %d", got)
	}
	if got := side.width(40); got != minSidebarWidth {
		t.Errorf("width = %d", got)
	}
	if got := side.width(88); got != 22 {
		t.Errorf("width = %d", got)
	}
	side.hidden = true
	if got := side.width(400); got != 0 {
		t.Errorf("a hidden sidebar takes no room, got %d", got)
	}
}

// A qualified name comes apart the same way whether or not it has a schema on
// the front of it.
func TestAQualifiedNameComesApart(t *testing.T) {
	for _, want := range []struct {
		name          string
		qualified     string
		schema, table string
	}{
		{"with a schema", "app.users", "app", "users"},
		{"without one", "users", "", "users"},
		{"more than one dot", "a.b.c", "a.b", "c"},
	} {
		t.Run(want.name, func(t *testing.T) {
			schema, table := split(want.qualified)
			if schema != want.schema || table != want.table {
				t.Errorf("split(%q) = %q, %q; want %q, %q",
					want.qualified, schema, table, want.schema, want.table)
			}
		})
	}
}

// The wheel stops at the ends of the schema rather than wrapping, and a schema
// with nothing in it does not move at all.
func TestTheSchemaStopsAtItsEnds(t *testing.T) {
	m := workbench(t)
	m.sidebar = m.sidebar.withTables(m.tables, m.fields)
	if empty := newExplorer(ui.Default()).roll(3); empty.cursor != 0 {
		t.Error("a schema with nothing in it has nowhere to go")
	}
	far := m.sidebar
	for range 20 {
		far = far.roll(3)
	}
	if far.cursor != len(m.sidebar.rows)-1 {
		t.Errorf("cursor = %d, it must stop at the last row", far.cursor)
	}
	back := far
	for range 20 {
		back = back.roll(-3)
	}
	if back.cursor != 0 {
		t.Errorf("cursor = %d, and at the first", back.cursor)
	}
}

// The schema is only scrolled when there is more of it than there is room for.
func TestTheSchemaScrollsOnlyWhenItHasTo(t *testing.T) {
	m := workbench(t)
	m.sidebar = m.sidebar.withTables(m.tables, m.fields)
	lines := m.sidebar.paint(24, false)
	if at := m.sidebar.offset(lines, len(lines)+10); at != 0 {
		t.Errorf("offset = %d, a schema that fits is not scrolled", at)
	}
	if at := m.sidebar.offset(lines, 0); at != 0 {
		t.Errorf("offset = %d, a pane with no room shows the top", at)
	}
	far := m.sidebar
	far.cursor = len(far.rows) - 1
	if at := far.offset(lines, 2); at == 0 {
		t.Error("a cursor at the end must have scrolled the schema")
	}
}
