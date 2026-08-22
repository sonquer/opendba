package ui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func plain(s string) string { return ansi.Strip(s) }

func TestSeverityLabelsAndGlyphs(t *testing.T) {
	cases := []struct {
		severity Severity
		label    string
		glyph    string
	}{
		{SevOK, "ok", "✓"},
		{SevWarn, "warn", "⚠"},
		{SevCritical, "fail", "✗"},
		{SevInfo, "info", "·"},
		{SevInactive, "n/a", "○"},
	}
	for _, c := range cases {
		if got := c.severity.String(); got != c.label {
			t.Errorf("String() = %q, want %q", got, c.label)
		}
		if got := c.severity.Glyph(); got != c.glyph {
			t.Errorf("Glyph() = %q, want %q", got, c.glyph)
		}
	}
}

func TestEnvColours(t *testing.T) {
	if len(EnvColors) != 8 {
		t.Fatalf("EnvColors = %v", EnvColors)
	}
	seen := map[string]bool{}
	for _, env := range EnvColors {
		if !env.Valid() {
			t.Errorf("%s must be valid", env)
		}
		glyph := env.Glyph()
		if seen[glyph] {
			t.Errorf("%s reuses the glyph %q; environments must be distinguishable without colour", env, glyph)
		}
		seen[glyph] = true
		if env.Swatch() == nil {
			t.Errorf("%s has no colour", env)
		}
	}
}

func TestUnknownEnvColourFallsBackToGray(t *testing.T) {
	unknown := EnvColor("chartreuse")
	if unknown.Valid() {
		t.Error("an unknown colour must not be valid")
	}
	if unknown.Swatch() != EnvGray.Swatch() || unknown.Glyph() != EnvGray.Glyph() {
		t.Error("an unknown colour must not crash the interface")
	}
}

func TestThemeStatus(t *testing.T) {
	theme := Default()
	if got := plain(theme.Status(SevWarn)); got != "⚠ warn" {
		t.Errorf("Status() = %q", got)
	}
	for _, severity := range []Severity{SevOK, SevWarn, SevCritical, SevInfo, SevInactive} {
		if theme.Severity(severity).GetForeground() == nil {
			t.Errorf("%v has no colour", severity)
		}
	}
}

func TestThemeIsBuiltFromItsPalette(t *testing.T) {
	palette := DefaultPalette()
	theme := NewTheme(palette)
	if theme.P != palette {
		t.Error("the theme must keep its palette")
	}
	if theme.Title.GetForeground() != palette.Fg || !theme.Title.GetBold() {
		t.Error("the title style must come from the palette")
	}
}

func TestRule(t *testing.T) {
	theme := Default()
	if got := plain(theme.Rule(5)); got != "─────" {
		t.Errorf("Rule() = %q", got)
	}
	if theme.Rule(0) != "" || theme.Rule(-3) != "" {
		t.Error("a rule with no width renders nothing")
	}
}

func TestIdentityLine(t *testing.T) {
	got := plain(Default().IdentityLine(EnvRed, "production-eu", "postgres 16.3", "READ ONLY"))
	for _, want := range []string{EnvRed.Glyph(), "production-eu", "postgres 16.3", "READ ONLY"} {
		if !strings.Contains(got, want) {
			t.Errorf("the line must show %q: %q", want, got)
		}
	}
	if strings.Contains(got, "█") {
		t.Errorf("the environment is a dot, not a wall: %q", got)
	}
}

func TestIdentityLineWithoutServerOrMode(t *testing.T) {
	got := plain(Default().IdentityLine(EnvGreen, "local", "", ""))
	if strings.Contains(got, "·") {
		t.Errorf("missing fields must not leave separators behind: %q", got)
	}
}

func TestReadWriteIsCalledOut(t *testing.T) {
	theme := Default()
	quiet := theme.Mode(ReadOnlyLabel)
	loud := theme.Mode("READ / WRITE")
	if quiet == loud {
		t.Error("read write must not look like read only")
	}
	if plain(loud) != "READ / WRITE" {
		t.Errorf("Mode() = %q", plain(loud))
	}
}

func TestSection(t *testing.T) {
	got := plain(Default().Section("health", "2 ok", 40))
	if !strings.HasPrefix(got, "HEALTH") {
		t.Errorf("a section title is upper case: %q", got)
	}
	if !strings.HasSuffix(got, "2 ok") || lipgloss.Width(got) != 40 {
		t.Errorf("the tag sits at the far end: %q", got)
	}
	if plain(Default().Section("schema", "", 40)) != "SCHEMA" {
		t.Error("a section without a tag is just its title")
	}
}

func TestSplitLineKeepsAGap(t *testing.T) {
	got := plain(SplitLine("left", "right", 4))
	if got != "left  right" {
		t.Errorf("SplitLine() = %q", got)
	}
}

func TestFindingRow(t *testing.T) {
	got := plain(Default().Finding(SevWarn, "cache", 12, "94.0%", 8, "small cache"))
	for _, want := range []string{SevWarn.Glyph(), "cache", "94.0%", "small cache"} {
		if !strings.Contains(got, want) {
			t.Errorf("row missing %q: %q", want, got)
		}
	}
	if strings.Contains(got, "█") || strings.Contains(got, "░") {
		t.Errorf("findings are read, not measured: %q", got)
	}
	bare := plain(Default().Finding(SevOK, "integrity", 12, "", 0, ""))
	if strings.HasSuffix(bare, " ") {
		t.Errorf("a row without a value or note has no trailing space: %q", bare)
	}
}

func TestKV(t *testing.T) {
	got := plain(Default().KV("host", 8, "db.example.com"))
	if !strings.HasPrefix(got, "host") || !strings.HasSuffix(got, "db.example.com") {
		t.Errorf("KV() = %q", got)
	}
}

func TestBadge(t *testing.T) {
	if got := plain(Default().Badge("READ ONLY", SevOK)); got != "READ ONLY" {
		t.Errorf("Badge() = %q", got)
	}
}

func TestDotted(t *testing.T) {
	if got := Dotted("a", "", "  ", "b"); got != "a · b" {
		t.Errorf("Dotted() = %q", got)
	}
	if got := Dotted(); got != "" {
		t.Errorf("Dotted() = %q", got)
	}
}

func TestTruncateAndPad(t *testing.T) {
	cases := map[string]string{
		Truncate("abcdef", 10): "abcdef",
		Truncate("abcdef", 3):  "ab…",
		Truncate("abcdef", 1):  "…",
		Truncate("abcdef", 0):  "",
		Pad("ab", 4):           "ab  ",
		Pad("abcdef", 3):       "ab…",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
	if got := Truncate("日本語です", 4); lipgloss.Width(got) > 4 {
		t.Errorf("wide runes must respect the width: %q is %d columns", got, lipgloss.Width(got))
	}
	if got := Truncate("日本", 2); got != "…" {
		t.Errorf("a width that fits no wide rune leaves the ellipsis alone: %q", got)
	}
}

func TestThePaletteIsDarkOnPurpose(t *testing.T) {
	palette := DefaultPalette()
	if palette.Bg == nil || palette.Fg == nil {
		t.Fatal("the interface paints its own background and text")
	}
	r, g, b, _ := palette.Bg.RGBA()
	if r != 0 || g != 0 || b != 0 {
		t.Errorf("the background is black, got %v", palette.Bg)
	}
	if plain(Default().Base.Render("x")) != "x" {
		t.Error("the base style must not add characters")
	}
}
