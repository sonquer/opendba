package app

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/tree"

	"github.com/sonquer/tui4db/src/cli/internal/driver"
	"github.com/sonquer/tui4db/src/cli/internal/sqlfiles"
	"github.com/sonquer/tui4db/src/cli/internal/ui"
)

const (
	sidebarWidth    = 26
	minSidebarWidth = 18
)

const (
	sectionTables = "tables"
	sectionFiles  = "files"
)

// explorer is the schema of the database you are writing against, drawn beside
// the editor so the statement does not have to be written from memory.
type explorer struct {
	theme  *ui.Theme
	rows   []row
	cursor int
	open   map[string]bool
	hidden bool

	// tabled and filed are the two blocks the pane is made of, kept apart so a
	// catalogue read again does not blank the files and a directory read again does
	// not blank the catalogue.
	tabled []row
	filed  []row

	// trouble is what went wrong reading the directory, said under the heading
	// of the block it is about rather than over the whole pane.
	trouble string
}

func newExplorer(theme *ui.Theme) explorer {
	return explorer{theme: theme, open: map[string]bool{}}
}

func (e explorer) withTables(tables []driver.Table, fields map[string][]driver.Column) explorer {
	rows := make([]row, 0, len(tables)+len(e.open))
	schema := ""
	for _, table := range tables {
		if table.Schema != schema {
			schema = table.Schema
			if schema != "" {
				rows = append(rows, row{key: "schema:" + schema, label: schema, section: sectionTables})
			}
		}
		rows = append(rows, row{
			key:     "table:" + table.Qualified(),
			label:   table.Name,
			note:    ui.Count(table.Rows),
			section: sectionTables,
			depth:   1,
		})
		if !e.open[table.Qualified()] {
			continue
		}
		for _, column := range fields[table.Name] {
			rows = append(rows, row{
				key:   "column:" + table.Qualified() + "." + column.Name,
				label: column.Name,
				note:  column.Type,
				depth: 2,
			})
		}
	}
	e.tabled = rows
	return e.rebuilt()
}

// withFiles is the statements kept beside this connection, or what went wrong
// looking for them.
func (e explorer) withFiles(files []sqlfiles.File, trouble string) explorer {
	rows := make([]row, 0, len(files))
	for _, file := range files {
		rows = append(rows, row{
			key:     "file:" + file.Name,
			label:   file.Name,
			section: sectionFiles,
			depth:   1,
		})
	}
	e.filed = rows
	e.trouble = trouble
	return e.rebuilt()
}

// rebuilt puts the two blocks end to end and keeps the cursor on the list.
func (e explorer) rebuilt() explorer {
	rows := make([]row, 0, len(e.tabled)+len(e.filed))
	rows = append(rows, e.tabled...)
	rows = append(rows, e.filed...)
	e.rows = rows
	if e.cursor >= len(rows) {
		e.cursor = max(0, len(rows)-1)
	}
	return e
}

// file returns the name of the statement the cursor sits on, if it is on one.
func (e explorer) file() (string, bool) {
	item, ok := e.selected()
	if !ok {
		return "", false
	}
	return strings.CutPrefix(item.key, "file:")
}

func (e explorer) move(step int) explorer {
	if len(e.rows) == 0 {
		return e
	}
	e.cursor = (e.cursor + step + len(e.rows)) % len(e.rows)
	return e
}

// onTable puts the cursor on something worth pressing enter on, which a schema
// heading is not.
func (e explorer) onTable() explorer {
	for i := range e.rows {
		at := (e.cursor + i) % len(e.rows)
		if e.rows[at].depth > 0 {
			e.cursor = at
			return e
		}
	}
	return e
}

func (e explorer) selected() (row, bool) {
	if e.cursor < 0 || e.cursor >= len(e.rows) {
		return row{}, false
	}
	return e.rows[e.cursor], true
}

// table returns the qualified name of whatever the cursor sits on, so a column
// hands back the table it belongs to.
func (e explorer) table() (string, bool) {
	item, ok := e.selected()
	if !ok {
		return "", false
	}
	name, found := strings.CutPrefix(item.key, "table:")
	if found {
		return name, true
	}
	name, found = strings.CutPrefix(item.key, "column:")
	if !found {
		return "", false
	}
	return name[:strings.LastIndex(name, ".")], true
}

func (e explorer) toggle() explorer {
	name, ok := e.table()
	if !ok {
		return e
	}
	next := map[string]bool{}
	for key, value := range e.open {
		next[key] = value
	}
	next[name] = !next[name]
	e.open = next
	return e
}

func (e explorer) width(available int) int {
	if e.hidden {
		return 0
	}
	room := available / 4
	switch {
	case room > sidebarWidth:
		return sidebarWidth
	case room < minSidebarWidth:
		return minSidebarWidth
	default:
		return room
	}
}

// painted is one drawn line of the schema and the row it belongs to.
type painted struct {
	text string
	row  int
}

// view draws the schema with lipgloss/tree, which owns the connectors, while
// the cursor stays ours.
func (e explorer) view(width, height int, focused bool) string {
	head := e.theme.Section(sectionTables, "", width)
	return head + "\n\n" + e.window(e.paint(width, focused), height-2)
}

// paint draws every line of the schema and says which row each one came from.
func (e explorer) paint(width int, focused bool) []painted {
	lines := make([]painted, 0, len(e.rows))
	var branch, table *tree.Tree
	var members []int
	flush := func() {
		if branch == nil {
			return
		}
		lines = append(lines, zip(branch.String(), members)...)
		lines = append(lines, painted{row: -1})
		branch, table, members = nil, nil, nil
	}
	for i, item := range e.tabled {
		label := e.label(item, width, i == e.cursor, focused)
		switch item.depth {
		case 0:
			flush()
			lines = append(lines, painted{text: e.theme.Label.Render(label), row: i})
			branch = e.branch()
		case 1:
			if branch == nil {
				branch = e.branch()
			}
			table = e.branch().Root(label)
			branch.Child(table)
			members = append(members, i)
		default:
			if table == nil {
				continue
			}
			table.Child(label)
			members = append(members, i)
		}
	}
	flush()
	if len(e.tabled) == 0 {
		lines = append(lines, painted{text: e.theme.Muted.Render("nothing here"), row: -1})
	}
	return append(lines, e.painted4Files(width, focused)...)
}

// painted4Files draws the statements kept beside this connection, under a
// heading of its own so an empty workspace still says where they would go.
func (e explorer) painted4Files(width int, focused bool) []painted {
	lines := []painted{
		{row: -1},
		{text: e.theme.Section(sectionFiles, "", width), row: -1},
		{row: -1},
	}
	switch {
	case e.trouble != "":
		return append(lines, painted{text: e.theme.Muted.Render(" " + e.trouble), row: -1})
	case len(e.filed) == 0:
		return append(lines, painted{text: e.theme.Muted.Render(" nothing yet"), row: -1})
	}
	branch := e.branch()
	members := make([]int, 0, len(e.filed))
	for i := range e.filed {
		at := len(e.tabled) + i
		branch.Child(e.label(e.rows[at], width, at == e.cursor, focused))
		members = append(members, at)
	}
	return append(lines, zip(branch.String(), members)...)
}

// zip pairs the lines a tree drew with the rows they were built from.
func zip(drawn string, rows []int) []painted {
	texts := strings.Split(strings.TrimRight(drawn, "\n"), "\n")
	lines := make([]painted, 0, len(texts))
	for i, text := range texts {
		row := -1
		if len(texts) == len(rows) {
			row = rows[i]
		}
		lines = append(lines, painted{text: text, row: row})
	}
	return lines
}

// window keeps the row under the cursor on screen when the schema is longer
// than the pane.
func (e explorer) window(lines []painted, height int) string {
	if height < 1 {
		height = 1
	}
	if len(lines) <= height {
		return ui.Fit(text4Lines(lines), height)
	}
	offset := clamp(e.lineOf(lines)-height/2, 0, len(lines)-height)
	shown := lines[offset : offset+height]
	if offset+height < len(lines) {
		shown = append([]painted(nil), shown[:height-1]...)
		shown = append(shown, painted{row: -1, text: e.theme.Subtle.Render(
			"  ↓ " + ui.Plural(len(lines)-offset-height+1, "more", "more"))})
	}
	return text4Lines(shown)
}

// offset is the first line of the schema on screen, which is what a click has
// to be counted from.
func (e explorer) offset(lines []painted, height int) int {
	if height < 1 {
		height = 1
	}
	if len(lines) <= height {
		return 0
	}
	return clamp(e.lineOf(lines)-height/2, 0, len(lines)-height)
}

// rowAt says which row of the schema a line of the pane is showing.
func (e explorer) rowAt(width, height, line int, focused bool) (int, bool) {
	lines := e.paint(width, focused)
	room := height - 2
	at := e.offset(lines, room) + line - treeTop
	if line < treeTop || at < 0 || at >= len(lines) {
		return 0, false
	}
	if lines[at].row < 0 {
		return 0, false
	}
	return lines[at].row, true
}

// treeTop is what the pane spends above the schema: the word TABLES, and the
// blank line under it.
const treeTop = 2

func text4Lines(lines []painted) string {
	texts := make([]string, 0, len(lines))
	for _, line := range lines {
		texts = append(texts, line.text)
	}
	return strings.Join(texts, "\n")
}

// lineOf finds the drawn line the cursor sits on, which is not its place in the
// list once the tree has added its connectors and headings.
func (e explorer) lineOf(lines []painted) int {
	for i, line := range lines {
		if line.row == e.cursor {
			return i
		}
	}
	return 0
}

func (e explorer) branch() *tree.Tree {
	return tree.New().
		Enumerator(tree.DefaultEnumerator).
		EnumeratorStyle(e.theme.Divider).
		IndenterStyle(e.theme.Divider)
}

func (e explorer) label(item row, width int, active, focused bool) string {
	text := ui.Truncate(item.label, width-8)
	switch {
	case active && focused:
		return e.theme.Accent.Render("▌" + text)
	case item.depth == 2:
		return e.theme.Muted.Render(" " + text)
	default:
		return e.theme.Value.Render(" " + text)
	}
}

func (m Model) explorerKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Up):
		m.sidebar = m.sidebar.move(-1)
		return m, nil
	case key.Matches(msg, m.keys.Down):
		m.sidebar = m.sidebar.move(1)
		return m, nil
	case key.Matches(msg, m.keys.Expand):
		m.sidebar = m.sidebar.toggle()
		return m.refreshSidebar()
	case key.Matches(msg, m.keys.Insert):
		return m.insertTable()
	case key.Matches(msg, m.keys.Choose):
		return m.openedInSidebar()
	case key.Matches(msg, m.keys.Reload):
		return m, m.readFiles()
	case key.Matches(msg, m.keys.Forget):
		return m.confirmDeleteFile()
	}
	return m, nil
}

// openedInSidebar is what enter on the row under the cursor means, which
// depends on which of the two blocks it is in.
func (m Model) openedInSidebar() (tea.Model, tea.Cmd) {
	if name, ok := m.sidebar.file(); ok {
		return m, m.readFile(name)
	}
	return m.openTable()
}

// openTable opens the rows of whatever the tree has the cursor on, in a tab of
// its own.
func (m Model) openTable() (tea.Model, tea.Cmd) {
	name, ok := m.sidebar.table()
	if !ok {
		return m, nil
	}
	schema, bare := split(name)
	sheet := newWorksheet(m.theme, sheetTable, bare)
	sheet.editor.SetValue(m.session.Conn.Preview(schema, bare))
	opened := m.openSheet(sheet)
	opened.focus = focusEditor
	return opened.attempt()
}

// split takes a qualified name apart.
func split(qualified string) (schema, table string) {
	cut := strings.LastIndex(qualified, ".")
	if cut < 0 {
		return "", qualified
	}
	return qualified[:cut], qualified[cut+1:]
}

func describe(column driver.Column) string {
	parts := []string{column.Type}
	if column.PrimaryKey {
		parts = append(parts, "primary key")
	}
	if !column.Nullable {
		parts = append(parts, "not null")
	}
	if column.Default != "" {
		parts = append(parts, "default "+column.Default)
	}
	return strings.Join(parts, " · ")
}

// indexesOf writes what reads this table quickly, in the words of someone who
// has to decide whether an index is worth its cost.
func (m Model) indexesOf(table string) string {
	lines := []string{"## Indexes", ""}
	found := 0
	for _, index := range m.indexes {
		if index.Table != table {
			continue
		}
		found++
		note := driver.ByteSize(index.Size)
		if index.Scans == 0 {
			note += ", never used on this node"
		}
		lines = append(lines, "- **"+index.Name+"** "+note)
	}
	if found == 0 {
		lines = append(lines, "Nothing indexes this table, so every read walks all of it.")
	}
	return strings.Join(lines, "\n")
}

// insertTable writes what the cursor points at into the statement, which is the
// whole reason the schema is on screen.
func (m Model) insertTable() (tea.Model, tea.Cmd) {
	item, ok := m.sidebar.selected()
	if !ok || item.depth == 0 || item.section == sectionFiles {
		return m, nil
	}
	name := item.label
	if qualified, found := strings.CutPrefix(item.key, "table:"); found {
		name = qualified
	}
	m.editor.InsertString(name)
	m.focus = focusEditor
	return m, m.editor.Focus()
}

func (m Model) refreshSidebar() (tea.Model, tea.Cmd) {
	m.sidebar = m.sidebar.withTables(m.tables, m.fields)
	name, ok := m.sidebar.table()
	if !ok || !m.sidebar.open[name] {
		return m, nil
	}
	return m, m.readOneColumn(name)
}

func (m Model) workbench() string {
	width := ui.FrameWidth(m.width)
	height := m.workbenchHeight()
	if m.zoomed {
		return m.zoomBody()
	}
	right := m.editorPane(width-m.sidebar.width(width)-3, height)
	if m.sidebar.hidden {
		return right
	}
	side := m.sidebar.width(width)
	return lipgloss.JoinHorizontal(lipgloss.Top,
		lipgloss.NewStyle().Width(side).Height(height).MaxHeight(height).
			Render(m.sidebar.view(side, height, m.focus == focusSidebar)),
		lipgloss.NewStyle().Foreground(m.theme.P.Border).
			Render(strings.Repeat("│\n", max(height-1, 1))+"│"),
		lipgloss.NewStyle().Width(width-side-1).Height(height).MaxHeight(height).
			PaddingLeft(1).Render(right),
	)
}

// roll moves the cursor by a notch of the wheel.
func (e explorer) roll(step int) explorer {
	if len(e.rows) == 0 {
		return e
	}
	e.cursor = clamp(e.cursor+step, 0, len(e.rows)-1)
	return e
}
