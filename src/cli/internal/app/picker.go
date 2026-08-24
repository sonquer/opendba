package app

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/sonquer/tui4db/src/cli/internal/ui"
)

// row is one line of any list in this program: a connection, a database, a
// table, a command.
// row is one line of any list. cap is the key the row answers to, drawn on a
// keycap at the far right so it is never confused with the prose beside it.
type row struct {
	key     string
	label   string
	note    string
	cap     string
	mark    string
	section string
	depth   int
	current bool
	on      bool
}

// picker is the list every screen shares: a cursor that wraps, sections that
// announce themselves once, and one way of drawing a row.
//
// caps is how wide the column of keys is, measured once from the rows it was
// given. Without it a row ending in a key and a row ending in a sentence end in
// the same place, and the keys stop being a column anybody can read down.
type picker struct {
	theme  *ui.Theme
	rows   []row
	hints  map[string]string
	cursor int
	caps   int
	empty  string
}

func newPicker(theme *ui.Theme, empty string) picker {
	return picker{theme: theme, empty: empty}
}

func (p picker) withRows(rows []row) picker {
	p.rows = rows
	p.caps = 0
	for _, item := range rows {
		if held := lipgloss.Width(p.theme.KeycapStyle.Render(item.cap)); item.cap != "" && held > p.caps {
			p.caps = held
		}
	}
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
			lines = append(lines, p.theme.Section(section, p.hints[section], width), "")
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
	case item.current, item.on:
		label = p.theme.Accent.Render(item.label)
	}
	if item.current {
		label += p.theme.Subtle.Render(" ·")
	}
	left := marker + strings.Repeat("  ", item.depth) + p.box(item, active) + label
	right := p.aside(item, max(width-lipgloss.Width(left)-2, 8))
	if right == "" {
		return left
	}
	return ui.SplitLine(left, right, width)
}

// aside is what sits at the right of a row: what the row is about, and then the
// key it answers to on a cap of its own. They are drawn apart on purpose. A
// column where one row holds a keystroke and the next holds a sentence is a
// column the eye cannot run down, and a key printed as plain text beside prose
// does not read as something to press.
func (p picker) aside(item row, room int) string {
	column := p.caps
	cap4Key := strings.Repeat(" ", column)
	if item.cap != "" {
		drawn := p.theme.KeycapStyle.Render(item.cap)
		cap4Key = strings.Repeat(" ", max(column-lipgloss.Width(drawn), 0)) + drawn
	}
	if column > 0 {
		room -= column + 2
	}
	note := ""
	if item.note != "" && room > 0 {
		note = p.theme.Muted.Render(ui.Truncate(item.note, room))
	}
	switch {
	case column == 0:
		return note
	case note == "":
		return cap4Key
	default:
		return note + "  " + cap4Key
	}
}

// box draws the state of a row in a form, where a row is chosen rather than
// opened. A list that is not a form leaves the mark empty and loses nothing.
func (p picker) box(item row, active bool) string {
	if item.mark == "" {
		return ""
	}
	if item.on || active {
		return p.theme.Accent.Render(item.mark) + " "
	}
	return p.theme.Subtle.Render(item.mark) + " "
}
