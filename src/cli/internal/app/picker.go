package app

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/sonquer/opendba/src/cli/internal/ui"
)

// row is one line of any list in this program: a connection, a database, a
// table, a command.
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

	// badge is the one word about a row that is drawn rather than said: it is
	// already styled, so it is neither muted nor clipped with the remark.
	badge string
}

// picker is the list every screen shares: a cursor that wraps, sections that
// announce themselves once, and one way of drawing a row.
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
		lines = append(lines, p.line(item, width, i == p.cursor, p.column()))
	}
	return strings.Join(lines, "\n")
}

// column is how wide the labels are drawn, so that what follows them stands in
// one place down the list rather than wherever each label happened to end. A
// list with nothing to stand after its labels has no column to keep.
func (p picker) column() int {
	widest := 0
	for _, item := range p.rows {
		if item.badge == "" {
			continue
		}
		widest = max(widest, lipgloss.Width(item.label)+2*item.depth)
	}
	return widest
}

func (p picker) line(item row, width int, active bool, column int) string {
	marker := "  "
	style := p.theme.Value
	switch {
	case active:
		marker = p.theme.Accent.Render("▌ ")
		style = p.theme.Accent
	case item.current, item.on:
		style = p.theme.Accent
	}
	label := style.Render(item.label)
	if column > 0 {
		label = style.Render(item.label + strings.Repeat(" ", max(column-lipgloss.Width(item.label)-2*item.depth, 0)))
	}
	left := marker + strings.Repeat("  ", item.depth) + p.box(item, active) + label
	if item.badge != "" {
		left += "  " + item.badge
	}
	right := p.aside(item, max(width-lipgloss.Width(left)-2, 8))
	if right == "" {
		return left
	}
	return ui.SplitLine(left, right, width)
}

// aside is what sits at the right of a row: what the row is about, then the word
// for the row that is the one in use, then the key it answers to on a cap of its
// own.
func (p picker) aside(item row, room int) string {
	held := ""
	if item.current {
		held = p.theme.Subtle.Render(here4Row)
		room -= lipgloss.Width(held) + 2
	}
	cap4Key := ""
	if p.caps > 0 {
		cap4Key = p.cap4Key(item)
		room -= p.caps + 2
	}
	note := ""
	if item.note != "" && room > 0 {
		note = p.theme.Muted.Render(ui.Truncate(item.note, room))
	}
	return strings.TrimRight(strings.TrimLeft(
		strings.Join(kept4Row(note, held, cap4Key), "  "), " "), " ")
}

// kept4Row drops the pieces a row has nothing to put in, so two spaces never
// stand in for something that was not there.
func kept4Row(parts ...string) []string {
	kept := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			kept = append(kept, part)
		}
	}
	return kept
}

// here4Row is the word on the row somebody is already on.
const here4Row = "in use"

// cap4Key is the key a row answers to, right-aligned in the column every row
// shares so the keys read down rather than sitting wherever the prose left off.
func (p picker) cap4Key(item row) string {
	if item.cap == "" {
		return strings.Repeat(" ", p.caps)
	}
	drawn := p.theme.KeycapStyle.Render(item.cap)
	return strings.Repeat(" ", max(p.caps-lipgloss.Width(drawn), 0)) + drawn
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
