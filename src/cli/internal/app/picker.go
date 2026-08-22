package app

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/sonquer/tui4db/src/cli/internal/ui"
)

// row is one line of any list in this program: a connection, a database, a
// table, a command.
type row struct {
	key     string
	label   string
	note    string
	section string
	depth   int
	current bool
}

// picker is the list every screen shares: a cursor that wraps, sections that
// announce themselves once, and one way of drawing a row.
type picker struct {
	theme  *ui.Theme
	rows   []row
	cursor int
	empty  string
}

func newPicker(theme *ui.Theme, empty string) picker {
	return picker{theme: theme, empty: empty}
}

func (p picker) withRows(rows []row) picker {
	p.rows = rows
	if p.cursor >= len(rows) {
		p.cursor = max(0, len(rows)-1)
	}
	return p
}

func (p picker) move(step int) picker {
	if len(p.rows) == 0 {
		return p
	}
	p.cursor = (p.cursor + step + len(p.rows)) % len(p.rows)
	return p
}

func (p picker) selected() (row, bool) {
	if p.cursor < 0 || p.cursor >= len(p.rows) {
		return row{}, false
	}
	return p.rows[p.cursor], true
}

func (p picker) at(key string) picker {
	for i, item := range p.rows {
		if item.key == key {
			p.cursor = i
			return p
		}
	}
	return p
}

func (p picker) view(width int) string {
	if len(p.rows) == 0 {
		return p.theme.Muted.Render("  " + p.empty)
	}
	lines := make([]string, 0, len(p.rows)+4)
	section := ""
	for i, item := range p.rows {
		if item.section != section {
			section = item.section
			if len(lines) > 0 {
				lines = append(lines, "")
			}
			lines = append(lines, p.theme.Section(section, "", width), "")
		}
		lines = append(lines, p.line(item, width, i == p.cursor))
	}
	return strings.Join(lines, "\n")
}

func (p picker) line(item row, width int, active bool) string {
	marker := "  "
	label := p.theme.Value.Render(item.label)
	switch {
	case active:
		marker = p.theme.Accent.Render("▌ ")
		label = p.theme.Accent.Render(item.label)
	case item.current:
		label = p.theme.Accent.Render(item.label)
	}
	if item.current {
		label += p.theme.Subtle.Render(" ·")
	}
	left := marker + strings.Repeat("  ", item.depth) + label
	if item.note == "" {
		return left
	}
	note := ui.Truncate(item.note, max(width-lipgloss.Width(left)-2, 8))
	return ui.SplitLine(left, p.theme.Muted.Render(note), width)
}
