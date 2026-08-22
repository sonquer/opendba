package ui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// ReadOnlyLabel is the mode every driver falls back to, and the only one the
// interface renders quietly.
const ReadOnlyLabel = "READ ONLY"

func (t *Theme) Rule(width int) string {
	if width <= 0 {
		return ""
	}
	return t.Separator.Render(strings.Repeat("─", width))
}

const (
	identityName   = 40
	identityServer = 48
)

// IdentityLine says where you are, in the shape of a shell prompt. The colour
// of the environment is the line across the top, not a mark in the text.
func (t *Theme) IdentityLine(env EnvColor, name, server, mode string) string {
	line := t.Prompt.Render("→ ") + t.Muted.Render("~ ") + t.Title.Render(truncate(name, identityName))
	if server != "" {
		line += t.Muted.Render(" · " + truncate(server, identityServer))
	}
	if mode != "" {
		line += t.Muted.Render(" · ") + t.Mode(mode)
	}
	return line
}

func (t *Theme) Mode(mode string) string {
	if strings.EqualFold(mode, ReadOnlyLabel) {
		return t.Subtle.Render(mode)
	}
	return t.Severity(SevWarn).Render(mode)
}

func (t *Theme) Section(title, tag string, width int) string {
	return SplitLine(t.SectionHead.Render(strings.ToUpper(title)), tag, width)
}

// SplitLine pushes right to the far end of the given width.
func SplitLine(left, right string, width int) string {
	if right == "" {
		return left
	}
	room := width - lipgloss.Width(left)
	if room < lipgloss.Width(right)+2 {
		room = lipgloss.Width(right) + 2
	}
	return left + lipgloss.NewStyle().Width(room).AlignHorizontal(lipgloss.Right).Render(right)
}

const gaugeWidth = 18

// Gauge is a measurement drawn as a bar, filled to the ratio and coloured by
// what the reading means.
func (t *Theme) Gauge(ratio float64, sev Severity) string {
	filled := int(ratio*float64(gaugeWidth) + 0.5)
	switch {
	case filled < 0:
		filled = 0
	case filled > gaugeWidth:
		filled = gaugeWidth
	}
	return t.Separator.Render("[") +
		t.Severity(sev).Render(strings.Repeat("█", filled)) +
		t.Subtle.Render(strings.Repeat("░", gaugeWidth-filled)) +
		t.Separator.Render("]")
}

// Blank is the space a gauge would take, so rows without a measurement still
// line up with the rows that have one.
func (t *Theme) Blank() string { return strings.Repeat(" ", gaugeWidth+2) }

// Reading is one line of a health report: what was measured, how far along it
// is, and what that means.
type Reading struct {
	Severity Severity
	Label    string
	Value    string
	Note     string
	Ratio    float64
	Measured bool
}

// Columns are the widths readings line up on. Measuring every reading on the
// screen once, rather than each block on its own, is what keeps separate groups
// in the same columns.
type Columns struct {
	Label int
	Value int
}

// Measure returns the columns a set of readings needs.
func Measure(readings []Reading) Columns {
	var at Columns
	for _, reading := range readings {
		at.Label = max(at.Label, lipgloss.Width(reading.Label))
		at.Value = max(at.Value, lipgloss.Width(reading.Value))
	}
	return at
}

// Readings draws the report as aligned rows, with a bar for the checks that are
// a proportion and the same space held open for the checks that are not.
func (t *Theme) Readings(readings []Reading, width int, at Columns) string {
	labels, values := at.Label, at.Value
	if labels == 0 && values == 0 {
		measured := Measure(readings)
		labels, values = measured.Label, measured.Value
	}
	lines := make([]string, 0, len(readings))
	for _, reading := range readings {
		bar := t.Blank()
		if reading.Measured {
			bar = t.Gauge(reading.Ratio, reading.Severity)
		}
		row := "  " + t.Severity(reading.Severity).Render(reading.Severity.Glyph()) + "  " +
			t.Value.Render(pad(reading.Label, labels)) + "  " + bar + "  " +
			t.Label.Render(pad(reading.Value, values))
		if reading.Note != "" {
			row += "  " + t.Muted.Render(truncate(reading.Note, width-lipgloss.Width(row)-2))
		}
		lines = append(lines, row)
	}
	return strings.Join(lines, "\n")
}

func (t *Theme) KV(label string, labelWidth int, value string) string {
	return t.Label.Render(pad(label, labelWidth)) + " " + t.Value.Render(value)
}

func Dotted(parts ...string) string {
	kept := parts[:0]
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, " · ")
}

func (t *Theme) Badge(text string, sev Severity) string {
	return t.Severity(sev).Bold(true).Render(text)
}

// pad fills a cell to the given width, clipping what does not fit. Widths come
// from lipgloss so that wide runes and styled text are measured, not counted.
func pad(s string, w int) string {
	if w <= 0 {
		return ""
	}
	return lipgloss.NewStyle().Width(w).MaxHeight(1).Render(truncate(s, w))
}

// truncate clips styled text without cutting an escape sequence in half.
func truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	return ansi.Truncate(s, w, "…")
}

// Truncate clips styled text to a width, marking what was left out.
func Truncate(s string, w int) string { return truncate(s, w) }

// Pad fills a cell to a width.
func Pad(s string, w int) string { return pad(s, w) }
