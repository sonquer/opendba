package app

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/tree"

	"github.com/sonquer/tui4db/src/cli/internal/driver"
	"github.com/sonquer/tui4db/src/cli/internal/ui"
)

const (
	sidebarWidth    = 26
	minSidebarWidth = 18
)

// explorer is the schema of the database you are writing against, drawn beside
// the editor so the statement does not have to be written from memory.
type explorer struct {
	theme  *ui.Theme
	rows   []row
	cursor int
	open   map[string]bool
	hidden bool
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
				rows = append(rows, row{key: "schema:" + schema, label: schema})
			}
		}
		rows = append(rows, row{
			key:   "table:" + table.Qualified(),
			label: table.Name,
			note:  ui.Count(table.Rows),
			depth: 1,
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
	e.rows = rows
	if e.cursor >= len(rows) {
		e.cursor = max(0, len(rows)-1)
	}
	return e
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

// view draws the schema with lipgloss/tree, which owns the connectors, while
// the cursor stays ours.
func (e explorer) view(width, height int, focused bool) string {
	head := e.theme.Section("tables", "", width)
	if len(e.rows) == 0 {
		return head + "\n\n" + e.theme.Muted.Render("nothing here")
	}
	lines := []string{head, ""}
	var branch *tree.Tree
	var table *tree.Tree
	flush := func() {
		if branch != nil {
			lines = append(lines, branch.String(), "")
		}
		branch, table = nil, nil
	}
	for i, item := range e.rows {
		label := e.label(item, width, i == e.cursor, focused)
		switch item.depth {
		case 0:
			flush()
			lines = append(lines, e.theme.Label.Render(label))
			branch = e.branch()
		case 1:
			if branch == nil {
				branch = e.branch()
			}
			table = e.branch().Root(label)
			branch.Child(table)
		default:
			if table != nil {
				table.Child(label)
			}
		}
	}
	flush()
	return head + "\n\n" + e.window(strings.Join(lines[2:], "\n"), height-2)
}

// window keeps the row under the cursor on screen when the schema is longer
// than the pane.
func (e explorer) window(drawn string, height int) string {
	rows := strings.Split(strings.TrimRight(drawn, "\n"), "\n")
	if len(rows) <= height {
		return ui.Fit(drawn, height)
	}
	at := e.line(rows)
	offset := clamp(at-height/2, 0, len(rows)-height)
	view := strings.Join(rows[offset:offset+height], "\n")
	if offset+height < len(rows) {
		view = strings.Join(strings.Split(view, "\n")[:height-1], "\n") + "\n" +
			e.theme.Subtle.Render("  ↓ "+ui.Plural(len(rows)-offset-height+1, "more", "more"))
	}
	return view
}

// line finds the drawn row the cursor sits on, which is not its index in the
// list once the tree has added its connectors and headings.
func (e explorer) line(rows []string) int {
	for i, drawn := range rows {
		if strings.Contains(drawn, "▌") {
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
	mark := ""
	if name, ok := strings.CutPrefix(item.key, "table:"); ok {
		mark = "▸ "
		if e.open[name] {
			mark = "▾ "
		}
	}
	text := ui.Truncate(mark+item.label, width-8)
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
	case key.Matches(msg, m.keys.Choose):
		return m.insertTable()
	}
	return m, nil
}

// insertTable writes what the cursor points at into the statement, which is the
// whole reason the schema is on screen.
func (m Model) insertTable() (tea.Model, tea.Cmd) {
	item, ok := m.sidebar.selected()
	if !ok || item.depth == 0 {
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
	height := ui.BodyHeight(m.height)
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
