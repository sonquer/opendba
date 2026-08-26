package ui

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
)

// BarStyle is how a measurement is drawn.
type BarStyle struct {
	Name  string
	Full  string
	Empty string
	Parts []string
	Open  string
	Close string

	// Dim draws the empty cells in a dark tint of the colour rather than the colour
	// itself, which is what a style needs when its empty glyph is as heavy as its
	// full one.
	Dim bool

	// Neutral draws the empty cells in grey rather than in the colour of the
	// reading, so the part of the scale nothing was measured against says nothing
	// about the measurement.
	Neutral bool
}

// BarStyles are the shapes a bar can take, in the order a person should be
// offered them.
var BarStyles = []BarStyle{
	{
		Name: "pipes", Full: "|", Empty: "|", Neutral: true,
		Open: "[", Close: "]",
	},
	{
		Name: "smooth", Full: "█", Empty: "█", Dim: true,
		Parts: []string{"▏", "▎", "▍", "▌", "▋", "▊", "▉"},
		Open:  "", Close: "",
	},
	{
		Name: "shade", Full: "█", Empty: "░",
		Open: "[", Close: "]",
	},
	{
		Name: "rail", Full: "━", Empty: "─", Dim: true,
		Open: "", Close: "",
	},
	{
		Name: "segments", Full: "▰", Empty: "▱",
		Open: "", Close: "",
	},
	{
		Name: "braille", Full: "█", Empty: "⣿", Dim: true,
		Open: "[", Close: "]",
	},
	{
		Name: "ascii", Full: "#", Empty: "-",
		Open: "[", Close: "]",
	},
}

// DefaultBarStyle is the shape a bar takes when nobody has chosen one.
const DefaultBarStyle = "pipes"

// BarStyleNamed returns a style by name, falling back to the default rather
// than to nothing, because a bar is not a setting worth failing to start over.
func BarStyleNamed(name string) BarStyle {
	for _, style := range BarStyles {
		if style.Name == name {
			return style
		}
	}
	for _, style := range BarStyles {
		if style.Name == DefaultBarStyle {
			return style
		}
	}
	return BarStyles[0]
}

// BarStyleNames is what a setting can be set to.
func BarStyleNames() []string {
	names := make([]string, 0, len(BarStyles))
	for _, style := range BarStyles {
		names = append(names, style.Name)
	}
	return names
}

// Bars sets the shape every bar in this theme is drawn with.
func (t *Theme) Bars(name string) { t.style = BarStyleNamed(name) }

// draw renders a bar of the given width, filled to the ratio.
func (t *Theme) draw(ratio float64, sev Severity, width int) string {
	return t.drawOn(ratio, sev, width, nil)
}

// Track is progress rather than a reading, so it is drawn in the accent colour:
// a download is neither healthy nor unhealthy, and colouring it as though it
// were says something about it that is not true.
func (t *Theme) Track(ratio float64, width int) string {
	style := t.shape()
	inner := width
	if style.Open != "" {
		inner = max(width-lipgloss.Width(style.Open)-lipgloss.Width(style.Close), 1)
	}
	return t.paint(ratio, lipgloss.NewStyle().Foreground(t.P.Accent), inner, nil)
}

// shape is the style a bar is drawn with, which is the default until somebody
// has chosen one.
func (t *Theme) shape() BarStyle {
	if t.style.Full == "" {
		return BarStyleNamed(DefaultBarStyle)
	}
	return t.style
}

// drawOn is the same bar on a background, for the row a cursor is sitting on.
func (t *Theme) drawOn(ratio float64, sev Severity, width int, ground color.Color) string {
	return t.paint(ratio, t.Bar(sev), width, ground)
}

// paint is the bar itself, given the colour to fill it with.
func (t *Theme) paint(ratio float64, colour lipgloss.Style, width int, ground color.Color) string {
	style := t.shape()
	full, part, empty := cells(ratio, width, len(style.Parts))
	track := colour
	switch {
	case style.Neutral:
		track = t.Subtle
	case style.Dim:
		track = lipgloss.NewStyle().Foreground(dim(colour.GetForeground(), t.P.Bg))
	}
	if ground != nil {
		colour, track = colour.Background(ground), track.Background(ground)
	}
	bar := colour.Render(strings.Repeat(style.Full, full))
	if part > 0 {
		bar += colour.Render(style.Parts[part-1])
	}
	bar += track.Render(strings.Repeat(style.Empty, empty))
	if style.Open == "" {
		return bar
	}
	return colour.Render(style.Open) + bar + colour.Render(style.Close)
}

// cells splits a ratio into whole cells, one partial cell, and the rest. The
// partial cell is the eighth of a cell a whole-cell bar has to throw away.
func cells(ratio float64, width, parts int) (full, part, empty int) {
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	steps := parts + 1
	total := int(ratio*float64(width*steps) + 0.5)
	full, part = total/steps, total%steps
	if full >= width {
		return width, 0, 0
	}
	return full, part, width - full - min(part, 1)
}

// BarWidth is the room a bar takes on a row, brackets included, so a reading
// with nothing to measure still lines up with the ones that have something.
func (t *Theme) BarWidth(width int) int {
	if t.style.Open == "" {
		return width
	}
	return width + 2
}

// dim walks a colour most of the way to the page, for the half of a bar that is
// the scale rather than the measurement.
func dim(hue, ground color.Color) color.Color {
	blend := lipgloss.Blend1D(dimSteps, hue, ground)
	return blend[dimSteps-3]
}

const dimSteps = 12
