package app

import (
	"context"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/sonquer/tui4db/src/cli/internal/driver"
	"github.com/sonquer/tui4db/src/cli/internal/ui"
)

const (
	completionRows  = 6
	completionWidth = 34
	editorGutter    = 6
	headerRows      = 5
)

var keywords = []string{
	"SELECT", "FROM", "WHERE", "GROUP BY", "HAVING", "ORDER BY", "LIMIT", "OFFSET",
	"JOIN", "LEFT JOIN", "INNER JOIN", "ON", "AS", "AND", "OR", "NOT", "NULL", "IS",
	"IN", "LIKE", "BETWEEN", "DISTINCT", "COUNT", "SUM", "AVG", "MIN", "MAX",
	"WITH", "UNION", "EXPLAIN", "CASE", "WHEN", "THEN", "ELSE", "END", "DESC", "ASC",
}

type columnsMsg struct {
	table   string
	columns []driver.Column
}

type suggestion struct {
	text string
	kind string
}

type completion struct {
	theme  *ui.Theme
	items  []suggestion
	prefix string
	cursor int
}

func (c completion) active() bool { return len(c.items) > 0 }

func (c completion) move(step int) completion {
	if len(c.items) == 0 {
		return c
	}
	c.cursor = (c.cursor + step + len(c.items)) % len(c.items)
	return c
}

func (c completion) selected() (suggestion, bool) {
	if c.cursor < 0 || c.cursor >= len(c.items) {
		return suggestion{}, false
	}
	return c.items[c.cursor], true
}

func (c completion) view() string {
	lines := make([]string, 0, len(c.items))
	for i, item := range c.items {
		marker := "  "
		text := c.theme.Value.Render(item.text)
		if i == c.cursor {
			marker = c.theme.Accent.Render("▌ ")
			text = c.theme.Accent.Render(item.text)
		}
		lines = append(lines, ui.SplitLine(
			marker+text, c.theme.Subtle.Render(item.kind), completionWidth))
	}
	return c.theme.Panel.Render(strings.Join(lines, "\n"))
}

// resuggest reads the word the cursor sits on and offers what could finish it.
func (m Model) resuggest() (Model, tea.Cmd) {
	m.suggest.theme = m.theme
	prefix := m.wordBeforeCursor()
	if prefix == "" {
		m.suggest.items = nil
		return m, nil
	}
	items := m.candidates(prefix)
	if len(items) == 1 && strings.EqualFold(items[0].text, prefix) {
		items = nil
	}
	m.suggest.items = items
	m.suggest.prefix = prefix
	if m.suggest.cursor >= len(items) {
		m.suggest.cursor = 0
	}
	return m, m.readColumns()
}

func (m Model) wordBeforeCursor() string {
	lines := strings.Split(m.editor.Value(), "\n")
	row := m.editor.Line()
	if row < 0 || row >= len(lines) {
		return ""
	}
	info := m.editor.LineInfo()
	line := []rune(lines[row])
	column := min(info.StartColumn+info.ColumnOffset, len(line))
	start := column
	for start > 0 && wordRune(line[start-1]) {
		start--
	}
	return string(line[start:column])
}

func wordRune(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		return true
	default:
		return r == '_' || r == '.'
	}
}

// candidates reads the shape of what is being typed. A dot is the whole
// grammar here: before one is a schema or a table, after one is what it holds.
func (m Model) candidates(prefix string) []suggestion {
	qualifier, stem, dotted := strings.Cut(prefix, ".")
	if !dotted {
		return best(m.everything(prefix), prefix, completionRows)
	}
	if inner, deeper, again := strings.Cut(stem, "."); again {
		return best(m.columnsOf(inner, qualifier+"."+inner+".", deeper), prefix, completionRows)
	}
	if found := m.tablesIn(qualifier, stem); len(found) > 0 {
		return best(found, prefix, completionRows)
	}
	return best(m.columnsOf(qualifier, qualifier+".", stem), prefix, completionRows)
}

func (m Model) everything(prefix string) []suggestion {
	found := make([]suggestion, 0, completionRows)
	for _, schema := range m.schemas() {
		if matches(schema, prefix) {
			found = append(found, suggestion{text: schema, kind: "schema"})
		}
	}
	for _, table := range m.tables {
		if matches(table.Name, prefix) {
			found = append(found, suggestion{text: table.Name, kind: table.Kind})
		}
	}
	for _, table := range m.mentioned() {
		found = append(found, m.columnsOf(table, "", prefix)...)
	}
	for _, keyword := range keywords {
		if matches(keyword, prefix) {
			found = append(found, suggestion{text: keyword, kind: "keyword"})
		}
	}
	return found
}

// tablesIn answers a schema followed by a dot, which is most of the typing on a
// database with more than one schema.
func (m Model) tablesIn(schema, stem string) []suggestion {
	found := make([]suggestion, 0, completionRows)
	for _, table := range m.tables {
		if !strings.EqualFold(table.Schema, schema) {
			continue
		}
		if stem == "" || matches(table.Name, stem) {
			found = append(found, suggestion{text: schema + "." + table.Name, kind: table.Kind})
		}
	}
	return found
}

func (m Model) schemas() []string {
	seen := map[string]bool{}
	var names []string
	for _, table := range m.tables {
		if table.Schema == "" || seen[table.Schema] {
			continue
		}
		seen[table.Schema] = true
		names = append(names, table.Schema)
	}
	return names
}

func (m Model) columnsOf(table, lead, stem string) []suggestion {
	found := make([]suggestion, 0, completionRows)
	for _, column := range m.fields[table] {
		if matches(column.Name, stem) {
			found = append(found, suggestion{text: lead + column.Name, kind: column.Type})
		}
	}
	return found
}

// mentioned returns the tables this statement already names, which are the ones
// whose columns are worth offering and worth reading from the server.
func (m Model) mentioned() []string {
	statement := strings.ToLower(m.statement())
	var named []string
	for _, table := range m.tables {
		if strings.Contains(statement, strings.ToLower(table.Name)) {
			named = append(named, table.Name)
		}
	}
	return named
}

func (m Model) readColumns() tea.Cmd {
	var cmds []tea.Cmd
	for _, table := range m.mentioned() {
		if cmd := m.readOneColumn(table); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

// readOneColumn reads the columns of a table once and remembers them, because
// both the completions and the schema beside the editor want the same answer.
func (m Model) readOneColumn(table string) tea.Cmd {
	name := table
	if cut := strings.LastIndex(name, "."); cut >= 0 {
		name = name[cut+1:]
	}
	if _, known := m.fields[name]; known {
		return nil
	}
	conn := m.session.Conn
	schema := m.session.Connection.Schema()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), catalogTimeout)
		defer cancel()

		columns, err := conn.Columns(ctx, schema, name)
		if err != nil {
			return columnsMsg{table: name}
		}
		return columnsMsg{table: name, columns: columns}
	}
}

func matches(candidate, prefix string) bool {
	return prefix != "" && strings.HasPrefix(strings.ToLower(candidate), strings.ToLower(prefix))
}

// best keeps the suggestions closest to what was typed. Closest means fewest
// characters added to it: on a catalogue where a dozen tables start with
// `product`, `products` is the one being typed and the longest name is the one
// least likely to be, so a list cut in the order the server listed them throws
// away the answer and keeps the noise.
func best(items []suggestion, prefix string, count int) []suggestion {
	sort.SliceStable(items, func(i, j int) bool {
		return extra(items[i].text, prefix) < extra(items[j].text, prefix)
	})
	if len(items) > count {
		return items[:count]
	}
	return items
}

func extra(candidate, prefix string) int {
	return len([]rune(candidate)) - len([]rune(prefix))
}

// accept replaces the word under the cursor with the chosen suggestion.
func (m Model) accept() (tea.Model, tea.Cmd) {
	chosen, ok := m.suggest.selected()
	if !ok {
		return m, nil
	}
	for range []rune(m.suggest.prefix) {
		updated, _ := m.editor.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
		m.editor = updated
	}
	m.editor.InsertString(chosen.text)
	m.suggest.items = nil
	return m, nil
}

// suggestionAt puts the list under the word being typed, never over it.
func (m Model) suggestionAt() (int, int) {
	info := m.editor.LineInfo()
	x := ui.Gutter + editorGutter + info.ColumnOffset - len([]rune(m.suggest.prefix))
	if !m.sidebar.hidden && !m.zoomed {
		x += m.sidebar.width(ui.FrameWidth(m.width)) + 2
	}
	y := headerRows + m.editor.Line() + info.RowOffset + 1
	return x, y
}

func (m Model) withSuggestions(screen string) string {
	dialog := m.suggest.view()
	x, y := m.suggestionAt()
	x = clamp(x, 0, m.width-lipgloss.Width(dialog)-ui.Gutter)
	y = clamp(y, 0, m.height-lipgloss.Height(dialog)-3)
	return ui.At(screen, dialog, x, y)
}

func clamp(value, low, high int) int {
	switch {
	case high < low:
		return low
	case value < low:
		return low
	case value > high:
		return high
	default:
		return value
	}
}
