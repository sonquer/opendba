package ui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"

	"github.com/sonquer/tui4db/src/cli/internal/driver"
	"github.com/sonquer/tui4db/src/cli/pkg/sqlguard"
)

const maxCellWidth = 48

func (t *Theme) Env(env EnvColor) string {
	return lipgloss.NewStyle().Foreground(env.Swatch()).Render(env.Glyph())
}

func (t *Theme) ConnectionLine(name string, env EnvColor, server, mode string) string {
	return t.Env(env) + " " + t.Title.Render(name) + " " + t.Muted.Render(Dotted(server, strings.ToLower(mode)))
}

func (t *Theme) Severity4Driver(severity driver.Severity) Severity {
	switch severity {
	case driver.SeverityOK:
		return SevOK
	case driver.SeverityWarn:
		return SevWarn
	case driver.SeverityCritical:
		return SevCritical
	case driver.SeverityInfo:
		return SevInfo
	default:
		return SevInactive
	}
}

func (t *Theme) FindingTable(findings []driver.Finding) string {
	if len(findings) == 0 {
		return t.Muted.Render("nothing to report")
	}
	rendered := t.table("subsystem", "status", "value", "note")
	for _, finding := range findings {
		severity := t.Severity4Driver(finding.Severity)
		rendered.Row(finding.Subsystem, t.Severity(severity).Render(severity.Glyph()+" "+severity.String()), finding.Value, finding.Note)
	}
	return rendered.String()
}

func (t *Theme) Counts(healthy, warnings, failing int) string {
	parts := []string{
		t.Severity(SevOK).Render(fmt.Sprintf("%d ok", healthy)),
		t.Severity(SevWarn).Render(Plural(warnings, "warning", "warnings")),
		t.Severity(SevCritical).Render(fmt.Sprintf("%d failing", failing)),
	}
	return strings.Join(parts, t.Muted.Render(" · "))
}

func (t *Theme) TableList(tables []driver.Table) string {
	if len(tables) == 0 {
		return t.Muted.Render("no tables here")
	}
	rendered := t.plainTable("table", "kind", "rows", "size")
	for _, entry := range tables {
		rendered.Row(entry.Qualified(), entry.Kind, Count(entry.Rows), ByteSize(entry.Size))
	}
	return rendered.String()
}

func (t *Theme) IndexList(indexes []driver.Index) string {
	if len(indexes) == 0 {
		return t.Muted.Render("no indexes here")
	}
	rendered := t.plainTable("index", "table", "size", "scans")
	for _, index := range indexes {
		rendered.Row(index.Name, index.Table, ByteSize(index.Size), Count(index.Scans))
	}
	return rendered.String()
}

func (t *Theme) ResultTable(columns []string, rows [][]string) string {
	if len(columns) == 0 {
		return t.Muted.Render("no columns") + "\n"
	}
	rendered := t.table(columns...)
	for _, row := range rows {
		rendered.Row(row...)
	}
	return rendered.String() + "\n"
}

func (t *Theme) ResultFooter(rows int, duration time.Duration, truncated bool) string {
	parts := []string{fmt.Sprintf("%d rows", rows), Duration(duration)}
	if truncated {
		parts = append(parts, "truncated")
	}
	return t.Muted.Render(Dotted(parts...))
}

// Verdict renders how a statement was classified. A width of zero keeps the
// reason whole, which is what the command line wants.
func (t *Theme) Verdict(result sqlguard.Result, width int) string {
	severity := SevOK
	switch result.Verdict {
	case sqlguard.Warn:
		severity = SevWarn
	case sqlguard.Block:
		severity = SevCritical
	}
	head := t.Severity(severity).Render(severity.Glyph()+" "+string(result.Verdict)) + " " +
		t.Value.Render(string(result.Kind))
	reason := Reason(result.Reason)
	if width > 0 {
		reason = truncate(reason, width-lipgloss.Width(head)-3)
	}
	if reason == "" {
		return head
	}
	return head + " " + t.Muted.Render("· "+reason)
}

// Reason drops the list of tokens a parser was hoping for. It is accurate and
// unreadable, and the position it comes with already says where to look.
func Reason(reason string) string {
	if cut := strings.Index(reason, " expecting "); cut > 0 {
		return reason[:cut]
	}
	return reason
}

func (t *Theme) plainTable(headers ...string) *table.Table {
	return t.table(headers...).
		Border(lipgloss.NormalBorder()).
		BorderTop(false).
		BorderBottom(false).
		BorderLeft(false).
		BorderRight(false).
		BorderHeader(true)
}

func (t *Theme) table(headers ...string) *table.Table {
	return table.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(lipgloss.NewStyle().Foreground(t.P.Border)).
		BorderRow(false).
		BorderColumn(false).
		Headers(headers...).
		StyleFunc(func(row, _ int) lipgloss.Style {
			style := lipgloss.NewStyle().Padding(0, 1)
			if row == table.HeaderRow {
				return style.Foreground(t.P.Muted)
			}
			return style.Foreground(t.P.Fg)
		})
}

// Plural renders a count with the word that fits it.
func Plural(count int, one, many string) string {
	if count == 1 {
		return fmt.Sprintf("%d %s", count, one)
	}
	return fmt.Sprintf("%d %s", count, many)
}

func Strings(values []any) []string {
	cells := make([]string, 0, len(values))
	for _, value := range values {
		cells = append(cells, Cell(value))
	}
	return cells
}

func Cell(value any) string {
	if value == nil {
		return "∅"
	}
	text := fmt.Sprintf("%v", value)
	if bytes, ok := value.([]byte); ok {
		text = string(bytes)
	}
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\n", " "), "\t", " ")
	return Truncate(text, maxCellWidth)
}

func Count(value int64) string {
	if value < 0 {
		return "n/a"
	}
	text := fmt.Sprintf("%d", value)
	var parts []string
	for len(text) > 3 {
		parts = append([]string{text[len(text)-3:]}, parts...)
		text = text[:len(text)-3]
	}
	return strings.Join(append([]string{text}, parts...), ",")
}

func ByteSize(bytes int64) string {
	if bytes < 0 {
		return "n/a"
	}
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	value, exponent := float64(bytes), 0
	for value >= unit && exponent < 4 {
		value /= unit
		exponent++
	}
	return fmt.Sprintf("%.1f %ciB", value, "KMGT"[exponent-1])
}

func Duration(d time.Duration) string {
	switch {
	case d >= time.Minute:
		return fmt.Sprintf("%.1fm", d.Minutes())
	case d >= time.Second:
		return fmt.Sprintf("%.2fs", d.Seconds())
	default:
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
}
