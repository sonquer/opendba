package cover

import (
	"fmt"
	"io"
	"strings"
)

// WriteMarkdown writes the report as a markdown table, for a continuous
// integration job summary.
func WriteMarkdown(w io.Writer, rows []TableRow, min float64, title string) error {
	if len(rows) == 0 {
		return nil
	}
	var b strings.Builder
	total := rows[0].Stats
	status := "passed"
	if total.StatementPercent() < min {
		status = "failed"
	}
	fmt.Fprintf(&b, "## %s coverage\n\n", title)
	fmt.Fprintf(&b, "**%.2f%%** of %d statements · gate %.0f%% %s\n\n",
		total.StatementPercent(), total.Statements, min, status)
	b.WriteString("| File | % Stmts | % Funcs | % Lines | Uncovered Lines |\n")
	b.WriteString("| --- | ---: | ---: | ---: | --- |\n")
	for _, row := range rows {
		label := strings.Repeat("&nbsp;&nbsp;", row.Depth) + row.Label
		if row.Depth == 0 {
			label = "**" + label + "**"
		}
		fmt.Fprintf(&b, "| %s | %.2f | %.2f | %.2f | %s |\n",
			label,
			row.Stats.StatementPercent(),
			row.Stats.FuncPercent(),
			row.Stats.LinePercent(),
			markdownCell(row.Stats.UncoveredList()),
		)
	}
	b.WriteString("\n")
	_, err := io.WriteString(w, b.String())
	if err != nil {
		return fmt.Errorf("write coverage summary: %w", err)
	}
	return nil
}

func markdownCell(value string) string {
	if value == "" {
		return ""
	}
	if len(value) > 60 {
		value = value[:60] + "…"
	}
	return "`" + value + "`"
}
