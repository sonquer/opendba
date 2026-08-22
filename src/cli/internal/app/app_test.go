package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sonquer/tui4db/src/cli/internal/cli"
	"github.com/sonquer/tui4db/src/cli/internal/config"
	"github.com/sonquer/tui4db/src/cli/internal/driver"
	"github.com/sonquer/tui4db/src/cli/internal/ui"
	"github.com/sonquer/tui4db/src/cli/pkg/sqldialect"
	"github.com/sonquer/tui4db/src/cli/pkg/sqlguard"
)

type fakeConn struct {
	findings  []driver.Finding
	tables    []driver.Table
	indexes   []driver.Index
	rows      [][]any
	columns   []string
	failOn    string
	databases []driver.Database
	schemas   []driver.Schema
	fields    map[string][]driver.Column
	sessions  []driver.Session
	stopped   []string
}

func (f *fakeConn) fail(step string) error {
	if f.failOn == step {
		return errors.New(step + " failed")
	}
	return nil
}

func (f *fakeConn) Info(context.Context) (driver.ServerInfo, error) {
	return driver.ServerInfo{Driver: "sqlite", Version: "3.45"}, f.fail("info")
}

func (f *fakeConn) Sessions(context.Context) ([]driver.Session, error) {
	return f.sessions, f.fail("sessions")
}

func (f *fakeConn) Stop(_ context.Context, id string, terminate bool) error {
	if err := f.fail("stop"); err != nil {
		return err
	}
	verb := "cancelled"
	if terminate {
		verb = "terminated"
	}
	f.stopped = append(f.stopped, verb+" "+id)
	return nil
}

func (f *fakeConn) Databases(context.Context) ([]driver.Database, error) {
	return f.databases, f.fail("databases")
}

func (f *fakeConn) Schemas(context.Context) ([]driver.Schema, error) {
	return f.schemas, f.fail("schemas")
}

func (f *fakeConn) Tables(context.Context, string) ([]driver.Table, error) {
	return f.tables, f.fail("tables")
}

func (f *fakeConn) Columns(_ context.Context, _ string, table string) ([]driver.Column, error) {
	return f.fields[table], f.fail("columns")
}

func (f *fakeConn) Relations(context.Context, string, string) ([]driver.Relation, error) {
	return nil, f.fail("relations")
}

func (f *fakeConn) Indexes(context.Context, string) ([]driver.Index, error) {
	return f.indexes, f.fail("indexes")
}

func (f *fakeConn) Query(context.Context, string) (driver.ResultSet, error) {
	if err := f.fail("query"); err != nil {
		return nil, err
	}
	return &fakeResult{columns: f.columns, rows: f.rows}, nil
}

func (f *fakeConn) Explain(context.Context, string, bool) (driver.Plan, error) {
	return driver.Plan{}, f.fail("explain")
}

func (f *fakeConn) Health(context.Context) ([]driver.Finding, error) {
	return f.findings, f.fail("health")
}

func (f *fakeConn) Close() error { return nil }

type fakeResult struct {
	columns []string
	rows    [][]any
	index   int
}

func (r *fakeResult) Columns() []string { return r.columns }

func (r *fakeResult) Next() bool {
	if r.index >= len(r.rows) {
		return false
	}
	r.index++
	return true
}

func (r *fakeResult) Values() []any { return r.rows[r.index-1] }

func (r *fakeResult) Err() error { return nil }

func (r *fakeResult) Truncated() bool { return false }

func (r *fakeResult) Duration() time.Duration { return 12 * time.Millisecond }

func (r *fakeResult) Close() error { return nil }

func session(conn driver.Conn) cli.Session {
	return cli.Session{
		Connection: config.Connection{Name: "production-eu", Driver: "sqlite", Mode: config.ReadOnly, Color: "red"},
		Settings:   config.DefaultSettings(),
		Conn:       conn,
		Info:       driver.ServerInfo{Driver: "sqlite", Version: "3.45"},
		Guard:      sqlguard.New(sqldialect.SQLite()),
		Theme:      ui.Default(),
	}
}

func crowded(findings int) *fakeConn {
	conn := healthy()
	conn.findings = nil
	for i := range findings {
		conn.findings = append(conn.findings, driver.Finding{
			Group:     driver.GroupLoad,
			Subsystem: fmt.Sprintf("subsystem %d", i),
			Code:      "check",
			Severity:  driver.SeverityOK,
			Value:     "ok",
		})
	}
	return conn
}

func healthy() *fakeConn {
	return &fakeConn{
		findings: []driver.Finding{
			{Group: driver.GroupStorage, Subsystem: "integrity", Code: "integrity_check",
				Severity: driver.SeverityOK, Value: "ok"},
			{Group: driver.GroupMemory, Subsystem: "cache", Code: "cache", Severity: driver.SeverityWarn,
				Value: "94.0%", Note: "small", Ratio: 0.94, Measured: true},
		},
		tables:  []driver.Table{{Schema: "main", Name: "users", Kind: "table", Rows: 2, Size: 4096}},
		indexes: []driver.Index{{Schema: "main", Table: "users", Name: "users_pkey", Size: 2048}},
		columns: []string{"email"},
		rows:    [][]any{{"a@example.com"}, {"b@example.com"}},
		databases: []driver.Database{
			{Name: "app", Current: true, Comment: "the one in use"},
			{Name: "reporting"},
		},
		schemas: []driver.Schema{
			{Name: "public", Tables: 1},
			{Name: "pg_catalog", Tables: 62, System: true},
		},
	}
}

func plain(s string) string { return ansi.Strip(s) }

func press(t *testing.T, m Model, key string) (Model, tea.Cmd) {
	t.Helper()
	updated, cmd := m.Update(keyMsg(key))
	return updated.(Model), cmd
}

func firstRune(s string) rune {
	for _, r := range s {
		return r
	}
	return 0
}

func keyMsg(name string) tea.KeyPressMsg {
	named := map[string]rune{
		"enter":     tea.KeyEnter,
		"esc":       tea.KeyEscape,
		"tab":       tea.KeyTab,
		"backspace": tea.KeyBackspace,
		"up":        tea.KeyUp,
		"down":      tea.KeyDown,
		"left":      tea.KeyLeft,
		"right":     tea.KeyRight,
		"home":      tea.KeyHome,
		"end":       tea.KeyEnd,
		"pgup":      tea.KeyPgUp,
		"pgdown":    tea.KeyPgDown,
		"f5":        tea.KeyF5,
		"space":     tea.KeySpace,
	}
	modifiers := map[string]tea.KeyMod{
		"ctrl":  tea.ModCtrl,
		"alt":   tea.ModAlt,
		"shift": tea.ModShift,
		"super": tea.ModSuper,
	}
	parts := strings.Split(name, "+")
	msg := tea.KeyPressMsg{}
	for _, part := range parts[:len(parts)-1] {
		msg.Mod |= modifiers[part]
	}
	last := parts[len(parts)-1]
	if code, ok := named[last]; ok {
		msg.Code = code
		if last == "space" {
			msg.Text = " "
		}
		return msg
	}
	msg.Code = firstRune(last)
	if msg.Mod == 0 {
		msg.Text = last
	}
	return msg
}

func loaded(t *testing.T, conn *fakeConn) Model {
	t.Helper()
	return loadedWith(t, conn, workspaceWith(t))
}

func loadedWith(t *testing.T, conn *fakeConn, workspace cli.Workspace) Model {
	t.Helper()
	m := NewModel(session(conn), workspace)
	if m.Init() == nil {
		t.Fatal("the model must load on start")
	}
	updated, _ := m.Update(m.load()())
	return updated.(Model)
}

// runAll walks a batch and hands every message back, which is what a program
// would do with them.
func runAll(t *testing.T, cmd tea.Cmd) []tea.Msg {
	t.Helper()
	if cmd == nil {
		return nil
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		return []tea.Msg{msg}
	}
	var produced []tea.Msg
	for _, sub := range batch {
		if sub == nil {
			continue
		}
		if out := sub(); out != nil {
			produced = append(produced, out)
		}
	}
	return produced
}

func runFirst(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		t.Fatal("a command was expected")
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		return msg
	}
	for _, sub := range batch {
		if sub == nil {
			continue
		}
		produced := sub()
		if _, tick := produced.(spinner.TickMsg); tick || produced == nil {
			continue
		}
		return produced
	}
	t.Fatal("the batch produced nothing")
	return nil
}

func typeInto(t *testing.T, m Model, text string) Model {
	t.Helper()
	for _, key := range strings.Split(text, "") {
		if key == " " {
			m, _ = press(t, m, "space")
			continue
		}
		m, _ = press(t, m, key)
	}
	return m
}

func TestLoadsEverythingOnStart(t *testing.T) {
	m := loaded(t, healthy())
	if m.loading {
		t.Error("loading must finish")
	}
	if len(m.findings) != 2 || len(m.tables) != 1 || len(m.indexes) != 1 {
		t.Fatalf("model = %+v", m)
	}
	content := plain(m.content())
	for _, want := range []string{"production-eu", "sqlite 3.45", "READ ONLY", "integrity", "1 table", "1 index"} {
		if !strings.Contains(content, want) {
			t.Errorf("dashboard missing %q:\n%s", want, content)
		}
	}
}

func TestLoadFailuresAreShown(t *testing.T) {
	for _, step := range []string{"health", "tables", "indexes"} {
		t.Run(step, func(t *testing.T) {
			conn := healthy()
			conn.failOn = step
			m := loaded(t, conn)
			if m.failure == "" {
				t.Fatal("the failure must be kept")
			}
			if !strings.Contains(plain(m.content()), step+" failed") {
				t.Errorf("content = %s", plain(m.content()))
			}
		})
	}
}

func TestViewSwitching(t *testing.T) {
	m := loaded(t, healthy())
	cases := map[string]view{"s": viewSchema, "i": viewIndexes, "e": viewQuery, "?": viewHelp}
	for key, want := range cases {
		switched, _ := press(t, m, key)
		if switched.view != want {
			t.Errorf("%q switched to %v, want %v", key, switched.view, want)
		}
	}
	if !strings.Contains(plain(m.content()), "integrity") {
		t.Error("the dashboard is the health report")
	}
	schema, _ := press(t, m, "s")
	if !strings.Contains(plain(schema.content()), "users") {
		t.Error("the schema view must list the tables")
	}
	help, _ := press(t, m, "?")
	shown := plain(help.content())
	if !strings.Contains(shown, "query") || !strings.Contains(shown, "run") {
		t.Errorf("the help view must list the keys:\n%s", shown)
	}
	if !strings.Contains(shown, "never") || !strings.Contains(shown, "SAFETY") {
		t.Errorf("the help view must state the safety rule:\n%s", shown)
	}
	if strings.Contains(shown, "##") || strings.Contains(shown, "**") {
		t.Errorf("the help view is rendered, not printed:\n%s", shown)
	}
	back, _ := press(t, help, "esc")
	if back.view != viewDashboard {
		t.Errorf("escape must return to the dashboard, got %v", back.view)
	}
}

func TestQuittingAsksFirst(t *testing.T) {
	for _, key := range []string{"q", "ctrl+c"} {
		m := loaded(t, healthy())
		asked, cmd := press(t, m, key)
		if cmd != nil || asked.quitting {
			t.Fatalf("%q must ask before it leaves", key)
		}
		if asked.modal == nil {
			t.Fatalf("%q must raise the question", key)
		}
		if !strings.Contains(plain(asked.content()), "close tui4db?") {
			t.Errorf("content = %s", plain(asked.content()))
		}

		left, cmd := press(t, asked, "enter")
		if cmd == nil || left.modal != nil {
			t.Fatal("enter must answer the question")
		}
		quit, _ := left.Update(cmd())
		if !quit.(Model).quitting {
			t.Error("answering yes must leave")
		}

		stayed, cmd := press(t, asked, "esc")
		if cmd != nil || stayed.modal != nil || stayed.quitting {
			t.Error("esc must put the question away and stay")
		}
	}
}

func TestASecondCtrlCLeavesAtOnce(t *testing.T) {
	asked, _ := press(t, loaded(t, healthy()), "ctrl+c")
	quit, cmd := press(t, asked, "ctrl+c")
	if cmd == nil || !quit.quitting {
		t.Error("ctrl+c on the question must leave at once")
	}
}

func TestTheEditorAlsoAsks(t *testing.T) {
	editing, _ := press(t, loaded(t, healthy()), "e")
	asked, cmd := press(t, editing, "ctrl+c")
	if cmd != nil || asked.modal == nil {
		t.Error("the editor must ask too")
	}
}

func TestReload(t *testing.T) {
	m := loaded(t, healthy())
	reloading, cmd := press(t, m, "r")
	if cmd == nil || !reloading.loading {
		t.Fatal("r must read everything again")
	}
}

func TestScrollingIsBounded(t *testing.T) {
	m := loaded(t, crowded(20))
	m.height = 12
	inspect := m
	if up, _ := press(t, inspect, "up"); up.offset != 0 {
		t.Errorf("offset = %d", up.offset)
	}
	down, _ := press(t, inspect, "down")
	if down.offset != 1 {
		t.Errorf("offset = %d", down.offset)
	}
	far := down
	for range 200 {
		moved, _ := press(t, far, "down")
		if moved.offset == far.offset {
			break
		}
		far = moved
	}
	limit := ui.MaxOffset(far.body(), ui.BodyHeight(far.height))
	if far.offset != limit {
		t.Errorf("offset = %d, want the last row at %d", far.offset, limit)
	}
	page, _ := press(t, inspect, "pgdown")
	if page.offset == 0 {
		t.Error("pgdown must move a whole page")
	}
	if back, _ := press(t, page, "pgup"); back.offset != 0 {
		t.Errorf("pgup must come back to the top, got %d", back.offset)
	}
	if unknown, _ := press(t, inspect, "x"); unknown.offset != 0 {
		t.Error("an unknown key scrolls nothing")
	}
	dashboard, _ := press(t, inspect, "esc")
	if dashboard.offset != 0 {
		t.Error("leaving a view goes back to the top")
	}
}

func TestTheBodyIsClippedToTheWindow(t *testing.T) {
	m := loaded(t, crowded(20))
	m.width, m.height = 90, 14
	view := plain(m.content())
	if lines := len(strings.Split(view, "\n")); lines != 14 {
		t.Errorf("the screen must fill the window exactly, got %d rows", lines)
	}
	if !strings.Contains(view, "more") {
		t.Errorf("clipped content must say how much is left:\n%s", view)
	}
}

func TestWindowSize(t *testing.T) {
	m := loaded(t, healthy())
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	resized := updated.(Model)
	if resized.width != 120 || resized.height != 40 {
		t.Fatalf("size = %dx%d", resized.width, resized.height)
	}
	screen := plain(resized.content())
	if lines := len(strings.Split(screen, "\n")); lines != 40 {
		t.Errorf("the screen must fill the window, got %d rows", lines)
	}
	if strings.Contains(screen, "████████████████████████████") {
		t.Error("the environment is a line at the top, not a wall")
	}
	rows := strings.Split(screen, "\n")
	if strings.TrimSpace(rows[0]) != "" {
		t.Errorf("the frame breathes at the top: %q", rows[0])
	}
	if !strings.Contains(rows[1], "▔") {
		t.Errorf("the environment is a line across the top: %q", rows[1])
	}
	if !strings.HasPrefix(rows[2], "  → ~ production-eu") {
		t.Errorf("the identity line comes under it: %q", rows[2])
	}
	if resized.editor.Width() > resized.paneWidth() || resized.editor.Width() < resized.paneWidth()-8 {
		t.Errorf("the editor must fill its pane, got %d of %d", resized.editor.Width(), resized.paneWidth())
	}
	if resized.editor.Height() != editorRows(40) {
		t.Errorf("the editor must follow the height, got %d", resized.editor.Height())
	}
}

func TestUnknownMessagesAndKeysAreInert(t *testing.T) {
	m := loaded(t, healthy())
	updated, cmd := m.Update(struct{}{})
	if cmd != nil || updated.(Model).view != viewDashboard {
		t.Error("unknown messages must do nothing")
	}
	same, cmd := press(t, m, "z")
	if cmd != nil || same.view != viewDashboard {
		t.Error("unbound keys must do nothing")
	}
}

func TestQueryEditorRunsAllowedStatements(t *testing.T) {
	m := loaded(t, healthy())
	editing, _ := press(t, m, "e")
	editing = typeInto(t, editing, "SELECT 1")
	if editing.statement() != "SELECT 1" {
		t.Fatalf("editor = %q", editing.statement())
	}
	verdict := editing.Verdict()
	if !verdict.Allowed() {
		t.Fatalf("verdict = %+v", verdict)
	}
	ran, cmd := press(t, editing, "f5")
	if cmd == nil {
		t.Fatal("f5 must run the statement")
	}
	finished, _ := ran.Update(cmd())
	content := plain(finished.(Model).content())
	for _, want := range []string{"a@example.com", "2 rows", "allowed"} {
		if !strings.Contains(content, want) {
			t.Errorf("content missing %q:\n%s", want, content)
		}
	}
}

func TestQueryEditorRefusesToRunAWrite(t *testing.T) {
	m := loaded(t, healthy())
	editing, _ := press(t, m, "e")
	editing = typeInto(t, editing, "DELETE FROM users")
	if editing.Verdict().Allowed() {
		t.Fatalf("verdict = %+v", editing.Verdict())
	}
	_, cmd := press(t, editing, "f5")
	if cmd != nil {
		t.Fatal("a blocked statement must not run")
	}
	if !strings.Contains(plain(editing.content()), "blocked") {
		t.Errorf("content = %s", plain(editing.content()))
	}
}

func TestQueryFailuresAreShown(t *testing.T) {
	conn := healthy()
	m := loaded(t, conn)
	conn.failOn = "query"
	editing, _ := press(t, m, "e")
	editing = typeInto(t, editing, "SELECT 1")
	ran, cmd := press(t, editing, "f5")
	if cmd == nil {
		t.Fatal("the statement must run")
	}
	finished, _ := ran.Update(cmd())
	if !strings.Contains(plain(finished.(Model).content()), "query failed") {
		t.Errorf("content = %s", plain(finished.(Model).content()))
	}
}

func TestEmptyEditorIsBlocked(t *testing.T) {
	m := loaded(t, healthy())
	editing, _ := press(t, m, "e")
	if editing.Verdict().Allowed() {
		t.Error("an empty editor has nothing to run")
	}
	if !strings.Contains(plain(editing.content()), "nothing has run yet") {
		t.Errorf("content = %s", plain(editing.content()))
	}
}

func TestViewCarriesTheWindowTitle(t *testing.T) {
	m := loaded(t, healthy())
	v := m.View()
	if !v.AltScreen || !strings.Contains(v.WindowTitle, "production-eu") {
		t.Fatalf("view = %+v", v)
	}
}

func TestDashboardWithoutFindings(t *testing.T) {
	conn := healthy()
	conn.findings = nil
	m := loaded(t, conn)
	if !strings.Contains(plain(m.content()), "no health signals") {
		t.Errorf("content = %s", plain(m.content()))
	}
}

func TestLaunchStopsOnQuit(t *testing.T) {
	if err := launch(session(healthy()), workspaceWith(t),
		tea.WithInput(strings.NewReader("q\r")),
		tea.WithOutput(io.Discard)); err != nil {
		t.Fatalf("launch: %v", err)
	}
}

func TestIndexesHaveTheirOwnView(t *testing.T) {
	m := loaded(t, healthy())
	indexes, _ := press(t, m, "i")
	if indexes.view != viewIndexes {
		t.Fatalf("view = %v", indexes.view)
	}
	if !strings.Contains(plain(indexes.content()), "users_pkey") {
		t.Errorf("content = %s", plain(indexes.content()))
	}
	schema, _ := press(t, m, "s")
	if !strings.Contains(plain(schema.content()), "main.users") {
		t.Errorf("content = %s", plain(schema.content()))
	}
}

func TestQueryEditorIgnoresUnknownKeysAndKeepsTyping(t *testing.T) {
	m := loaded(t, healthy())
	editing, _ := press(t, m, "e")
	editing, _ = press(t, editing, "ctrl+r")
	if editing.view != viewQuery {
		t.Fatal("ctrl+r on an empty editor must not leave the view")
	}
	editing = typeInto(t, editing, "SELECT 1")
	ran, cmd := press(t, editing, "ctrl+r")
	if cmd == nil {
		t.Fatal("ctrl+r must run the statement too")
	}
	if ran.view != viewQuery {
		t.Errorf("view = %v", ran.view)
	}
}

func TestEditorEscapeReturnsToTheDashboard(t *testing.T) {
	m := loaded(t, healthy())
	editing, _ := press(t, m, "e")
	back, _ := press(t, editing, "esc")
	if back.view != viewDashboard {
		t.Errorf("escape must leave the editor, got %v", back.view)
	}
}

func TestFocusWalksTheWorkbench(t *testing.T) {
	m := loaded(t, healthy())
	editing, _ := press(t, m, "e")
	editing.sidebar.hidden = true

	noResults, _ := press(t, editing, "tab")
	if noResults.focus != focusEditor {
		t.Fatal("without results or a sidebar there is nowhere to move")
	}

	editing = typeInto(t, editing, "SELECT 1")
	ran, cmd := press(t, editing, "f5")
	withResults, _ := ran.Update(cmd())
	shown := withResults.(Model)
	if shown.focus != focusEditor {
		t.Fatal("a fresh result must leave the editor focused")
	}

	onResults, _ := press(t, shown, "tab")
	if onResults.focus != focusResults {
		t.Fatal("tab must move to the results")
	}

	scrolled, _ := press(t, onResults, "down")
	if scrolled.statement() != "SELECT 1" {
		t.Error("scrolling the results must not type into the editor")
	}

	back, _ := press(t, onResults, "tab")
	if back.focus != focusEditor {
		t.Fatal("tab must come back to the editor")
	}
	typed := typeInto(t, back, "2")
	if typed.statement() != "SELECT 12" {
		t.Errorf("the editor must accept input again: %q", typed.statement())
	}
}

func TestAFailedResultCannotBeFocused(t *testing.T) {
	conn := healthy()
	m := loaded(t, conn)
	conn.failOn = "query"
	editing, _ := press(t, m, "e")
	editing = typeInto(t, editing, "SELECT 1")
	ran, cmd := press(t, editing, "f5")
	failed, _ := ran.Update(cmd())

	broken := failed.(Model)
	broken.sidebar.hidden = true
	focused, _ := press(t, broken, "tab")
	if focused.focus == focusResults {
		t.Error("there is nothing to scroll when the query failed")
	}
}

func TestAnEmptyResultSaysSo(t *testing.T) {
	conn := healthy()
	conn.rows = nil
	m := loaded(t, conn)
	editing, _ := press(t, m, "e")
	editing = typeInto(t, editing, "SELECT 1")
	ran, cmd := press(t, editing, "f5")
	shown, _ := ran.Update(cmd())
	if !strings.Contains(plain(shown.(Model).content()), "no rows") {
		t.Errorf("content = %s", plain(shown.(Model).content()))
	}
}

func TestTheSpinnerTurnsWhileLoading(t *testing.T) {
	m := NewModel(session(healthy()), workspaceWith(t))
	updated, cmd := m.Update(spinner.TickMsg{})
	if cmd == nil {
		t.Fatal("the spinner must keep turning while loading")
	}
	if !strings.Contains(plain(updated.(Model).content()), "reading the server") {
		t.Errorf("content = %s", plain(updated.(Model).content()))
	}
	done := loaded(t, healthy())
	if _, cmd := done.Update(spinner.TickMsg{}); cmd != nil {
		t.Error("the spinner must stop once everything is read")
	}
}

func TestColumnsFillThePane(t *testing.T) {
	names := []string{"id", "email"}
	rows := [][]string{{"1", "a@example.com"}}

	wide := columnsFor(names, rows, 80)
	if width := tableWidth(wide, 80); width != 80 {
		t.Errorf("a result must fill its pane, got %d of 80", width)
	}

	narrow := columnsFor(names, rows, 20)
	if width := tableWidth(narrow, 20); width > 20 {
		t.Errorf("a result must fit its pane, got %d of 20", width)
	}
	for _, column := range narrow {
		if column.Width < minColumnWide {
			t.Errorf("a column must stay readable: %+v", column)
		}
	}

	long := columnsFor([]string{"definition"},
		[][]string{{strings.Repeat("x", 200)}}, 120)
	if long[0].Width <= 32 {
		t.Errorf("a long value is no longer capped at 32, got %d", long[0].Width)
	}

	tiny := columnsFor(names, rows, 4)
	for _, column := range tiny {
		if column.Width != minColumnWide {
			t.Errorf("a window with no room falls back to the floor: %+v", column)
		}
	}
	if columnsFor(nil, nil, 40) != nil {
		t.Error("a result without columns has no columns")
	}
}

func TestLaunchAndRunSetupAreThinWrappers(t *testing.T) {
	if err := Launch(session(healthy()), workspaceWith(t)); err == nil {
		t.Log("the interface started, which means this machine has a terminal")
	}
}

func TestTableWidthFitsTheTerminal(t *testing.T) {
	columns := columnsFor([]string{"a", "b"}, [][]string{{"1", "2"}}, 80)
	if got := tableWidth(columns, 0); got != 80 {
		t.Errorf("without a limit the table keeps the width it was given: %d", got)
	}
	if got := tableWidth(columns, 8); got != 8 {
		t.Errorf("a narrow terminal must cap the table: %d", got)
	}
}

func TestResultsIgnoreKeysWhenThereIsNothingToScroll(t *testing.T) {
	empty := results{}
	updated, cmd := empty.update(keyMsg("down"))
	if cmd != nil || updated.present {
		t.Error("an empty result has nothing to scroll")
	}
	failed := results{present: true, failure: "boom"}
	if _, cmd := failed.update(keyMsg("down")); cmd != nil {
		t.Error("a failed result has nothing to scroll")
	}
}

func TestTheHelpIsADocument(t *testing.T) {
	m := loaded(t, healthy())
	m.width, m.height = 100, 40
	help, _ := press(t, m, "?")
	first := plain(help.content())
	if !strings.Contains(first, "THE EDITOR") {
		t.Errorf("the document must be rendered:\n%s", first)
	}
	if strings.Contains(first, "`") || strings.Contains(first, "##") {
		t.Errorf("markdown must not leak through:\n%s", first)
	}
	if plain(help.content()) != first {
		t.Error("a second render at the same width must match the first")
	}
}
