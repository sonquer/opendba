package ui

import (
	"charm.land/lipgloss/v2"
	"strings"
	"testing"
	"time"

	"github.com/sonquer/tui4db/src/cli/internal/driver"
	"github.com/sonquer/tui4db/src/cli/pkg/sqldialect"
	"github.com/sonquer/tui4db/src/cli/pkg/sqlguard"
)

func TestEnvAndConnectionLine(t *testing.T) {
	theme := Default()
	if got := plain(theme.Env(EnvRed)); got != EnvRed.Glyph() {
		t.Errorf("Env() = %q", got)
	}
	line := plain(theme.ConnectionLine("production-eu", EnvRed, "postgres 16.3", "READ ONLY"))
	for _, want := range []string{"production-eu", "postgres 16.3", "read only"} {
		if !strings.Contains(line, want) {
			t.Errorf("line missing %q: %q", want, line)
		}
	}
}

func TestSeverityMapping(t *testing.T) {
	theme := Default()
	cases := map[driver.Severity]Severity{
		driver.SeverityOK:       SevOK,
		driver.SeverityWarn:     SevWarn,
		driver.SeverityCritical: SevCritical,
		driver.SeverityInfo:     SevInfo,
		driver.SeverityUnknown:  SevInactive,
	}
	for from, want := range cases {
		if got := theme.Severity4Driver(from); got != want {
			t.Errorf("Severity4Driver(%v) = %v, want %v", from, got, want)
		}
	}
}

func TestFindingTable(t *testing.T) {
	theme := Default()
	findings := []driver.Finding{
		{Subsystem: "cache", Severity: driver.SeverityOK, Value: "99.2%"},
		{Subsystem: "locks", Severity: driver.SeverityCritical, Value: "3 waiting", Note: "blocked"},
	}
	out := plain(theme.FindingTable(findings))
	for _, want := range []string{"subsystem", "cache", "99.2%", "locks", "blocked", "✗ fail"} {
		if !strings.Contains(out, want) {
			t.Errorf("table missing %q:\n%s", want, out)
		}
	}
	if !strings.Contains(plain(theme.FindingTable(nil)), "nothing to report") {
		t.Error("an empty report must say so")
	}
}

func TestCounts(t *testing.T) {
	out := plain(Default().Counts(6, 1, 2))
	for _, want := range []string{"6 ok", "1 warning", "2 failing"} {
		if !strings.Contains(out, want) {
			t.Errorf("counts missing %q: %q", want, out)
		}
	}
}

func TestTableList(t *testing.T) {
	theme := Default()
	out := plain(theme.TableList([]driver.Table{
		{Schema: "public", Name: "users", Kind: "table", Rows: 1200000, Size: 8192},
	}))
	for _, want := range []string{"public.users", "table", "1,200,000", "8.0 KiB"} {
		if !strings.Contains(out, want) {
			t.Errorf("list missing %q:\n%s", want, out)
		}
	}
	if !strings.Contains(plain(theme.TableList(nil)), "no tables here") {
		t.Error("an empty schema must say so")
	}
}

func TestIndexList(t *testing.T) {
	theme := Default()
	out := plain(theme.IndexList([]driver.Index{
		{Table: "orders", Name: "orders_pkey", Size: -1, Scans: -1},
	}))
	for _, want := range []string{"orders_pkey", "orders", "n/a"} {
		if !strings.Contains(out, want) {
			t.Errorf("list missing %q:\n%s", want, out)
		}
	}
	if !strings.Contains(plain(theme.IndexList(nil)), "no indexes here") {
		t.Error("an empty schema must say so")
	}
}

func TestResultTable(t *testing.T) {
	theme := Default()
	out := plain(theme.ResultTable([]string{"id", "email"}, [][]string{{"1", "a@example.com"}}))
	for _, want := range []string{"id", "email", "a@example.com"} {
		if !strings.Contains(out, want) {
			t.Errorf("result missing %q:\n%s", want, out)
		}
	}
	if !strings.Contains(plain(theme.ResultTable(nil, nil)), "no columns") {
		t.Error("a result without columns must say so")
	}
}

func TestResultFooter(t *testing.T) {
	theme := Default()
	if got := plain(theme.ResultFooter(3, 42*time.Millisecond, false)); got != "3 rows · 42ms" {
		t.Errorf("footer = %q", got)
	}
	if got := plain(theme.ResultFooter(1000, time.Second, true)); !strings.Contains(got, "truncated") {
		t.Errorf("footer = %q", got)
	}
}

func TestVerdictRendering(t *testing.T) {
	theme := Default()
	guard := sqlguard.New(sqldialect.PostgreSQL())
	cases := map[string]sqlguard.Mode{
		"allowed":            sqlguard.ModeReadOnly,
		"needs confirmation": sqlguard.ModeReadWrite,
	}
	allowed := plain(theme.Verdict(guard.Classify("SELECT 1", cases["allowed"]), 0))
	if !strings.Contains(allowed, "allowed") || !strings.Contains(allowed, "SELECT") {
		t.Errorf("verdict = %q", allowed)
	}
	warned := plain(theme.Verdict(guard.Classify("DELETE FROM t", cases["needs confirmation"]), 0))
	if !strings.Contains(warned, "needs confirmation") {
		t.Errorf("verdict = %q", warned)
	}
	blocked := plain(theme.Verdict(guard.Classify("DELETE FROM t", sqlguard.ModeReadOnly), 0))
	if !strings.Contains(blocked, "blocked") || !strings.Contains(blocked, "READ ONLY") {
		t.Errorf("verdict = %q", blocked)
	}

	parse := guard.Classify("SELECT * FROM", sqlguard.ModeReadOnly)
	if strings.Contains(Reason(parse.Reason), "expecting") {
		t.Errorf("the list of hoped for tokens is noise: %q", Reason(parse.Reason))
	}
	narrow := plain(theme.Verdict(parse, 40))
	if lipgloss.Width(narrow) > 40 {
		t.Errorf("a verdict must fit the window: %q", narrow)
	}
	if !strings.Contains(narrow, "…") {
		t.Errorf("a clipped reason must be marked: %q", narrow)
	}
	bare := plain(theme.Verdict(sqlguard.Result{Verdict: sqlguard.Allow}, 0))
	if strings.Contains(bare, "·") {
		t.Errorf("a verdict without a reason has no separator: %q", bare)
	}
}

func TestCell(t *testing.T) {
	cases := map[string]any{
		"∅":     nil,
		"42":    42,
		"a b":   "a\nb",
		"bytes": []byte("bytes"),
	}
	for want, value := range cases {
		if got := Cell(value); got != want {
			t.Errorf("Cell(%v) = %q, want %q", value, got, want)
		}
	}
	long := Cell(strings.Repeat("x", 200))
	if len([]rune(long)) != maxCellWidth {
		t.Errorf("a long value must be cut to the cell width, got %d", len([]rune(long)))
	}
}

func TestStrings(t *testing.T) {
	got := Strings([]any{1, nil, "x"})
	if len(got) != 3 || got[1] != "∅" {
		t.Errorf("Strings() = %v", got)
	}
}

func TestCount(t *testing.T) {
	cases := map[int64]string{0: "0", 42: "42", 1200: "1,200", 1200000: "1,200,000", -1: "n/a"}
	for value, want := range cases {
		if got := Count(value); got != want {
			t.Errorf("Count(%d) = %q, want %q", value, got, want)
		}
	}
}

func TestByteSizeAndDuration(t *testing.T) {
	sizes := map[int64]string{-1: "n/a", 0: "0 B", 8192: "8.0 KiB", 46170898432: "43.0 GiB"}
	for value, want := range sizes {
		if got := ByteSize(value); got != want {
			t.Errorf("ByteSize(%d) = %q, want %q", value, got, want)
		}
	}
	durations := map[time.Duration]string{
		42 * time.Millisecond:   "42ms",
		1500 * time.Millisecond: "1.50s",
		90 * time.Second:        "1.5m",
	}
	for value, want := range durations {
		if got := Duration(value); got != want {
			t.Errorf("Duration(%v) = %q, want %q", value, got, want)
		}
	}
}
