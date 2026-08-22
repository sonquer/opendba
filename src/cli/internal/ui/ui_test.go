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

func TestGauge(t *testing.T) {
	for _, style := range BarStyles {
		theme := Default()
		theme.Bars(style.Name)
		for _, c := range []struct {
			ratio  float64
			filled int
		}{{0, 0}, {0.5, gaugeWidth / 2}, {1, gaugeWidth}, {-1, 0}, {2, gaugeWidth}} {
			got := plain(theme.Gauge(c.ratio, SevOK))
			if lipgloss.Width(got) != theme.BarWidth(gaugeWidth) {
				t.Errorf("%s: every gauge is the same width: %q", style.Name, got)
			}
			if style.Full == style.Empty {
				continue
			}
			if strings.Count(got, style.Full) != c.filled {
				t.Errorf("%s Gauge(%v) = %q, want %d filled", style.Name, c.ratio, got, c.filled)
			}
			if strings.Count(got, style.Empty) != gaugeWidth-c.filled {
				t.Errorf("%s Gauge(%v) = %q, want %d of track",
					style.Name, c.ratio, got, gaugeWidth-c.filled)
			}
		}
		if lipgloss.Width(theme.Blank()) != theme.BarWidth(gaugeWidth) {
			t.Errorf("%s: a row without a measurement must still line up", style.Name)
		}
	}
}

// A bar made of whole cells can only say one twentieth at a time. A style with
// parts says the eighths in between, which is the difference between a bar that
// reports 42% and one that rounds it to 40%.
func TestAGaugeWithPartsSaysWhatIsBetweenTheCells(t *testing.T) {
	theme := Default()
	theme.Bars("smooth")
	got := plain(theme.Gauge(0.42, SevOK))
	if !strings.HasPrefix(got, strings.Repeat("█", 8)+"▍") {
		t.Errorf("42%% of twenty cells is eight whole ones and a third of the ninth: %q", got)
	}
	if lipgloss.Width(got) != gaugeWidth {
		t.Errorf("the partial cell is one of the twenty: %q", got)
	}
	whole := Default()
	whole.Bars("shade")
	if strings.Contains(plain(whole.Gauge(0.42, SevOK)), "▍") {
		t.Error("a style with no parts draws whole cells only")
	}
}

func TestBarStyles(t *testing.T) {
	if BarStyleNamed("nothing of the sort").Name != DefaultBarStyle {
		t.Error("an unknown style falls back rather than failing to start")
	}
	if got := BarStyleNamed("ascii"); got.Full != "#" {
		t.Errorf("style = %+v", got)
	}
	names := BarStyleNames()
	if len(names) != len(BarStyles) || names[0] != DefaultBarStyle {
		t.Errorf("names = %v", names)
	}
	unset := Default()
	unset.style = BarStyle{}
	fallback := BarStyleNamed(DefaultBarStyle)
	want := fallback.Open + strings.Repeat(fallback.Full, 4) + fallback.Close
	if got := plain(unset.draw(1, SevOK, 4)); got != want {
		t.Errorf("a theme with no style chosen still draws: %q", got)
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
	if strings.TrimSpace(plain(loud)) != "READ / WRITE" {
		t.Errorf("Mode() = %q", plain(loud))
	}
	if !strings.HasPrefix(plain(quiet), " ") {
		t.Errorf("a badge is padded so it reads as a label: %q", plain(quiet))
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
	bar := BarStyleNamed(DefaultBarStyle)
	for _, want := range []string{"cache", "94.0%", "watch", "integrity", bar.Full, bar.Empty} {
		if !strings.Contains(got, want) {
			t.Errorf("the report is missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "small cache") {
		t.Errorf("the sentence belongs on the page, not the row:\n%s", got)
	}
	rows := strings.Split(got, "\n")
	if len(rows) != 2 {
		t.Fatalf("one row per reading, got %d", len(rows))
	}
	if strings.Contains(rows[1], "\u2588") || strings.Contains(rows[1], "·") {
		t.Errorf("a reading that is not a proportion has no bar: %q", rows[1])
	}
	if lipgloss.Width(rows[0]) < lipgloss.Width(rows[1]) {
		t.Errorf("a row without a bar still lines up:\n%s", got)
	}
	for _, row := range rows {
		if lipgloss.Width(row) > 60 {
			t.Errorf("a reading must fit its width: %q", row)
		}
	}

	marked := plain(Default().Readings([]Reading{{Label: "cache", Value: "1", Cursor: true}}, 40, Columns{}))
	if !strings.Contains(marked, "\u258c") {
		t.Errorf("the row under the cursor is marked: %q", marked)
	}

	wide := []Reading{{Severity: SevOK, Label: "replication", Value: "0 inactive"}}
	at := Measure(append(append([]Reading{}, readings...), wide...))
	first := plain(Default().Readings(readings, 60, at))
	second := plain(Default().Readings(wide, 60, at))
	if lipgloss.Width(strings.Split(first, "\n")[1]) != lipgloss.Width(second) {
		t.Errorf("blocks measured together must line up:\n%s\n%s", first, second)
	}
	if alone := plain(Default().Readings(readings, 60, Columns{})); alone == first {
		t.Error("a block measured on its own uses its own columns")
	}
}

func TestVerdictWords(t *testing.T) {
	words := map[Severity]string{
		SevOK: "ok", SevWarn: "watch", SevCritical: "act", SevInfo: "note", SevInactive: "n/a",
	}
	for severity, want := range words {
		if got := Verdict(severity); got != want {
			t.Errorf("Verdict(%v) = %q, want %q", severity, got, want)
		}
	}
}

func TestMeter(t *testing.T) {
	theme := Default()
	drawn := plain(theme.Meter(Reading{Severity: SevOK, Ratio: 0.5, Measured: true}, 40))
	bar := BarStyleNamed(DefaultBarStyle)
	if !strings.Contains(drawn, bar.Full) || !strings.Contains(drawn, bar.Empty) {
		t.Errorf("a meter is a bar with a track behind it: %q", drawn)
	}
	if !strings.Contains(drawn, "0%") || !strings.Contains(drawn, "100%") {
		t.Errorf("the scale must be named: %q", drawn)
	}
	for _, line := range strings.Split(drawn, "\n") {
		if lipgloss.Width(line) > 40 {
			t.Errorf("the meter must fit: %q", line)
		}
	}
	if counted := plain(theme.Meter(Reading{Value: "3"}, 40)); !strings.Contains(counted, "not a proportion") {
		t.Errorf("a count has no bar to draw: %q", counted)
	}
	if narrow := plain(theme.Meter(Reading{Ratio: 1, Measured: true}, 10)); !strings.Contains(narrow, bar.Full) {
		t.Errorf("a narrow meter still draws: %q", narrow)
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

func TestAStatementWithTabsIsMeasuredRight(t *testing.T) {
	drawn := Default().Statement("SELECT\n\tone,\n\ttwo\nFROM t", 60)
	for _, line := range strings.Split(drawn, "\n") {
		if strings.Contains(line, "\t") {
			t.Errorf("a tab is as wide as the terminal decides, not as wide as we count: %q", line)
		}
		if lipgloss.Width(line) > 60 {
			t.Errorf("no line may leave its panel: %q", line)
		}
	}
	if !strings.Contains(plain(drawn), "  1  SELECT") {
		t.Errorf("the first line of a statement is line one:\n%s", plain(drawn))
	}
}

func TestALongLineWrapsUnderItsOwnNumber(t *testing.T) {
	long := "SELECT " + strings.Repeat("column_with_a_long_name, ", 6) + "1"
	drawn := plain(Default().Statement("SELECT 1\n"+long, 50))
	lines := strings.Split(drawn, "\n")
	if len(lines) < 3 {
		t.Fatalf("the long line must wrap:\n%s", drawn)
	}
	if !strings.HasPrefix(lines[1], "  2  ") {
		t.Errorf("the second statement line is line two: %q", lines[1])
	}
	if strings.TrimSpace(lines[2]) == "" || strings.HasPrefix(strings.TrimSpace(lines[2]), "3") {
		t.Errorf("what wrapped keeps the number it wrapped from: %q", lines[2])
	}
}
