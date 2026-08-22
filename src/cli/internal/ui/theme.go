package ui

import (
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

var envGlyphs = map[EnvColor]string{
	EnvRed:    "●",
	EnvOrange: "◆",
	EnvYellow: "▲",
	EnvGreen:  "■",
	EnvCyan:   "◇",
	EnvBlue:   "▼",
	EnvPurple: "★",
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
	Panel       lipgloss.Style
	TableHead   lipgloss.Style
}

func NewTheme(p Palette) *Theme {
	return &Theme{
		P:           p,
		Base:        lipgloss.NewStyle().Foreground(p.Fg),
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
		SectionHead: lipgloss.NewStyle().Foreground(p.Info).Bold(true),
		Error:       lipgloss.NewStyle().Foreground(p.Critical),
		Panel:       lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(p.Border).Padding(0, 1),
		TableHead:   lipgloss.NewStyle().Foreground(p.Muted),
	}
}

func Default() *Theme { return NewTheme(DefaultPalette()) }

func (t *Theme) Severity(s Severity) lipgloss.Style {
	switch s {
	case SevOK:
		return lipgloss.NewStyle().Foreground(t.P.OK)
	case SevWarn:
		return lipgloss.NewStyle().Foreground(t.P.Warn)
	case SevCritical:
		return lipgloss.NewStyle().Foreground(t.P.Critical)
	case SevInfo:
		return lipgloss.NewStyle().Foreground(t.P.Info)
	default:
		return lipgloss.NewStyle().Foreground(t.P.Inactive)
	}
}

func (t *Theme) Status(s Severity) string {
	return t.Severity(s).Render(s.Glyph() + " " + s.String())
}
