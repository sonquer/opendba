package app

import (
	"context"
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
	headerRows      = 4
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
	if len(prefix) < 1 || strings.HasSuffix(prefix, ".") && strings.Count(prefix, ".") > 1 {
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

func (m Model) candidates(prefix string) []suggestion {
	if qualifier, stem, ok := strings.Cut(prefix, "."); ok {
		return limit(m.columnsOf(qualifier, qualifier+".", stem), completionRows)
	}
	found := make([]suggestion, 0, completionRows)
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
	return limit(found, completionRows)
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
	conn := m.session.Conn
	schema := m.session.Connection.Schema()
	var cmds []tea.Cmd
	for _, table := range m.mentioned() {
		if _, known := m.fields[table]; known {
			continue
		}
		name := table
		cmds = append(cmds, func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), catalogTimeout)
			defer cancel()

			columns, err := conn.Columns(ctx, schema, name)
			if err != nil {
				return columnsMsg{table: name}
			}
			return columnsMsg{table: name, columns: columns}
		})
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

func matches(candidate, prefix string) bool {
	return prefix != "" && strings.HasPrefix(strings.ToLower(candidate), strings.ToLower(prefix))
}

func limit(items []suggestion, count int) []suggestion {
	if len(items) > count {
		return items[:count]
	}
	return items
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

func (m Model) suggestionAt() (int, int) {
	info := m.editor.LineInfo()
	column := info.ColumnOffset - len([]rune(m.suggest.prefix))
	x := ui.Gutter + editorGutter + column
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
