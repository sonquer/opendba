package app

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/table"
	"charm.land/bubbles/v2/textarea"
	"charm.land/lipgloss/v2"

	"github.com/sonquer/tui4db/src/cli/internal/export"
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
	editor.SetVirtualCursor(false)
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
	editor.KeyMap.DeleteCharacterBackward = key.NewBinding(
		key.WithKeys("backspace", "shift+backspace", "ctrl+h"),
		key.WithHelp("backspace", "delete character backward"))
	editor.KeyMap.InsertNewline = key.NewBinding(
		key.WithKeys("enter", "shift+enter", "ctrl+m"),
		key.WithHelp("enter", "insert newline"))
	return editor
}

type results struct {
	table  table.Model
	theme  *ui.Theme
	column int

	// first is the row at the top of the pane and cursor is the row under the
	// cursor, counted from the top of the whole result. The table is only ever
	// given the rows between them: handing a table with fifty thousand rows in
	// it fifty thousand rows on every frame is what made scrolling a large
	// result feel like the program had stopped.
	first  int
	cursor int

	// anchor is the row a drag started on, or minus one when nothing is being
	// dragged. What lies between it and the cursor is what a release copies.
	anchor int

	// widths is how wide each column wants to be, measured once from the rows
	// that arrived rather than from the ones on screen. Measuring the visible
	// rows instead would make every column change width while scrolling.
	widths []int

	width     int
	height    int
	statement string
	columns   []string

	// rows are the values the driver returned, and cells are those values as a
	// table can draw them. Both are kept: what is drawn is folded onto one line
	// and cut to the width of a column, which is right on a screen and wrong in
	// a file or on the clipboard.
	rows      [][]any
	cells     [][]string
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
		cells:     asCells(msg.rows),
		anchor:    -1,
		duration:  msg.duration,
		truncated: msg.truncated,
		present:   true,
	}
	if msg.err != nil {
		result.failure = msg.err.Error()
		return result
	}
	result.widths = naturalWidths(msg.columns, sample(result.cells))
	return result.rebuild()
}

// sample is the rows a column width is measured from. Every row would be more
// accurate and would also walk a whole result to draw the first screen of it;
// the rows people look at are at the top, and a value wider than the column is
// cut the same way whether the column was measured from ten rows or ten
// thousand.
func sample(cells [][]string) [][]string {
	if len(cells) <= sampleRows {
		return cells
	}
	return cells[:sampleRows]
}

// sampleRows is how many rows a column width is measured from.
const sampleRows = 500

// visible is how many rows of the result the pane can hold, once the header and
// the line under it have had their two.
func (r results) visible() int {
	return max(r.height-resultHeaderRows, 1)
}

// resultHeaderRows is what a table spends above its rows: the column names, and
// the rule under them.
const resultHeaderRows = 2

// follow moves the window so the row under the cursor is in it.
func (r results) follow() results {
	held := r.visible()
	switch {
	case r.cursor < r.first:
		r.first = r.cursor
	case r.cursor >= r.first+held:
		r.first = r.cursor - held + 1
	}
	r.first = clamp(r.first, 0, max(len(r.rows)-held, 0))
	return r
}

// move takes the cursor through the result, stopping at the ends rather than
// wrapping: a list that jumps back to the top when it runs off the bottom reads
// as a list that lost its place.
func (r results) move(step int) results {
	if !r.present || r.failure != "" || len(r.rows) == 0 {
		return r
	}
	r.cursor = clamp(r.cursor+step, 0, len(r.rows)-1)
	return r.follow().rebuild()
}

// rowAt says which row a line of the pane is showing, counting from the first
// line the rows themselves start on.
func (r results) rowAt(line int) (int, bool) {
	at := r.first + line - resultHeaderRows
	if line < resultHeaderRows || at < 0 || at >= len(r.rows) {
		return 0, false
	}
	return at, true
}

// rebuild draws the columns that fit from the one the cursor is on, which is
// how a result with thirty columns is read at all.
func (r results) rebuild() results {
	columns, rows := r.window()
	r.table = table.New(
		table.WithColumns(columns),
		table.WithRows(rowsFor(rows)),
		table.WithHeight(max(r.height, 1)),
		table.WithWidth(tableWidth(columns, r.width)),
		table.WithStyles(tableStyles(r.theme, false)),
		table.WithFocused(true),
	)
	r.table.SetCursor(r.cursor - r.first)
	return r
}

// window is the slice of the result the pane is showing: the columns that fit
// starting at the one in front, and only the rows between the top of the pane
// and the bottom of it.
func (r results) window() ([]table.Column, [][]string) {
	if len(r.columns) == 0 {
		return nil, nil
	}
	from := min(max(r.column, 0), len(r.columns)-1)
	names := r.columns[from:]
	held := min(r.first+r.visible(), len(r.cells))
	cells := make([][]string, 0, max(held-r.first, 0))
	for _, row := range r.cells[min(r.first, held):held] {
		if from < len(row) {
			cells = append(cells, row[from:])
			continue
		}
		cells = append(cells, nil)
	}
	columns := columnsFor(names, r.widths[from:], r.width)
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

// place4Row says where the cursor is in a result taller than the pane, because
// a table showing rows nine hundred to nine hundred and twenty with no way to
// tell is a table nobody knows their way around.
func (r results) place4Row() string {
	if picked, ok := r.picked(); ok && len(picked) > 1 {
		return fmt.Sprintf("%d selected", len(picked))
	}
	if len(r.rows) <= r.visible() {
		return ""
	}
	return fmt.Sprintf("row %d of %d", r.cursor+1, len(r.rows))
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
	at := min(max(r.cursor, 0), len(r.rows)-1)
	row := r.rows[at]
	pairs := make([]pair, 0, len(r.columns))
	for i, name := range r.columns {
		value := "∅"
		if i < len(row) && row[i] != nil {
			value = export.Text(row[i])
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
func columnsFor(names []string, measured []int, width int) []table.Column {
	widths := append([]int(nil), measured...)
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

// asCells turns the values a driver returned into what a table can draw, which is
// lossy on purpose and why the values themselves are kept beside it.
func asCells(rows [][]any) [][]string {
	cells := make([][]string, 0, len(rows))
	for _, row := range rows {
		cells = append(cells, ui.Strings(row))
	}
	return cells
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
	return r.follow().rebuild()
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
	if place := ui.Dotted(r.place4Row(), r.place()); place != "" {
		footer = ui.SplitLine(footer, theme.Subtle.Render(place), r.width)
	}
	return strings.TrimRight(shown.View(), "\n") + "\n" + footer
}

// roll moves the row the cursor is on by a notch of the wheel.
func (r results) roll(step int) results { return r.move(step) }

// cell is the one value under the cursor, in the column the pane has in front.
func (r results) cell() (string, bool) {
	if !r.present || r.failure != "" || len(r.rows) == 0 {
		return "", false
	}
	at := min(max(r.cursor, 0), len(r.rows)-1)
	column := min(max(r.column, 0), len(r.columns)-1)
	row := r.rows[at]
	if column >= len(row) {
		return "", false
	}
	return export.Text(row[column]), true
}

// picked is the rows between where a drag started and where it is now, in the
// order they are drawn rather than the order they were dragged in.
func (r results) picked() ([][]any, bool) {
	if r.anchor < 0 || !r.present || len(r.rows) == 0 {
		return nil, false
	}
	from, to := min(r.anchor, r.cursor), max(r.anchor, r.cursor)
	from = clamp(from, 0, len(r.rows)-1)
	to = clamp(to, 0, len(r.rows)-1)
	return r.rows[from : to+1], true
}

// startPicking begins a selection on a row.
func (r results) startPicking(at int) results {
	if !r.present || r.failure != "" || len(r.rows) == 0 {
		return r
	}
	r.anchor = clamp(at, 0, len(r.rows)-1)
	r.cursor = r.anchor
	return r.follow().rebuild()
}

// pickTo carries a selection to another row.
func (r results) pickTo(at int) results {
	if r.anchor < 0 || len(r.rows) == 0 {
		return r
	}
	r.cursor = clamp(at, 0, len(r.rows)-1)
	return r.follow().rebuild()
}

// stopPicking ends a selection, leaving the cursor where it finished.
func (r results) stopPicking() results {
	r.anchor = -1
	return r
}
