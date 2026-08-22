package ui

import (
	"strings"

	"charm.land/lipgloss/v2"
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

func (t *Theme) IdentityLine(env EnvColor, name, server, mode string) string {
	line := t.Env(env) + " " + t.Title.Render(truncate(name, identityName))
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
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 2 {
		gap = 2
	}
	return left + strings.Repeat(" ", gap) + right
}

func (t *Theme) Finding(sev Severity, label string, labelWidth int, value string, valueWidth int, note string) string {
	row := "  " + t.Severity(sev).Render(sev.Glyph()) + "  "
	if value == "" && note == "" {
		return row + t.Value.Render(label)
	}
	row += t.Value.Render(pad(label, labelWidth)) + "  "
	if note == "" {
		return row + t.Label.Render(value)
	}
	return row + t.Label.Render(pad(value, valueWidth)) + "  " + t.Muted.Render(note)
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

func pad(s string, w int) string {
	s = truncate(s, w)
	if n := w - lipgloss.Width(s); n > 0 {
		return s + strings.Repeat(" ", n)
	}
	return s
}

func truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	if w == 1 {
		return "…"
	}
	runes := []rune(s)
	for i := len(runes); i > 0; i-- {
		candidate := string(runes[:i]) + "…"
		if lipgloss.Width(candidate) <= w {
			return candidate
		}
	}
	return "…"
}

func Truncate(s string, w int) string { return truncate(s, w) }

func Pad(s string, w int) string { return pad(s, w) }
