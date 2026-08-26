package render

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/sonquer/opendba/src/tools/pkg/cover"
)

func tableRows() []cover.TableRow {
	return []cover.TableRow{
		{Label: "All files", Stats: cover.Stats{Statements: 10, CoveredStatements: 8, Lines: 10, CoveredLines: 8, Funcs: 4, CoveredFuncs: 3}},
		{Label: "cli/pkg/sqlguard", Depth: 1, Stats: cover.Stats{Statements: 4, CoveredStatements: 4, Lines: 4, CoveredLines: 4, Funcs: 2, CoveredFuncs: 2}},
		{Label: "guard.go", Depth: 2, Stats: cover.Stats{Statements: 6, CoveredStatements: 1, Uncovered: []cover.LineRange{{From: 3, To: 9}}}},
	}
}

func TestCoverageTable(t *testing.T) {
	out := ansi.Strip(DefaultTheme().CoverageTable(tableRows(), 95))
	for _, want := range []string{"file", "stmts", "funcs", "lines", "uncovered", "ALL FILES", "cli/pkg/sqlguard", "guard.go", "100.00", "16.67", "3-9"} {
		if !strings.Contains(out, want) {
			t.Errorf("table missing %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "╭") || !strings.Contains(out, "╯") {
		t.Errorf("the table must be drawn with a border:\n%s", out)
	}
}

func TestCoverageTableWithoutRows(t *testing.T) {
	if got := DefaultTheme().CoverageTable(nil, 95); got != "" {
		t.Errorf("CoverageTable() = %q", got)
	}
}

func TestCoverageColour(t *testing.T) {
	theme := DefaultTheme()
	cases := []struct {
		value float64
		want  any
	}{
		{100, theme.Pass},
		{95, theme.Pass},
		{60, theme.Warn},
		{47.5, theme.Warn},
		{10, theme.Fail},
	}
	for _, c := range cases {
		if got := theme.coverageColor(c.value, 95); got != c.want {
			t.Errorf("coverageColor(%v) = %v, want %v", c.value, got, c.want)
		}
	}
}

func TestCoverageStyleCoversEveryColumn(t *testing.T) {
	rows := tableRows()
	style := DefaultTheme().coverageStyle(rows, 95)
	if style(-1, 0).GetForeground() == nil {
		t.Error("the header must be styled")
	}
	if !style(0, 0).GetBold() {
		t.Error("the total row must be bold")
	}
	if style(1, 1).GetAlign() == 0 {
		t.Error("numeric columns must be right aligned")
	}
	if style(99, 0).GetBold() {
		t.Error("rows outside the data must fall back to the plain style")
	}
	if style(1, len(coverageHeaders)-1).GetForeground() != DefaultTheme().Muted {
		t.Error("the uncovered column must stay muted")
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("12345", 10); got != "12345" {
		t.Errorf("truncate() = %q", got)
	}
	if got := truncate("1234567890", 5); got != "1234…" {
		t.Errorf("truncate() = %q", got)
	}
	if got := truncate("12345", 0); got != "" {
		t.Errorf("truncate() = %q", got)
	}
}

func TestPercentFormatting(t *testing.T) {
	if got := percent(37.5); got != "37.50" {
		t.Errorf("percent() = %q", got)
	}
}
