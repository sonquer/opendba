package ui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

const (
	// MaxTextWidth keeps prose and tables readable on a wide terminal.
	MaxTextWidth = 96

	minChromeWidth  = 40
	minChromeHeight = 11

	gutter    = 2
	chromePad = 9
)

// Frame is everything Chrome needs to draw a screen.
type Frame struct {
	Width  int
	Height int
	Env    EnvColor
	Header string
	Body   string
	Footer string
}

// Chrome frames a screen: a thin line of the environment colour across the very
// top, the header and a rule under it, the body filling everything in between,
// and the footer on the last written row of the window.
func (t *Theme) Chrome(f Frame) string {
	inner := FrameWidth(f.Width)
	height := f.Height
	if height < minChromeHeight {
		height = minChromeHeight
	}
	rows := []string{
		t.EnvLine(f.Env, inner),
		strings.TrimRight(f.Header, "\n"),
		t.Rule(inner),
		"",
		Fit(f.Body, BodyHeight(height)),
		"",
		t.Rule(inner),
		strings.TrimRight(f.Footer, "\n"),
	}
	return lipgloss.NewStyle().Padding(1, gutter).Render(strings.Join(rows, "\n"))
}

// EnvLine is the environment, as wide as the screen and as thin as a rule.
func (t *Theme) EnvLine(env EnvColor, width int) string {
	if width <= 0 {
		return ""
	}
	return lipgloss.NewStyle().Foreground(env.Swatch()).Render(strings.Repeat("▀", width))
}

// BodyHeight returns how many rows Chrome leaves for the body.
func BodyHeight(height int) int {
	if height < minChromeHeight {
		height = minChromeHeight
	}
	return max(height-chromePad, 1)
}

// FrameWidth returns the width the rules and the footer span.
func FrameWidth(width int) int {
	inner := width - 2*gutter
	if inner < minChromeWidth {
		return minChromeWidth
	}
	return inner
}

// TextWidth returns the width prose and tables should be rendered at.
func TextWidth(width int) int {
	inner := width - 2*gutter
	if inner < minChromeWidth {
		return minChromeWidth
	}
	if inner > MaxTextWidth {
		return MaxTextWidth
	}
	return inner
}

// Fit clips or pads content so that it occupies exactly the given number of rows.
func Fit(content string, lines int) string {
	if lines < 1 {
		lines = 1
	}
	return lipgloss.NewStyle().Height(lines).MaxHeight(lines).
		Render(strings.TrimRight(content, "\n"))
}

// Window returns the slice of content that starts at offset and fits in the
// given number of rows, along with how many rows are left below it.
func Window(content string, offset, lines int) (string, int) {
	if lines < 1 {
		lines = 1
	}
	rows := strings.Split(strings.TrimRight(content, "\n"), "\n")
	offset = clamp(offset, 0, maxOffset(len(rows), lines))
	end := offset + lines
	if end > len(rows) {
		end = len(rows)
	}
	return strings.Join(rows[offset:end], "\n"), len(rows) - end
}

// LineOf returns the row a marker sits on, which is how a list keeps its cursor
// on screen without knowing how it was drawn.
func LineOf(content, marker string) int {
	for i, row := range strings.Split(content, "\n") {
		if strings.Contains(row, marker) {
			return i
		}
	}
	return -1
}

// MaxOffset returns how far content can be scrolled inside an area.
func MaxOffset(content string, lines int) int {
	if lines < 1 {
		lines = 1
	}
	return maxOffset(len(strings.Split(strings.TrimRight(content, "\n"), "\n")), lines)
}

func maxOffset(rows, lines int) int {
	if rows <= lines {
		return 0
	}
	return rows - lines
}

func clamp(value, low, high int) int {
	switch {
	case value < low:
		return low
	case value > high:
		return high
	default:
		return value
	}
}

// Overlay draws a dialog centred on top of a full screen background.
func Overlay(background, dialog string, width, height int) string {
	return compose(background, dialog,
		(width-lipgloss.Width(dialog))/2,
		(height-lipgloss.Height(dialog))/2)
}

// Corner draws content in the bottom right of a full screen background, on the
// blank row Chrome keeps above its footer.
func Corner(background, content string, width, height int) string {
	return compose(background, content,
		width-lipgloss.Width(content)-gutter,
		height-lipgloss.Height(content)-3)
}

// At draws content over a background at an exact position.
func At(background, content string, x, y int) string {
	return compose(background, content, x, y)
}

// Gutter is the padding Chrome keeps on each side.
const Gutter = gutter

func compose(background, content string, x, y int) string {
	return lipgloss.NewCompositor(
		lipgloss.NewLayer(background),
		lipgloss.NewLayer(content).X(max(x, 0)).Y(max(y, 0)).Z(1),
	).Render()
}
