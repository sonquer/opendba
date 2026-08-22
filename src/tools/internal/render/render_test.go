package render

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/sonquer/tui4db/src/tools/internal/core"
)

func plain(s string) string { return ansi.Strip(s) }

func TestDuration(t *testing.T) {
	cases := map[time.Duration]string{
		0:                       "",
		1500 * time.Millisecond: "1.5s",
		90 * time.Second:        "1.5m",
		250 * time.Millisecond:  "250ms",
	}
	for in, want := range cases {
		if got := Duration(in); got != want {
			t.Errorf("Duration(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestStatusGlyphIsColoured(t *testing.T) {
	theme := DefaultTheme()
	for _, status := range []core.Status{core.StatusPass, core.StatusFail, core.StatusSkip, core.StatusRunning, core.StatusPending} {
		rendered := theme.Status(status)
		if plain(rendered) != status.Glyph() {
			t.Errorf("Status(%v) = %q", status, plain(rendered))
		}
	}
}

func TestLine(t *testing.T) {
	theme := DefaultTheme()
	report := core.Report{Check: "cover:cli", Status: core.StatusPass, Summary: "98.2% of 400 statements", Duration: 2 * time.Second}
	got := plain(theme.Line(report, 12))
	if !strings.Contains(got, "✓") || !strings.Contains(got, "cover:cli") || !strings.Contains(got, "98.2%") || !strings.Contains(got, "2.0s") {
		t.Fatalf("Line() = %q", got)
	}
	bare := plain(theme.Line(core.Report{Check: "x", Status: core.StatusPending}, 1))
	if strings.TrimSpace(bare) != "· x" {
		t.Errorf("bare Line() = %q", bare)
	}
}

func TestDetailRendersRowsAndLines(t *testing.T) {
	theme := DefaultTheme()
	report := core.Report{
		Rows:   []core.Row{{Label: "internal/ui", Value: "97.0%", Note: "97/100", Status: core.StatusPass}},
		Detail: []string{"report: coverage/cli/html/index.html"},
	}
	got := plain(theme.Detail(report))
	for _, want := range []string{"internal/ui", "97.0%", "97/100", "index.html"} {
		if !strings.Contains(got, want) {
			t.Errorf("Detail() missing %q in %q", want, got)
		}
	}
	if theme.Detail(core.Report{}) != "" {
		t.Error("empty report must render nothing")
	}
}

func TestReportsIncludesVerdict(t *testing.T) {
	theme := DefaultTheme()
	reports := []core.Report{
		{Check: "comments", Status: core.StatusPass, Summary: "no comments found"},
		{Check: "cover:cli", Status: core.StatusFail, Detail: []string{"below gate"}},
		{Check: "lint:cli", Status: core.StatusSkip},
	}
	got := plain(theme.Reports(reports))
	for _, want := range []string{"comments", "cover:cli", "below gate", "checks failed", "1 passed · 1 failed · 1 skipped"} {
		if !strings.Contains(got, want) {
			t.Errorf("Reports() missing %q", want)
		}
	}
}

func TestVerdictPasses(t *testing.T) {
	got := plain(DefaultTheme().Verdict([]core.Report{{Status: core.StatusPass}}))
	if !strings.Contains(got, "all checks passed") {
		t.Errorf("Verdict() = %q", got)
	}
}

func TestPadKeepsLongValues(t *testing.T) {
	if got := pad("abcdef", 3); got != "abcdef" {
		t.Errorf("pad() = %q", got)
	}
	if got := pad("ab", 4); got != "ab  " {
		t.Errorf("pad() = %q", got)
	}
}
