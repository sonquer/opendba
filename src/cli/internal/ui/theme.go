package ui

import (
	"fmt"
	"image/color"

	"charm.land/lipgloss/v2"
)

type Palette struct {
	Bg       color.Color
	Fg       color.Color
	Muted    color.Color
	Subtle   color.Color
	Border   color.Color
	Accent   color.Color
	OK       color.Color
	Warn     color.Color
	Critical color.Color
	Info     color.Color
	Inactive color.Color
	OnEnv    color.Color

	Selection   color.Color
	OnSelection color.Color
	Keycap      color.Color
	Empty       color.Color
	Badge       color.Color
	OnBadge     color.Color

	BarOK       color.Color
	BarWarn     color.Color
	BarCritical color.Color
	BarInfo     color.Color
	BarInactive color.Color
}

// DefaultPalette is dark, and says so to the terminal rather than adapting to
// it. The interface paints its own background, so the colours on top of it
// cannot be chosen by whatever theme the terminal happens to carry.
func DefaultPalette() Palette {
	return Palette{
		Bg:       lipgloss.Color("#000000"),
		Fg:       lipgloss.Color("#dcdcdc"),
		Muted:    lipgloss.Color("#8a8a8a"),
		Subtle:   lipgloss.Color("#5c5c5c"),
		Border:   lipgloss.Color("#3a3a3a"),
		Accent:   lipgloss.Color("#a3e635"),
		OK:       lipgloss.Color("#8fce9b"),
		Warn:     lipgloss.Color("#e6c384"),
		Critical: lipgloss.Color("#ea8a94"),
		Info:     lipgloss.Color("#8aadf4"),
		Inactive: lipgloss.Color("#585858"),
		OnEnv:    lipgloss.Color("#101010"),

		Selection:   lipgloss.Color("#27272a"),
		OnSelection: lipgloss.Color("#fafafa"),
		Keycap:      lipgloss.Color("#3f3f46"),
		Empty:       lipgloss.Color("#2a2a2e"),
		Badge:       lipgloss.Color("#d78ba0"),
		OnBadge:     lipgloss.Color("#141014"),

		BarOK:       lipgloss.Color("#6ac396"),
		BarWarn:     lipgloss.Color("#e4b750"),
		BarCritical: lipgloss.Color("#f47d67"),
		BarInfo:     lipgloss.Color("#71c9fa"),
		BarInactive: lipgloss.Color("#6d7277"),
	}
}

type Severity int

const (
	SevOK Severity = iota
	SevWarn
	SevCritical
	SevInfo
	SevInactive
)

func (s Severity) String() string {
	switch s {
	case SevOK:
		return "ok"
	case SevWarn:
		return "warn"
	case SevCritical:
		return "fail"
	case SevInfo:
		return "info"
	default:
		return "n/a"
	}
}

func (s Severity) Glyph() string {
	switch s {
	case SevOK:
		return "✓"
	case SevWarn:
		return "⚠"
	case SevCritical:
		return "✗"
	case SevInfo:
		return "·"
	default:
		return "○"
	}
}

type EnvColor string

const (
	EnvRed    EnvColor = "red"
	EnvOrange EnvColor = "orange"
	EnvYellow EnvColor = "yellow"
	EnvGreen  EnvColor = "green"
	EnvCyan   EnvColor = "cyan"
	EnvBlue   EnvColor = "blue"
	EnvPurple EnvColor = "purple"
	EnvGray   EnvColor = "gray"
)

var EnvColors = []EnvColor{EnvRed, EnvOrange, EnvYellow, EnvGreen, EnvCyan, EnvBlue, EnvPurple, EnvGray}

var envSwatches = map[EnvColor]color.Color{
	EnvRed:    lipgloss.Color("#d43f4a"),
	EnvOrange: lipgloss.Color("#e07b39"),
	EnvYellow: lipgloss.Color("#d6ad2e"),
	EnvGreen:  lipgloss.Color("#3f9c58"),
	EnvCyan:   lipgloss.Color("#2fa5a5"),
	EnvBlue:   lipgloss.Color("#3f7fd4"),
	EnvPurple: lipgloss.Color("#8a63d2"),
	EnvGray:   lipgloss.Color("#6d6d6d"),
}

// envGlyphs separate at a glance rather than by shape family, with the heaviest
// marks on the colours people give to production.
var envGlyphs = map[EnvColor]string{
	EnvRed:    "⬢",
	EnvOrange: "⬣",
	EnvYellow: "◈",
	EnvGreen:  "⬤",
	EnvCyan:   "◉",
	EnvBlue:   "◍",
	EnvPurple: "✦",
	EnvGray:   "○",
}

func (e EnvColor) Swatch() color.Color {
	if c, ok := envSwatches[e]; ok {
		return c
	}
	return envSwatches[EnvGray]
}

func (e EnvColor) Glyph() string {
	if g, ok := envGlyphs[e]; ok {
		return g
	}
	return envGlyphs[EnvGray]
}

func (e EnvColor) Valid() bool {
	_, ok := envSwatches[e]
	return ok
}

type Theme struct {
	P Palette

	Base        lipgloss.Style
	Title       lipgloss.Style
	Muted       lipgloss.Style
	Subtle      lipgloss.Style
	Accent      lipgloss.Style
	Label       lipgloss.Style
	Value       lipgloss.Style
	Prompt      lipgloss.Style
	Selected    lipgloss.Style
	KeyHint     lipgloss.Style
	KeyCap      lipgloss.Style
	Separator   lipgloss.Style
	SectionHead lipgloss.Style
	Error       lipgloss.Style
	Divider     lipgloss.Style
	Empty       lipgloss.Style
	Selected2   lipgloss.Style
	KeycapStyle lipgloss.Style
	BadgeStyle  lipgloss.Style
	Panel       lipgloss.Style

	// Toast is the ground a passing sentence is drawn on. It has a background
	// of its own because it sits over whatever screen it interrupted, and text
	// with no ground under it reads as part of that screen rather than as
	// something the program just said. The ground is the one the keycaps use:
	// the selection colour is a shade off black and disappears on the black a
	// terminal usually is.
	Toast     lipgloss.Style
	TableHead lipgloss.Style

	// Tab and TabIdle are the two states of a tab, and TabKey is the cap the
	// key that reaches one is printed on. The tab being worked in is a block of
	// colour and the rest are not, which is the only way a terminal can draw
	// the front sheet of a stack without corners to draw it with.
	Tab     lipgloss.Style
	TabIdle lipgloss.Style
	TabKey  lipgloss.Style

	sql        sqlStyles
	severities map[Severity]lipgloss.Style
	bars       map[Severity]lipgloss.Style
	style      BarStyle
	markdown   *Markdown
}

// sqlStyles is the colour of each part of a statement in the editor.
type sqlStyles struct {
	plain       lipgloss.Style
	keyword     lipgloss.Style
	text        lipgloss.Style
	number      lipgloss.Style
	comment     lipgloss.Style
	punctuation lipgloss.Style
}

func NewTheme(p Palette) *Theme {
	theme := &Theme{
		P:           p,
		Base:        lipgloss.NewStyle().Foreground(p.Fg).Background(p.Bg),
		Title:       lipgloss.NewStyle().Foreground(p.Fg).Bold(true),
		Muted:       lipgloss.NewStyle().Foreground(p.Muted),
		Subtle:      lipgloss.NewStyle().Foreground(p.Subtle),
		Accent:      lipgloss.NewStyle().Foreground(p.Accent),
		Label:       lipgloss.NewStyle().Foreground(p.Muted),
		Value:       lipgloss.NewStyle().Foreground(p.Fg),
		Prompt:      lipgloss.NewStyle().Foreground(p.Accent).Bold(true),
		Selected:    lipgloss.NewStyle().Foreground(p.Accent).Bold(true),
		KeyHint:     lipgloss.NewStyle().Foreground(p.Muted),
		KeyCap:      lipgloss.NewStyle().Foreground(p.Accent),
		Separator:   lipgloss.NewStyle().Foreground(p.Border),
		SectionHead: lipgloss.NewStyle().Foreground(p.Muted),
		Error:       lipgloss.NewStyle().Foreground(p.Critical),
		Panel: lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).
			BorderForeground(p.Border).BorderBackground(p.Bg).Background(p.Bg).Padding(0, 1),
		TableHead:   lipgloss.NewStyle().Foreground(p.Muted),
		Divider:     lipgloss.NewStyle().Foreground(p.Border),
		Selected2:   lipgloss.NewStyle().Foreground(p.OnSelection).Background(p.Selection),
		KeycapStyle: lipgloss.NewStyle().Foreground(p.OnSelection).Background(p.Keycap).Padding(0, 1),
		Toast:       lipgloss.NewStyle().Foreground(p.OnSelection).Background(p.Keycap).Padding(1, 2),
		BadgeStyle: lipgloss.NewStyle().Foreground(p.OnBadge).Background(p.Badge).
			Bold(true).Padding(0, 1),
		Tab:     lipgloss.NewStyle().Foreground(p.Accent).Background(p.Selection).Bold(true),
		TabIdle: lipgloss.NewStyle().Foreground(p.Muted),
		TabKey:  lipgloss.NewStyle().Foreground(p.OnSelection).Background(p.Keycap),
	}
	theme.sql = sqlStyles{
		plain:       lipgloss.NewStyle().Foreground(p.Fg),
		keyword:     lipgloss.NewStyle().Foreground(p.Accent).Bold(true),
		text:        lipgloss.NewStyle().Foreground(p.OK),
		number:      lipgloss.NewStyle().Foreground(p.Warn),
		comment:     lipgloss.NewStyle().Foreground(p.Subtle).Italic(true),
		punctuation: lipgloss.NewStyle().Foreground(p.Muted),
	}
	theme.severities = map[Severity]lipgloss.Style{
		SevOK:       lipgloss.NewStyle().Foreground(p.OK),
		SevWarn:     lipgloss.NewStyle().Foreground(p.Warn),
		SevCritical: lipgloss.NewStyle().Foreground(p.Critical),
		SevInfo:     lipgloss.NewStyle().Foreground(p.Info),
		SevInactive: lipgloss.NewStyle().Foreground(p.Inactive),
	}
	theme.bars = map[Severity]lipgloss.Style{
		SevOK:       lipgloss.NewStyle().Foreground(p.BarOK),
		SevWarn:     lipgloss.NewStyle().Foreground(p.BarWarn),
		SevCritical: lipgloss.NewStyle().Foreground(p.BarCritical),
		SevInfo:     lipgloss.NewStyle().Foreground(p.BarInfo),
		SevInactive: lipgloss.NewStyle().Foreground(p.BarInactive),
	}
	theme.style = BarStyleNamed(DefaultBarStyle)
	return theme
}

// Bar is the colour a measurement is drawn in, whichever half of the scale it
// is. The glyph does the work: a solid block for what was measured and a weave
// for the rest. Darkening the empty half instead loses it against the page, and
// a background behind either half turns the scale into two slabs, because a
// cell with a background is a filled cell whatever is printed on it.
func (t *Theme) Bar(s Severity) lipgloss.Style {
	if style, ok := t.bars[s]; ok {
		return style
	}
	return t.bars[SevInactive]
}

func Default() *Theme { return NewTheme(DefaultPalette()) }

// hex renders a colour the way Glamour wants it, as a string.
func hex(c color.Color) string {
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8)
}

// Severity returns the style of a severity. The styles are built once, because
// this is called for every finding of every frame.
func (t *Theme) Severity(s Severity) lipgloss.Style {
	if style, ok := t.severities[s]; ok {
		return style
	}
	return t.severities[SevInactive]
}

func (t *Theme) Status(s Severity) string {
	return t.Severity(s).Render(s.Glyph() + " " + s.String())
}
