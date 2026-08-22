package app

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/sonquer/tui4db/src/cli/internal/driver"
	"github.com/sonquer/tui4db/src/cli/internal/ui"
)

func TestAReadingExplainsItself(t *testing.T) {
	conn := healthy()
	conn.findings = []driver.Finding{{
		Group: driver.GroupMemory, Subsystem: "cache", Code: "cache_hit_ratio",
		Severity: driver.SeverityWarn, Value: "94.0%", Note: "small cache",
		Ratio: 0.94, Measured: true,
	}}
	m := loadedWith(t, conn, workspaceWith(t))
	m.width, m.height = 110, 32

	page, _ := press(t, m, "enter")
	if page.page == nil {
		t.Fatal("enter on a reading must open its page")
	}
	view := plain(page.content())
	for _, want := range []string{"cache", "94.0%", ui.BarStyleNamed(ui.DefaultBarStyle).Full, "0%", "100%", "WHAT THIS IS"} {
		if !strings.Contains(view, want) {
			t.Errorf("the page must show %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "##") {
		t.Errorf("the page is rendered, not printed:\n%s", view)
	}

	scrolled, _ := press(t, page, "down")
	if scrolled.page.offset != 1 {
		t.Errorf("offset = %d", scrolled.page.offset)
	}
	if back, _ := press(t, scrolled, "up"); back.page.offset != 0 {
		t.Error("up must scroll back")
	}
	closed, _ := press(t, page, "esc")
	if closed.page != nil {
		t.Error("esc must close the page")
	}
}

func TestAReadingWithoutAPageFallsBackToItsNote(t *testing.T) {
	conn := healthy()
	conn.findings = []driver.Finding{{
		Group: driver.GroupStorage, Subsystem: "journal", Code: "a check with no page",
		Severity: driver.SeverityInfo, Value: "delete", Note: "wal keeps readers out of the way",
	}}
	m := loadedWith(t, conn, workspaceWith(t))
	page, _ := press(t, m, "enter")
	view := plain(page.content())
	if !strings.Contains(view, "wal keeps readers") {
		t.Errorf("the driver note must stand in:\n%s", view)
	}
	if strings.Contains(view, "0%") {
		t.Error("a reading that is not a proportion has no bar")
	}
}

func TestASessionExplainsItself(t *testing.T) {
	m := watching(t, busy(), session)
	on, _ := press(t, m, "tab")
	page, _ := press(t, on, "enter")
	if page.page == nil {
		t.Fatal("enter on a session must open its page")
	}
	view := plain(page.content())
	for _, want := range []string{"session 40218", "api", "Lock", "UPDATE orders"} {
		if !strings.Contains(view, want) {
			t.Errorf("the page must show %q:\n%s", want, view)
		}
	}
}

func TestARecordIsReadableWithoutWalkingSideways(t *testing.T) {
	conn := healthy()
	conn.columns = []string{"id", "email", "created_at"}
	conn.rows = [][]any{{1, "ada@example.com", "2026-01-04"}}
	m := loadedWith(t, conn, workspaceWith(t))
	m.width, m.height = 110, 32
	editing, _ := press(t, m, "e")
	typed := typeInto(t, editing, "SELECT * FROM users")
	ran, cmd := press(t, typed, "ctrl+r")
	shown, _ := ran.Update(cmd())
	onResults, _ := press(t, shown.(Model), "tab")
	if onResults.focus != focusResults {
		t.Fatalf("focus = %v", onResults.focus)
	}
	page, _ := press(t, onResults, "enter")
	if page.page == nil {
		t.Fatal("enter on a row must open the record")
	}
	view := plain(page.content())
	for _, want := range []string{"row 1 of 1", "3 columns", "email", "ada@example.com", "SELECT * FROM users"} {
		if !strings.Contains(view, want) {
			t.Errorf("the record must show %q:\n%s", want, view)
		}
	}
}

func TestAWideResultWalksSideways(t *testing.T) {
	conn := healthy()
	conn.columns = nil
	row := []any{}
	for i := range 20 {
		conn.columns = append(conn.columns, "column_"+string(rune('a'+i)))
		row = append(row, "value")
	}
	conn.rows = [][]any{row}
	m := loadedWith(t, conn, workspaceWith(t))
	m.width, m.height = 90, 30
	editing, _ := press(t, m, "e")
	typed := typeInto(t, editing, "SELECT * FROM wide")
	ran, cmd := press(t, typed, "ctrl+r")
	shown, _ := ran.Update(cmd())
	onResults, _ := press(t, shown.(Model), "tab")

	view := plain(onResults.content())
	if !strings.Contains(view, "column 1 of 20") {
		t.Errorf("a result too wide to show must say where it is:\n%s", view)
	}
	right, _ := press(t, onResults, "right")
	if right.results.column != 1 {
		t.Fatalf("column = %d", right.results.column)
	}
	if !strings.Contains(plain(right.content()), "column 2 of 20") {
		t.Errorf("moving right must be visible:\n%s", plain(right.content()))
	}
	if shown, _ := right.results.window(); shown[0].Title != "column_b" {
		t.Errorf("the first column shown must be the one moved to: %+v", shown[0])
	}
	left, _ := press(t, right, "left")
	if left.results.column != 0 {
		t.Errorf("column = %d", left.results.column)
	}
	far := left
	for range 40 {
		far, _ = press(t, far, "right")
	}
	if far.results.column != 19 {
		t.Errorf("the last column is the end of the road, got %d", far.results.column)
	}
}

func TestTheEditorSplitsInHalfAndResizes(t *testing.T) {
	m := loadedWith(t, healthy(), workspaceWith(t))
	sized, _ := m.Update(tea.WindowSizeMsg{Width: 110, Height: 40})
	editing, _ := press(t, sized.(Model), "e")
	half := editing.editorRows()
	if half < 8 || half > ui.BodyHeight(40)/2+1 {
		t.Fatalf("the editor takes half the pane, got %d of %d", half, ui.BodyHeight(40))
	}

	taller, _ := press(t, editing, "ctrl+up")
	if taller.editorRows() != half+2 {
		t.Errorf("ctrl+up must give the editor room, got %d", taller.editorRows())
	}
	shorter, _ := press(t, taller, "ctrl+down")
	if shorter.editorRows() != half {
		t.Errorf("ctrl+down must take it back, got %d", shorter.editorRows())
	}
	tiny := shorter
	for range 40 {
		tiny, _ = press(t, tiny, "ctrl+down")
	}
	if tiny.editorRows() != minEditorRows {
		t.Errorf("the editor keeps a floor, got %d", tiny.editorRows())
	}
}

func TestTheStatementIsHighlightedOnceTheCursorLeaves(t *testing.T) {
	m := loadedWith(t, healthy(), workspaceWith(t))
	m.width, m.height = 110, 32
	editing, _ := press(t, m, "e")
	typed := typeInto(t, editing, "SELECT 1")
	if strings.Contains(plain(typed.statementView(80)), "SELECT 1") &&
		typed.statementView(80) == m.theme.Markdown(80).SQL("SELECT 1") {
		t.Error("the editor stays plain while it is being typed into")
	}
	blurred := typed
	blurred.focus = focusResults
	if blurred.statementView(80) != m.theme.Markdown(80).SQL("SELECT 1") {
		t.Error("a blurred editor shows the statement highlighted")
	}
	fresh := loadedWith(t, healthy(), workspaceWith(t))
	quiet, _ := press(t, fresh, "e")
	quiet.focus = focusResults
	if !strings.Contains(plain(quiet.statementView(80)), "SELECT ...") {
		t.Errorf("an empty editor has nothing to highlight: %q", plain(quiet.statementView(80)))
	}
}

func TestTheDashboardUsesTwoColumnsWhenThereIsRoom(t *testing.T) {
	conn := healthy()
	conn.findings = []driver.Finding{
		{Group: driver.GroupMemory, Subsystem: "cache", Code: "cache_hit_ratio",
			Severity: driver.SeverityOK, Value: "99%"},
		{Group: driver.GroupLoad, Subsystem: "waiting", Code: "waiting_locks",
			Severity: driver.SeverityCritical, Value: "3"},
		{Group: driver.GroupScans, Subsystem: "full scans", Code: "sequential_scans",
			Severity: driver.SeverityWarn, Value: "38%"},
	}
	m := loadedWith(t, conn, workspaceWith(t))

	m.width, m.height = 160, 40
	wide := plain(m.content())
	if !strings.Contains(wide, "MEMORY") || !strings.Contains(wide, "LOAD") {
		t.Fatalf("every group must be on screen:\n%s", wide)
	}
	side := ""
	for _, line := range strings.Split(wide, "\n") {
		if strings.Contains(line, "MEMORY") {
			side = line
		}
	}
	if !strings.Contains(side, "LOAD") {
		t.Errorf("a wide window puts two groups side by side: %q", side)
	}

	m.width, m.height = 90, 40
	narrow := plain(m.content())
	for _, line := range strings.Split(narrow, "\n") {
		if strings.Contains(line, "MEMORY") && strings.Contains(line, "LOAD") {
			t.Errorf("a narrow window stacks them: %q", line)
		}
	}
	for _, want := range []string{"ACT", "waiting · full scans", "1 to act", "1 to watch", "1 fine"} {
		if !strings.Contains(narrow, want) {
			t.Errorf("the headline must say %q:\n%s", want, narrow)
		}
	}
}

func TestADashboardWithoutFindingsSaysNothingIsThere(t *testing.T) {
	conn := healthy()
	conn.findings = nil
	m := loadedWith(t, conn, workspaceWith(t))
	view := plain(m.content())
	if !strings.Contains(view, "no health signals") {
		t.Errorf("content = %s", view)
	}
	if !strings.Contains(view, "nothing needs attention") {
		t.Errorf("an empty report is not a warning:\n%s", view)
	}
	if same, handled := m.walkReadings(keyMsg("down")); handled || same.reading != 0 {
		t.Error("there is nothing to walk")
	}
	if page, _ := press(t, m, "enter"); page.page != nil {
		t.Error("there is nothing to open")
	}
}

func TestAFindingWithoutAGroupStillShows(t *testing.T) {
	conn := healthy()
	conn.findings = []driver.Finding{{Subsystem: "orphan", Code: "orphan", Value: "1"}}
	m := loadedWith(t, conn, workspaceWith(t))
	view := plain(m.content())
	if !strings.Contains(view, "HEALTH") || !strings.Contains(view, "orphan") {
		t.Errorf("a finding with no group belongs under health:\n%s", view)
	}
}

// The headline is worst first, capped, and says how much is fine. A dashboard
// that only counts problems never tells you the other half.
func TestTheHeadline(t *testing.T) {
	cases := []struct {
		name     string
		findings []driver.Finding
		want     []string
		avoid    []string
	}{
		{
			name: "nothing wrong",
			findings: []driver.Finding{
				{Subsystem: "cache", Severity: driver.SeverityOK},
				{Subsystem: "size", Severity: driver.SeverityOK},
			},
			want: []string{"OK", "nothing needs attention", "2 checks, all of them fine"},
		},
		{
			name: "warnings only",
			findings: append(fine(11), []driver.Finding{
				{Subsystem: "idle indexes", Severity: driver.SeverityWarn},
				{Subsystem: "checkpoints", Severity: driver.SeverityWarn},
			}...),
			want:  []string{"WATCH", "idle indexes · checkpoints", "11 of 13 checks are fine"},
			avoid: []string{"ACT"},
		},
		{
			name: "worst first",
			findings: []driver.Finding{
				{Subsystem: "idle indexes", Severity: driver.SeverityWarn},
				{Subsystem: "wraparound", Severity: driver.SeverityCritical},
			},
			want: []string{"ACT", "wraparound · idle indexes", "1 to act", "1 to watch", "0 fine"},
		},
		{
			name:     "more than fits",
			findings: append(fine(1), warn("one", "two", "three", "four", "five")...),
			want:     []string{"WATCH", "one · two · three · +2 more"},
			avoid:    []string{"four"},
		},
		{
			name: "a role that cannot see",
			findings: []driver.Finding{
				{Subsystem: "cache", Severity: driver.SeverityOK},
				{Subsystem: "scans", Severity: driver.SeverityUnknown},
			},
			want: []string{"OK", "1 check, all of them fine", "1 reading the role cannot see"},
		},
		{
			name: "a reading that is a note rather than a check",
			findings: []driver.Finding{
				{Subsystem: "cache", Severity: driver.SeverityOK},
				{Subsystem: "journal", Severity: driver.SeverityInfo},
			},
			want:  []string{"OK", "2 checks, all of them fine"},
			avoid: []string{"cannot see"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			conn := healthy()
			conn.findings = c.findings
			m := loadedWith(t, conn, workspaceWith(t))
			m.width, m.height = 120, 40
			got := plain(m.verdict4Health(ui.FrameWidth(m.width)))
			for _, want := range c.want {
				if !strings.Contains(got, want) {
					t.Errorf("headline must say %q: %q", want, got)
				}
			}
			for _, avoid := range c.avoid {
				if strings.Contains(got, avoid) {
					t.Errorf("headline must not say %q: %q", avoid, got)
				}
			}
		})
	}
}

func fine(count int) []driver.Finding {
	out := make([]driver.Finding, 0, count)
	for i := range count {
		out = append(out, driver.Finding{
			Subsystem: fmt.Sprintf("check %d", i), Severity: driver.SeverityOK,
		})
	}
	return out
}

func warn(names ...string) []driver.Finding {
	out := make([]driver.Finding, 0, len(names))
	for _, name := range names {
		out = append(out, driver.Finding{Subsystem: name, Severity: driver.SeverityWarn})
	}
	return out
}

// Groups are not the same height, so they go in the shorter column rather than
// in alternate ones.
func TestTheColumnsBalance(t *testing.T) {
	left, right := balanced([]string{"a\nb\nc\nd\ne", "f", "g", "h"})
	if len(left) != 1 || len(right) != 3 {
		t.Errorf("left = %d blocks, right = %d", len(left), len(right))
	}
	if one, none := balanced([]string{"a"}); len(one) != 1 || len(none) != 0 {
		t.Errorf("a single block goes on the left: %d and %d", len(one), len(none))
	}
}

// Every finding the drivers can produce has a page. A reading with no page
// falls back to its one line note, which is the difference between a dashboard
// that explains and one that only measures.
func TestEveryFindingHasAPage(t *testing.T) {
	codes := []string{
		"cache_hit_ratio", "index_hit_ratio", "temp_files", "shared_buffers",
		"connections", "waiting_locks", "rollback_ratio", "long_running",
		"idle_in_transaction", "deadlocks", "transaction_read_only",
		"sequential_scans", "dead_tuples", "unused_indexes", "vacuum_age",
		"transaction_age", "forced_checkpoints", "wal_size", "inactive_slots",
		"database_size", "server_size", "unavailable",
		"integrity_check", "foreign_key_check", "freelist_count", "journal_mode", "query_only",
	}
	for _, code := range codes {
		page := explain(code)
		if page == "" {
			t.Errorf("%s has no page", code)
			continue
		}
		if !strings.Contains(page, "## What this is") {
			t.Errorf("%s must start by saying what the number is:\n%s", code, page)
		}
		if !strings.Contains(page, "## What to do") {
			t.Errorf("%s must end by saying what to do about it", code)
		}
	}
	if explain("no such check") != "" {
		t.Error("a code with no page has none")
	}
}
