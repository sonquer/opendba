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
	for _, want := range []string{"→ ~", "production-eu", "postgres 16.3", "READ ONLY"} {
		if !strings.Contains(got, want) {
			t.Errorf("the line must show %q: %q", want, got)
		}
	}
	if strings.Contains(got, "█") {
		t.Errorf("the environment is a line at the top, not a wall: %q", got)
	}
}

// column is where text starts on screen, which is not where it starts in the
// string once box drawing has spent three bytes per cell.
func column(row, text string) int {
	return lipgloss.Width(row[:strings.Index(row, text)])
}

func TestGauge(t *testing.T) {
	theme := Default()
	for _, c := range []struct {
		ratio  float64
		filled int
	}{{0, 0}, {0.5, 9}, {1, gaugeWidth}, {-1, 0}, {2, gaugeWidth}} {
		got := plain(theme.Gauge(c.ratio, SevOK))
		if strings.Count(got, "█") != c.filled {
			t.Errorf("Gauge(%v) = %q, want %d filled", c.ratio, got, c.filled)
		}
		if lipgloss.Width(got) != gaugeWidth+2 {
			t.Errorf("every gauge is the same width: %q", got)
		}
	}
	if lipgloss.Width(theme.Blank()) != gaugeWidth+2 {
		t.Error("a row without a measurement must still line up")
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

func TestReadings(t *testing.T) {
	readings := []Reading{
		{Severity: SevWarn, Label: "cache", Value: "94.0%", Note: "small cache", Ratio: 0.94, Measured: true},
		{Severity: SevOK, Label: "integrity", Value: "ok"},
	}
	got := plain(Default().Readings(readings, 60, Measure(readings)))
	for _, want := range []string{SevWarn.Glyph(), "cache", "94.0%", "small cache", "█", "░", "integrity"} {
		if !strings.Contains(got, want) {
			t.Errorf("the report is missing %q:\n%s", want, got)
		}
	}
	rows := strings.Split(got, "\n")
	if len(rows) != 2 {
		t.Fatalf("one row per reading, got %d", len(rows))
	}
	if column(rows[0], "94.0%") != column(rows[1], "ok") {
		t.Errorf("a reading without a bar must still line up:\n%s", got)
	}
	if strings.Contains(rows[1], "█") {
		t.Errorf("a word is not a proportion: %q", rows[1])
	}
	for _, row := range rows {
		if lipgloss.Width(row) > 60 {
			t.Errorf("a reading must fit its width: %q", row)
		}
	}

	// Two blocks measured together keep the same columns, which is what stops
	// the dashboard looking ragged between groups.
	wide := []Reading{{Severity: SevOK, Label: "replication", Value: "0 inactive"}}
	at := Measure(append(append([]Reading{}, readings...), wide...))
	first := plain(Default().Readings(readings, 60, at))
	second := plain(Default().Readings(wide, 60, at))
	if column(second, "0 inactive") != column(strings.Split(first, "\n")[0], "94.0%") {
		t.Errorf("blocks measured together must line up:\n%s\n%s", first, second)
	}
	if alone := plain(Default().Readings(readings, 60, Columns{})); alone == first {
		t.Error("a block measured on its own uses its own columns")
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
