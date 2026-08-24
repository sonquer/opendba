package render

import (
	"fmt"
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"

	"github.com/sonquer/tui4db/src/tools/pkg/cover"
)

const uncoveredWidth = 26

var coverageHeaders = []string{"file", "stmts", "funcs", "lines", "uncovered"}

func (t Theme) CoverageTable(rows []cover.TableRow, min float64) string {
	if len(rows) == 0 {
		return ""
	}
	rendered := table.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(lipgloss.NewStyle().Foreground(t.Muted)).
		BorderRow(false).
		BorderColumn(false).
		Headers(coverageHeaders...).
		StyleFunc(t.coverageStyle(rows, min))
	for _, row := range rows {
		rendered.Row(t.coverageCells(row)...)
	}
	return rendered.String() + "\n"
}

func (t Theme) coverageCells(row cover.TableRow) []string {
	label := strings.Repeat("  ", row.Depth) + row.Label
	if row.Depth == 0 {
		label = strings.ToUpper(row.Label)
	}
	return []string{
		label,
		percent(row.Stats.StatementPercent()),
		percent(row.Stats.FuncPercent()),
		percent(row.Stats.LinePercent()),
		truncate(row.Stats.UncoveredList(), uncoveredWidth),
	}
}

func (t Theme) coverageStyle(rows []cover.TableRow, min float64) table.StyleFunc {
	return func(row, col int) lipgloss.Style {
		style := lipgloss.NewStyle().Padding(0, 1)
		if col > 0 && col < len(coverageHeaders)-1 {
			style = style.Align(lipgloss.Right)
		}
		if row == table.HeaderRow {
			return style.Foreground(t.Muted)
		}
		if row < 0 || row >= len(rows) {
			return style
		}
		entry := rows[row]
		style = style.Foreground(t.coverageColor(entry.Stats.StatementPercent(), min))
		if entry.Depth == 0 {
			style = style.Bold(true)
		}
		if col == len(coverageHeaders)-1 {
			style = style.Foreground(t.Muted)
		}
		return style
	}
}

func (t Theme) coverageColor(value, min float64) color.Color {
	switch {
	case value >= min:
		return t.Pass
	case value >= min/2:
		return t.Warn
	default:
		return t.Fail
	}
}

func percent(value float64) string { return fmt.Sprintf("%.2f", value) }

func truncate(s string, w int) string {
	if lipgloss.Width(s) <= w {
		return s
	}
	runes := []rune(s)
	for i := len(runes); i > 0; i-- {
		if candidate := string(runes[:i]) + "…"; lipgloss.Width(candidate) <= w {
			return candidate
		}
	}
	return ""
}
