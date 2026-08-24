package render

import (
	"fmt"
	"image/color"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/sonquer/opendba/src/tools/internal/core"
)

type Theme struct {
	Fg     color.Color
	Muted  color.Color
	Accent color.Color
	Pass   color.Color
	Warn   color.Color
	Fail   color.Color
	Skip   color.Color
}

func DefaultTheme() Theme {
	return Theme{
		Fg:     lipgloss.Color("#dcdcdc"),
		Muted:  lipgloss.Color("#8a8a8a"),
		Accent: lipgloss.Color("#8bd5ca"),
		Pass:   lipgloss.Color("#8fce9b"),
		Warn:   lipgloss.Color("#e6c384"),
		Fail:   lipgloss.Color("#ea8a94"),
		Skip:   lipgloss.Color("#8aadf4"),
	}
}

func (t Theme) statusColor(s core.Status) color.Color {
	switch s {
	case core.StatusPass:
		return t.Pass
	case core.StatusFail:
		return t.Fail
	case core.StatusSkip:
		return t.Skip
	case core.StatusRunning:
		return t.Accent
	default:
		return t.Muted
	}
}

func (t Theme) Status(s core.Status) string {
	return lipgloss.NewStyle().Foreground(t.statusColor(s)).Render(s.Glyph())
}

func (t Theme) muted(text string) string {
	return lipgloss.NewStyle().Foreground(t.Muted).Render(text)
}

func (t Theme) Line(r core.Report, nameWidth int) string {
	name := lipgloss.NewStyle().Foreground(t.Fg).Render(pad(r.Check, nameWidth))
	parts := []string{t.Status(r.Status), name}
	if r.Summary != "" {
		parts = append(parts, t.muted(r.Summary))
	}
	if r.Duration > 0 {
		parts = append(parts, t.muted(Duration(r.Duration)))
	}
	return strings.Join(parts, " ")
}

func (t Theme) Detail(r core.Report) string {
	var b strings.Builder
	for _, row := range r.Rows {
		fmt.Fprintf(&b, "  %s %s %s %s\n",
			t.Status(row.Status),
			lipgloss.NewStyle().Foreground(t.Fg).Render(pad(row.Label, 32)),
			lipgloss.NewStyle().Foreground(t.statusColor(row.Status)).Render(pad(row.Value, 8)),
			t.muted(row.Note),
		)
	}
	for _, line := range r.Detail {
		fmt.Fprintf(&b, "  %s\n", t.muted(line))
	}
	return b.String()
}

func (t Theme) Reports(reports []core.Report) string {
	width := 0
	for _, r := range reports {
		if n := lipgloss.Width(r.Check); n > width {
			width = n
		}
	}
	var b strings.Builder
	for _, r := range reports {
		b.WriteString(t.Line(r, width) + "\n")
		if detail := t.Detail(r); detail != "" {
			b.WriteString(detail)
		}
	}
	b.WriteString(t.Verdict(reports) + "\n")
	return b.String()
}

func (t Theme) Verdict(reports []core.Report) string {
	status := core.Aggregate(reports)
	counts := map[core.Status]int{}
	for _, r := range reports {
		counts[r.Status]++
	}
	summary := fmt.Sprintf("%d passed · %d failed · %d skipped",
		counts[core.StatusPass], counts[core.StatusFail], counts[core.StatusSkip])
	label := "all checks passed"
	if status == core.StatusFail {
		label = "checks failed"
	}
	return lipgloss.NewStyle().Foreground(t.statusColor(status)).Bold(true).Render(label) + " " + t.muted(summary)
}

func Duration(d time.Duration) string {
	switch {
	case d >= time.Minute:
		return fmt.Sprintf("%.1fm", d.Minutes())
	case d >= time.Second:
		return fmt.Sprintf("%.1fs", d.Seconds())
	case d > 0:
		return fmt.Sprintf("%dms", d.Milliseconds())
	default:
		return ""
	}
}

func pad(s string, w int) string {
	if n := w - lipgloss.Width(s); n > 0 {
		return s + strings.Repeat(" ", n)
	}
	return s
}
