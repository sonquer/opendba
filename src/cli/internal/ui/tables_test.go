package ui

import (
	"fmt"
	"image/color"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/sonquer/opendba/src/cli/internal/driver"
	"github.com/sonquer/opendba/src/cli/pkg/sqldialect"
	"github.com/sonquer/opendba/src/cli/pkg/sqlguard"
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
	tables := []driver.Table{
		{Schema: "public", Name: "users", Kind: "table", Rows: 1200000, Size: 8192,
			Stats: true, IndexScans: 900, SeqScans: 100, LiveRows: 1180, DeadRows: 20, CacheHit: 0.98},
		{Schema: "public", Name: "fresh", Kind: "table", Rows: 0, Size: 8192},
	}
	out := plain(theme.TableList(tables, List{Cursor: 0, Sort: -1, Width: 110}))
	for _, want := range []string{
		"table", "rows", "size", "read from memory",
		"public.users", "1,200,000", "8.0 KiB", "98%",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("list missing %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "never read") {
		t.Errorf("a table with no counters says so rather than showing a wrong bar:\n%s", out)
	}
	rows := strings.Split(out, "\n")
	if len(rows) != 4 {
		t.Errorf("a heading, a rule and one row per table:\n%s", out)
	}
	painted := strings.Split(theme.TableList(tables, List{Cursor: 0, Sort: -1, Width: 110}), "\n")
	first := len(painted) - 2
	if !strings.Contains(painted[first], background(theme.P.Surface)) {
		t.Errorf("the row under the cursor is painted end to end: %q", painted[first])
	}
	if strings.Contains(painted[first+1], background(theme.P.Surface)) {
		t.Errorf("and only that row: %q", painted[first+1])
	}
	if lipgloss.Width(painted[first]) != 110 {
		t.Errorf("a painted row reaches the far side: %d", lipgloss.Width(painted[first]))
	}
	if !strings.Contains(plain(theme.TableList(nil, List{Cursor: 0, Sort: -1, Width: 110})), "no tables here") {
		t.Error("an empty schema must say so")
	}
}

func TestATableThatIsWalkedOrRottingIsFlagged(t *testing.T) {
	theme := Default()
	rotting := driver.Table{Schema: "public", Name: "queue", Size: 8192, Stats: true,
		IndexScans: 10, LiveRows: 100, DeadRows: 40, CacheHit: 0.99}
	if got := plain(theme.TableList([]driver.Table{rotting}, List{Cursor: -1, Sort: -1, Width: 110})); !strings.Contains(got, "act") {
		t.Errorf("two rows in five dead is worth acting on:\n%s", got)
	}
	cold := driver.Table{Schema: "public", Name: "logs", Size: 8192, Stats: true,
		IndexScans: 1, LiveRows: 10, CacheHit: 0.4}
	if got := plain(theme.TableList([]driver.Table{cold}, List{Cursor: -1, Sort: -1, Width: 110})); !strings.Contains(got, "watch") {
		t.Errorf("a table read from disk is worth watching:\n%s", got)
	}
}

func TestIndexList(t *testing.T) {
	theme := Default()
	indexes := []driver.Index{
		{Table: "orders", Name: "orders_pkey", Size: 16384, Scans: 90, Primary: true, Unique: true, Stats: true},
		{Table: "orders", Name: "orders_placed", Size: 8192, Scans: 0, Stats: true},
		{Table: "orders", Name: "orders_late", Size: -1, Scans: -1},
	}
	out := plain(theme.IndexList(indexes, List{Cursor: 0, Sort: -1, Width: 110}))
	for _, want := range []string{"orders_pkey", "orders", "primary", "idle", "act", "n/a"} {
		if !strings.Contains(out, want) {
			t.Errorf("list missing %q:\n%s", want, out)
		}
	}
	if !strings.Contains(plain(theme.IndexList(nil, List{Cursor: 0, Sort: -1, Width: 110})), "no indexes here") {
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
		90 * time.Second:        "1m30s",
		2 * time.Hour:           "120m00s",
	}
	for value, want := range durations {
		if got := Duration(value); got != want {
			t.Errorf("Duration(%v) = %q, want %q", value, got, want)
		}
	}
}

// A window with no room for a sentence keeps the numbers and the name, because
// those are what the list is for.
func TestANarrowListingDropsItsNotes(t *testing.T) {
	theme := Default()
	indexes := []driver.Index{
		{Table: "orders", Name: "orders_placed_at_idx", Size: 8192, Scans: 4, Stats: true},
	}
	wide := plain(theme.IndexList(indexes, List{Cursor: 0, Sort: -1, Width: 120}))
	narrow := plain(theme.IndexList(indexes, List{Cursor: 0, Sort: -1, Width: 64}))
	if !strings.Contains(wide, "used") {
		t.Errorf("a wide window has room to say why:\n%s", wide)
	}
	if strings.Contains(narrow, "used") {
		t.Errorf("a narrow window gives the sentence up first:\n%s", narrow)
	}
	if strings.Contains(narrow, "why") {
		t.Errorf("a dropped column loses its heading too:\n%s", narrow)
	}
	if !strings.Contains(narrow, "index") || !strings.Contains(narrow, "size") {
		t.Errorf("the columns that stay keep their names:\n%s", narrow)
	}
	if !strings.Contains(narrow, "orders_placed") {
		t.Errorf("the name survives:\n%s", narrow)
	}
	if lipgloss.Width(narrow) > 64 {
		t.Errorf("the row must fit the window: %d", lipgloss.Width(narrow))
	}
}

func TestADriverThatCountsNothingSaysSo(t *testing.T) {
	theme := Default()
	got := plain(theme.IndexList([]driver.Index{{Table: "t", Name: "t_idx"}}, List{Cursor: -1, Sort: -1, Width: 120}))
	for _, want := range []string{"unknown", "n/a"} {
		if !strings.Contains(got, want) {
			t.Errorf("list missing %q:\n%s", want, got)
		}
	}
}

// background is the escape a colour is set as, which is how a test can say a
// row was painted rather than merely marked.
func background(c color.Color) string {
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("48;2;%d;%d;%d", r>>8, g>>8, b>>8)
}

// A list says what it is in the order of, because a column of numbers nobody
// sorted and a column somebody sorted look the same.
func TestTheSortedColumnIsMarked(t *testing.T) {
	theme := Default()
	tables := []driver.Table{{Schema: "public", Name: "users", Size: 8192}}
	if got := plain(theme.TableList(tables, List{Cursor: -1, Sort: 2, Width: 110})); !strings.Contains(got, "size ↑") {
		t.Errorf("the sorted column must say so:\n%s", got)
	}
	down := plain(theme.TableList(tables, List{Cursor: -1, Sort: 2, Reversed: true, Width: 110}))
	if !strings.Contains(down, "size ↓") {
		t.Errorf("and which way round:\n%s", down)
	}
	if strings.Contains(down, "rows ↓") {
		t.Errorf("one column at a time:\n%s", down)
	}
	none := plain(theme.TableList(tables, List{Cursor: -1, Sort: -1, Width: 110}))
	if strings.Contains(none, "↑") || strings.Contains(none, "↓") {
		t.Errorf("a list nobody sorted says nothing:\n%s", none)
	}
}

// The bar on an index row is measured against the busiest index on its table,
// counted before any filter. Narrowed to one index, its own scans would be the
// busiest thing left and every bar would be full.
func TestABarSurvivesANarrowedList(t *testing.T) {
	theme := Default()
	all := []driver.Index{
		{Table: "orders", Name: "orders_pkey", Scans: 1000, Stats: true},
		{Table: "orders", Name: "orders_placed", Scans: 100, Stats: true},
	}
	theme.Bars("shade")
	fill := BarStyleNamed("shade").Full
	one := plain(theme.IndexList(all[1:], List{
		Cursor: -1, Sort: -1, Width: 120, Busiest: Busiest(all),
	}))
	if got := strings.Count(one, fill); got != 2 {
		t.Errorf("a tenth of the busiest index is a tenth of the bar, got %d cells:\n%s", got, one)
	}
	alone := plain(theme.IndexList(all[1:], List{Cursor: -1, Sort: -1, Width: 120}))
	if got := strings.Count(alone, fill); got != gaugeWidth {
		t.Errorf("without the count taken first, an index is the busiest thing left: %d", got)
	}
}
