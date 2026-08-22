package app

import (
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
	maxEditorRows  = 12
	minResultsRows = 3
	minColumnWide  = 6
)

// editorRows splits the body area between the statement and its results.
func editorRows(height int) int {
	rows := ui.BodyHeight(height) / 3
	switch {
	case rows < minEditorRows:
		return minEditorRows
	case rows > maxEditorRows:
		return maxEditorRows
	default:
		return rows
	}
}

func resultsRows(height int) int {
	rows := ui.BodyHeight(height) - editorRows(height) - 5
	if rows < minResultsRows {
		return minResultsRows
	}
	return rows
}

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
	columns := columnsFor(msg.columns, msg.rows, width)
	result.table = table.New(
		table.WithColumns(columns),
		table.WithRows(rowsFor(msg.rows)),
		table.WithHeight(height),
		table.WithWidth(tableWidth(columns, width)),
		table.WithStyles(tableStyles(theme)),
		table.WithFocused(true),
	)
	return result
}

func tableStyles(theme *ui.Theme) table.Styles {
	styles := table.DefaultStyles()
	styles.Header = styles.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(theme.P.Border).
		BorderBottom(true).
		Foreground(theme.P.Muted).
		Bold(false)
	styles.Cell = styles.Cell.Foreground(theme.P.Fg)
	styles.Selected = styles.Selected.Foreground(theme.P.OnEnv).Background(theme.P.Accent)
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
	columns := columnsFor(r.columns, r.rows, width)
	r.table.SetColumns(columns)
	r.table.SetWidth(tableWidth(columns, width))
	r.table.SetHeight(height)
	return r
}

func (r results) update(msg tea.Msg) (results, tea.Cmd) {
	if !r.present || r.failure != "" {
		return r, nil
	}
	updated, cmd := r.table.Update(msg)
	r.table = updated
	return r, cmd
}

func (r results) render(theme *ui.Theme) string {
	if !r.present {
		return theme.Muted.Render("nothing has run yet")
	}
	if r.failure != "" {
		return theme.Error.Render("✗ " + r.failure)
	}
	if len(r.rows) == 0 {
		return theme.Muted.Render("no rows") + "\n" + theme.ResultFooter(0, r.duration, false)
	}
	return strings.TrimRight(r.table.View(), "\n") + "\n" +
		theme.ResultFooter(len(r.rows), r.duration, r.truncated)
}
