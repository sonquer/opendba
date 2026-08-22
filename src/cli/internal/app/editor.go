package app

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/table"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/sonquer/tui4db/src/cli/internal/ui"
)

const (
	minEditorRows  = 3
	minResultsRows = 3
	minColumnWide  = 6
)

func newEditor(theme *ui.Theme) textarea.Model {
	editor := textarea.New()
	editor.Placeholder = "SELECT ..."
	editor.ShowLineNumbers = true
	editor.SetHeight(minEditorRows)
	editor.SetWidth(ui.MaxTextWidth)
	styles := textarea.DefaultStyles(true)
	styles.Focused.Base = lipgloss.NewStyle()
	styles.Blurred.Base = lipgloss.NewStyle()
	styles.Focused.CursorLine = lipgloss.NewStyle()
	styles.Focused.Placeholder = lipgloss.NewStyle().Foreground(theme.P.Subtle)
	styles.Focused.LineNumber = lipgloss.NewStyle().Foreground(theme.P.Subtle)
	styles.Blurred.LineNumber = lipgloss.NewStyle().Foreground(theme.P.Subtle)
	editor.SetStyles(styles)
	return editor
}

type results struct {
	table     table.Model
	theme     *ui.Theme
	column    int
	width     int
	height    int
	statement string
	columns   []string
	rows      [][]string
	duration  time.Duration
	truncated bool
	failure   string
	present   bool
}

func newResults(theme *ui.Theme, msg queriedMsg, width, height int) results {
	result := results{
		theme:     theme,
		width:     width,
		height:    height,
		statement: msg.statement,
		columns:   msg.columns,
		rows:      msg.rows,
		duration:  msg.duration,
		truncated: msg.truncated,
		present:   true,
	}
	if msg.err != nil {
		result.failure = msg.err.Error()
		return result
	}
	return result.rebuild()
}

// rebuild draws the columns that fit from the one the cursor is on, which is
// how a result with thirty columns is read at all.
func (r results) rebuild() results {
	columns, rows := r.window()
	at := r.table.Cursor()
	r.table = table.New(
		table.WithColumns(columns),
		table.WithRows(rowsFor(rows)),
		table.WithHeight(max(r.height, 1)),
		table.WithWidth(tableWidth(columns, r.width)),
		table.WithStyles(tableStyles(r.theme, false)),
		table.WithFocused(true),
	)
	r.table.SetCursor(at)
	return r
}

// window is the slice of columns that fits, starting at the one in front.
func (r results) window() ([]table.Column, [][]string) {
	if len(r.columns) == 0 {
		return nil, nil
	}
	from := min(max(r.column, 0), len(r.columns)-1)
	names := r.columns[from:]
	cells := make([][]string, 0, len(r.rows))
	for _, row := range r.rows {
		if from < len(row) {
			cells = append(cells, row[from:])
			continue
		}
		cells = append(cells, nil)
	}
	columns := columnsFor(names, cells, r.width)
	if fits := len(columns); fits > 0 {
		total := 0
		kept := 0
		for _, column := range columns {
			if total+column.Width+2 > r.width && kept > 0 {
				break
			}
			total += column.Width + 2
			kept++
		}
		columns = columns[:kept]
		for i := range cells {
			if len(cells[i]) > kept {
				cells[i] = cells[i][:kept]
			}
		}
	}
	return columns, cells
}

func (r results) shift(step int) results {
	if !r.present || len(r.columns) == 0 {
		return r
	}
	r.column = min(max(r.column+step, 0), len(r.columns)-1)
	return r.rebuild()
}

// place says which column is in front, for a result too wide to show at once.
func (r results) place() string {
	shown, _ := r.window()
	if len(r.columns) == 0 || len(shown) >= len(r.columns) {
		return ""
	}
	return fmt.Sprintf("column %d of %d", r.column+1, len(r.columns))
}

// record is every column of the row in front, which is the only way to read a
// wide row without walking sideways.
func (r results) record() (details, bool) {
	if !r.present || r.failure != "" || len(r.rows) == 0 {
		return details{}, false
	}
	at := min(max(r.table.Cursor(), 0), len(r.rows)-1)
	row := r.rows[at]
	pairs := make([]pair, 0, len(r.columns))
	for i, name := range r.columns {
		value := "∅"
		if i < len(row) {
			value = row[i]
		}
		pairs = append(pairs, pair{key: name, value: value})
	}
	return details{
		theme: r.theme,
		title: "row " + fmt.Sprintf("%d of %d", at+1, len(r.rows)),
		tag:   ui.Plural(len(r.columns), "column", "columns"),
		pairs: pairs,
		code:  r.statement,
	}, true
}

// tableStyles paints the cursor row only when the pane is being driven, so a
// table nobody is looking at does not claim to have a selection.
func tableStyles(theme *ui.Theme, focused bool) table.Styles {
	styles := table.DefaultStyles()
	styles.Header = styles.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(theme.P.Border).
		BorderBottom(true).
		Foreground(theme.P.Muted).
		Bold(false)
	styles.Cell = lipgloss.NewStyle().Padding(0, 1)
	styles.Selected = lipgloss.NewStyle()
	if focused {
		styles.Selected = theme.Selected2
	}
	return styles
}

// columnsFor gives a result the width it was given: the natural width of the
// data when it fits, shared out when it does not, and the spare room spread
// across the columns so the table fills its pane instead of huddling.
func columnsFor(names []string, rows [][]string, width int) []table.Column {
	widths := naturalWidths(names, rows)
	if len(widths) == 0 {
		return nil
	}
	spare := width - total(widths) - 2*len(widths)
	switch {
	case spare > 0:
		share(widths, spare)
	case spare < 0:
		squeeze(widths, width-2*len(widths))
	}
	columns := make([]table.Column, 0, len(names))
	for i, name := range names {
		columns = append(columns, table.Column{Title: name, Width: widths[i]})
	}
	return columns
}

func naturalWidths(names []string, rows [][]string) []int {
	widths := make([]int, len(names))
	for i, name := range names {
		widths[i] = lipgloss.Width(name)
	}
	for _, row := range rows {
		for i, cell := range row {
			if i < len(widths) && lipgloss.Width(cell) > widths[i] {
				widths[i] = lipgloss.Width(cell)
			}
		}
	}
	for i := range widths {
		if widths[i] < minColumnWide {
			widths[i] = minColumnWide
		}
	}
	return widths
}

func share(widths []int, spare int) {
	each := spare / len(widths)
	for i := range widths {
		widths[i] += each
	}
	for i := 0; i < spare%len(widths); i++ {
		widths[i]++
	}
}

func squeeze(widths []int, room int) {
	if room < minColumnWide*len(widths) {
		for i := range widths {
			widths[i] = minColumnWide
		}
		return
	}
	sum := total(widths)
	left := room
	for i := range widths {
		widths[i] = max(widths[i]*room/sum, minColumnWide)
		left -= widths[i]
	}
	for i := 0; left > 0 && i < len(widths); i++ {
		widths[i]++
		left--
	}
}

func total(widths []int) int {
	sum := 0
	for _, width := range widths {
		sum += width
	}
	return sum
}

func tableWidth(columns []table.Column, available int) int {
	total := 0
	for _, column := range columns {
		total += column.Width + 2
	}
	if available > 0 && total > available {
		return available
	}
	return total
}

func rowsFor(values [][]string) []table.Row {
	rows := make([]table.Row, 0, len(values))
	for _, value := range values {
		rows = append(rows, table.Row(value))
	}
	return rows
}

func (r results) resize(width, height int) results {
	if !r.present || r.failure != "" {
		return r
	}
	r.width, r.height = width, height
	return r.rebuild()
}

func (r results) update(msg tea.Msg) (results, tea.Cmd) {
	if !r.present || r.failure != "" {
		return r, nil
	}
	updated, cmd := r.table.Update(msg)
	r.table = updated
	return r, cmd
}

func (r results) render(theme *ui.Theme, focused bool) string {
	if !r.present {
		return theme.Muted.Render("nothing has run yet")
	}
	if r.failure != "" {
		return theme.Error.Render("✗ " + r.failure)
	}
	if len(r.rows) == 0 {
		return theme.Muted.Render("no rows") + "\n" + theme.ResultFooter(0, r.duration, false)
	}
	shown := r.table
	shown.SetStyles(tableStyles(theme, focused))
	footer := theme.ResultFooter(len(r.rows), r.duration, r.truncated)
	if place := r.place(); place != "" {
		footer = ui.SplitLine(footer, theme.Subtle.Render(place), r.width)
	}
	return strings.TrimRight(shown.View(), "\n") + "\n" + footer
}
