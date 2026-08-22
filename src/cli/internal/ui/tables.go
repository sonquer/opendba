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

// TableList is what the server holds and how it is being read: the size on
// disk, and a bar for the share of reads the server answered from memory.
func (t *Theme) TableList(tables []driver.Table, cursor, width int) string {
	if len(tables) == 0 {
		return t.Muted.Render("no tables here")
	}
	list := make([]Listing, 0, len(tables))
	for i, entry := range tables {
		list = append(list, Listing{
			Name:     entry.Qualified(),
			Facts:    []string{Count(entry.Rows), ByteSize(entry.Size), ByteSize(entry.IndexSize)},
			Note:     cached(entry),
			Ratio:    entry.CacheHit,
			Measured: entry.Stats && entry.CacheHit > 0,
			Severity: t.Severity4Driver(entry.Health()),
			Cursor:   i == cursor,
		})
	}
	return t.Rows(list, Head{
		Name:  "table",
		Facts: []string{"rows", "size", "indexes"},
		Gauge: "read from memory",
	}, width)
}

// cached says what the bar on a table row is measuring, in the words of what
// the number means rather than of the counter it came from.
func cached(table driver.Table) string {
	if !table.Stats || table.CacheHit <= 0 {
		return "never read"
	}
	return fmt.Sprintf("%.0f%%", table.CacheHit*100)
}

// IndexList is what each index costs and what it is worth: its size, how often
// the server read it, and a bar for its share of the reads on its table.
func (t *Theme) IndexList(indexes []driver.Index, cursor, width int) string {
	if len(indexes) == 0 {
		return t.Muted.Render("no indexes here")
	}
	busiest := map[string]int64{}
	for _, index := range indexes {
		busiest[index.Table] = max(busiest[index.Table], index.Scans)
	}
	list := make([]Listing, 0, len(indexes))
	for i, index := range indexes {
		share, measured := float64(0), false
		if top := busiest[index.Table]; top > 0 {
			share, measured = float64(index.Scans)/float64(top), true
		}
		list = append(list, Listing{
			Name:     index.Name,
			Facts:    []string{index.Table, ByteSize(index.Size), reads(index)},
			Note:     worth(index),
			Ratio:    share,
			Measured: measured,
			Severity: t.Severity4Driver(index.Health()),
			Cursor:   i == cursor,
		})
	}
	return t.Rows(list, Head{
		Name:  "index",
		Facts: []string{"on table", "size", "reads"},
		Gauge: "share of its table",
		Note:  "why",
	}, width)
}

// reads is how often the server went to this index, or a word for a driver that
// does not count.
func reads(index driver.Index) string {
	if !index.Stats {
		return "n/a"
	}
	return Count(index.Scans)
}

// worth says in one word why an index is on the list. The sentence behind the
// word is on the page, which is what enter is for.
func worth(index driver.Index) string {
	switch {
	case !index.Stats:
		return "unknown"
	case index.Primary:
		return "primary"
	case index.Idle():
		return "idle"
	case index.Unique:
		return "unique"
	case index.Scans == 0:
		return "unread"
	default:
		return "used"
	}
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

// ByteSize writes a size the way a person reads one.
func ByteSize(bytes int64) string { return driver.ByteSize(bytes) }

// Duration writes a length of time at the precision that matters.
func Duration(d time.Duration) string { return driver.Duration(d) }
