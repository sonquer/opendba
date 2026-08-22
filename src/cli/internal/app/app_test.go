package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
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

	// A batch runs its commands at once, so the counter is written from more
	// than one goroutine and has to be held.
	mu    sync.Mutex
	reads map[string]int
}

// fail is also where every read is counted, because a test that asks how often
// the program went to the server has to count somewhere.
func (f *fakeConn) fail(step string) error {
	f.mu.Lock()
	if f.reads == nil {
		f.reads = map[string]int{}
	}
	f.reads[step]++
	f.mu.Unlock()
	if f.failOn == step {
		return errors.New(step + " failed")
	}
	return nil
}

// counted is what the server has been asked for so far, so a test can measure
// what one beat added.
func (f *fakeConn) counted() map[string]int {
	f.mu.Lock()
	defer f.mu.Unlock()
	taken := map[string]int{}
	for step, count := range f.reads {
		taken[step] = count
	}
	return taken
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

// writable is a session on a profile that may change things, which is what the
// keys that stop other sessions are gated on.
func writable(conn driver.Conn) cli.Session {
	opened := session(conn)
	opened.Connection.Mode = config.ReadWrite
	return opened
}

func session(conn driver.Conn) cli.Session {
	return cli.Session{
		Capabilities: driver.Capabilities{Sessions: true, Health: true},
		Connection:   config.Connection{Name: "production-eu", Driver: "sqlite", Mode: config.ReadOnly, Color: "red"},
		Settings:     config.DefaultSettings(),
		Conn:         conn,
		Info:         driver.ServerInfo{Driver: "sqlite", Version: "3.45"},
		Guard:        sqlguard.New(sqldialect.SQLite()),
		Theme:        ui.Default(),
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

// twoSchemas is a server with something to choose between, which is what a form
// that ticks several schemas needs.
func twoSchemas() *fakeConn {
	conn := healthy()
	conn.tables = []driver.Table{
		{Schema: "public", Name: "users", Kind: "table", Rows: 2, Size: 4096},
		{Schema: "reporting", Name: "daily", Kind: "view", Rows: 9, Size: 8192},
	}
	conn.indexes = []driver.Index{
		{Schema: "public", Table: "users", Name: "users_pkey", Size: 2048},
		{Schema: "reporting", Table: "daily", Name: "daily_day", Size: 1024},
	}
	conn.schemas = []driver.Schema{
		{Name: "public", Tables: 1},
		{Name: "reporting", Tables: 1},
		{Name: "pg_catalog", Tables: 62, System: true},
	}
	return conn
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
	return settle(t, m, m.load())
}

// settle runs a command and feeds every message it produced back into the
// model, which is what a program does with a batch.
func settle(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	out := tea.Model(m)
	for _, msg := range runAll(t, cmd) {
		out, _ = out.Update(msg)
	}
	return out.(Model)
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
	for _, want := range []string{"production-eu", "sqlite 3.45", "READ ONLY", "integrity"} {
		if !strings.Contains(content, want) {
			t.Errorf("dashboard missing %q:\n%s", want, content)
		}
	}
	tables, _ := press(t, m, "s")
	if !strings.Contains(plain(tables.content()), "1 table") {
		t.Errorf("the tables screen counts what it lists:\n%s", plain(tables.content()))
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

// A page that is read rather than walked scrolls on its own, and cannot be
// scrolled past either end of itself.
func TestScrollingIsBounded(t *testing.T) {
	m := loaded(t, crowded(20))
	m.width, m.height = 90, 12
	page, _ := press(t, m, "?")
	if up, _ := press(t, page, "up"); up.offset != 0 {
		t.Errorf("offset = %d", up.offset)
	}
	down, _ := press(t, page, "down")
	if down.offset != 1 {
		t.Errorf("offset = %d", down.offset)
	}
	far := down
	for range 400 {
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
	paged, _ := press(t, page, "pgdown")
	if paged.offset == 0 {
		t.Error("pgdown must move a whole page")
	}
	if back, _ := press(t, paged, "pgup"); back.offset != 0 {
		t.Errorf("pgup must come back to the top, got %d", back.offset)
	}
	if unknown, _ := press(t, page, "w"); unknown.offset != 0 {
		t.Error("an unknown key scrolls nothing")
	}
	dashboard, _ := press(t, page, "esc")
	if dashboard.offset != 0 {
		t.Error("leaving a view goes back to the top")
	}
}

// The tables and the indexes are lists, so the arrows walk them, the body
// follows the cursor, and enter opens the row it is on.
func TestTheCatalogueListsAreWalked(t *testing.T) {
	for _, screen := range []struct {
		key   string
		view  view
		title string
	}{{"s", viewSchema, "main.users"}, {"i", viewIndexes, "main.users_pkey"}} {
		m := loaded(t, healthy())
		m.width, m.height = 100, 20
		list, _ := press(t, m, screen.key)
		if list.view != screen.view || list.listing != 0 {
			t.Fatalf("%s = %s, cursor %d", screen.key, list.view, list.listing)
		}
		wrapped, _ := press(t, list, "up")
		if wrapped.listing != list.listed()-1 {
			t.Errorf("the list wraps, got %d of %d", wrapped.listing, list.listed())
		}
		opened, _ := press(t, list, "enter")
		if opened.page == nil {
			t.Fatalf("enter on %s must open the row", screen.view)
		}
		if opened.page.title != screen.title {
			t.Errorf("page = %q, want %q", opened.page.title, screen.title)
		}
		closed, _ := press(t, opened, "esc")
		if closed.page != nil {
			t.Error("esc closes the page")
		}
	}
}

func TestAnEmptyCatalogueListHasNothingToOpen(t *testing.T) {
	conn := healthy()
	conn.tables, conn.indexes = nil, nil
	m := loaded(t, conn)
	m.width, m.height = 100, 20
	for _, key := range []string{"s", "i"} {
		list, _ := press(t, m, key)
		moved, _ := press(t, list, "down")
		if moved.listing != 0 {
			t.Errorf("there is nothing to walk: %d", moved.listing)
		}
		opened, _ := press(t, list, "enter")
		if opened.page != nil {
			t.Error("there is nothing to open")
		}
		if !strings.Contains(plain(list.content()), "no ") {
			t.Errorf("an empty list must say so:\n%s", plain(list.content()))
		}
	}
}

// The dashboard is a list, so the arrows walk it and the body follows the
// cursor rather than scrolling on its own.
func TestTheDashboardWalksItsReadings(t *testing.T) {
	m := loaded(t, crowded(20))
	m.width, m.height = 90, 16
	down, _ := press(t, m, "down")
	if down.reading != 1 {
		t.Fatalf("reading = %d", down.reading)
	}
	up, _ := press(t, down, "up")
	if up.reading != 0 {
		t.Errorf("reading = %d", up.reading)
	}
	wrapped, _ := press(t, up, "up")
	if wrapped.reading != len(wrapped.readings(every))-1 {
		t.Errorf("the list wraps, got %d", wrapped.reading)
	}
	if wrapped.offset == 0 {
		t.Error("the body must follow the cursor down the list")
	}
	if !strings.Contains(plain(wrapped.content()), "▌") {
		t.Errorf("the row under the cursor must be visible:\n%s", plain(wrapped.content()))
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
	if !strings.Contains(rows[1], "▀") {
		t.Errorf("the environment is a line across the top: %q", rows[1])
	}
	if !strings.HasPrefix(rows[2], "  → ~ production-eu") {
		t.Errorf("the identity line comes under it: %q", rows[2])
	}
	if resized.editor.Width() > resized.paneWidth() || resized.editor.Width() < resized.paneWidth()-8 {
		t.Errorf("the editor must fill its pane, got %d of %d", resized.editor.Width(), resized.paneWidth())
	}
	if resized.editor.Height() != resized.editorRows() {
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
	refused, cmd := press(t, editing, "f5")
	if refused.results.statement != "" {
		t.Fatal("a blocked statement must not run")
	}
	if cmd == nil {
		t.Fatal("a key that silently does nothing is a broken key")
	}
	if !strings.Contains(plain(refused.content()), "READ ONLY") {
		t.Errorf("the refusal must say why:\n%s", plain(refused.content()))
	}
	if !strings.Contains(plain(editing.content()), "blocked") {
		t.Errorf("content = %s", plain(editing.content()))
	}
}

// READ / WRITE was a mode that did nothing: the classifier answered Warn and
// both run paths dropped it in silence.
func TestAWriteAsksBeforeItRuns(t *testing.T) {
	conn := healthy()
	m := settle(t, NewModel(writable(conn), workspaceWith(t)), nil)
	m = settle(t, m, m.load())
	m.width, m.height = 110, 32
	editing, _ := press(t, m, "e")
	editing = typeInto(t, editing, "DELETE FROM users")
	if !editing.Verdict().NeedsConfirmation() {
		t.Fatalf("verdict = %+v", editing.Verdict())
	}
	asked, _ := press(t, editing, "f5")
	if asked.modal == nil {
		t.Fatal("a write must be asked about")
	}
	if !asked.modal.danger {
		t.Error("and asked about in the colour of something that costs")
	}
	dialog := strings.Join(strings.Fields(plain(asked.modal.view(110))), " ")
	for _, want := range []string{"run this statement?", "READ / WRITE", "DELETE FROM users"} {
		if !strings.Contains(dialog, want) {
			t.Errorf("the dialog must show %q:\n%s", want, dialog)
		}
	}
	answered, cmd := press(t, asked, "enter")
	if answered.modal != nil || cmd == nil {
		t.Fatal("enter answers it")
	}
	before := conn.counted()["query"]
	settle(t, settle(t, answered, cmd), answered.run(editing.statement()))
	if conn.counted()["query"] == before {
		t.Error("and the statement reaches the server")
	}

	left, cmd := press(t, asked, "esc")
	if left.modal != nil || cmd != nil {
		t.Error("esc leaves it alone")
	}
}

// A profile that has said not to ask is not asked.
func TestAWriteRunsWithoutAskingWhenToldTo(t *testing.T) {
	conn := healthy()
	m := settle(t, NewModel(writable(conn), workspaceWith(t)), nil)
	m.session.Settings.Safety.ConfirmQueries = false
	m = settle(t, m, m.load())
	m.width, m.height = 110, 32
	editing, _ := press(t, m, "e")
	editing = typeInto(t, editing, "DELETE FROM users")
	ran, cmd := press(t, editing, "f5")
	if ran.modal != nil {
		t.Error("nothing was asked")
	}
	if cmd == nil {
		t.Fatal("and the statement was run")
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

func TestAnEmptyEditorSaysItOnce(t *testing.T) {
	m := loaded(t, healthy())
	m.width, m.height = 100, 32
	editing, _ := press(t, m, "e")
	view := plain(editing.content())
	if strings.Contains(view, "nothing to run yet") {
		t.Errorf("one placeholder is enough:\n%s", view)
	}
	if !strings.Contains(view, "nothing has run yet") {
		t.Errorf("the result still says it is empty:\n%s", view)
	}
	typed, _ := press(t, editing, "s")
	if typed.verdict(80) == "" {
		t.Error("a statement being typed is classified")
	}
}

// A list of 148 indexes is a list nobody scrolls. f narrows it, and what is
// typed reaches the filter rather than the keys those letters are bound to.
func TestTheCatalogueListsAreSearched(t *testing.T) {
	m := loaded(t, twoSchemas())
	m.width, m.height = 120, 30
	list, _ := press(t, m, "s")
	finding, cmd := press(t, list, "f")
	if cmd == nil || !finding.lists[0].typing {
		t.Fatal("f must open the filter")
	}
	typed := finding
	for _, letter := range []string{"d", "a", "i"} {
		typed, _ = press(t, typed, letter)
	}
	if got := typed.lists[0].needle(); got != "dai" {
		t.Fatalf("every letter must reach the filter, got %q", got)
	}
	if shown := typed.shownTables(); len(shown) != 1 || shown[0].Name != "daily" {
		t.Errorf("the list must narrow: %+v", shown)
	}
	if typed.listed() != 1 {
		t.Errorf("and everything that counts rows must count the narrowed ones: %d", typed.listed())
	}
	view := plain(typed.content())
	if !strings.Contains(view, "1 of 2 match dai") {
		t.Errorf("a list hiding rows must say so:\n%s", view)
	}
	if strings.Contains(view, "public.users") {
		t.Errorf("and must not draw them:\n%s", view)
	}

	kept, _ := press(t, typed, "enter")
	if kept.lists[0].typing || !kept.lists[0].active() {
		t.Error("enter keeps the filter and gives the keys back to the list")
	}
	opened, _ := press(t, kept, "enter")
	if opened.page == nil || !strings.Contains(opened.page.title, "daily") {
		t.Error("enter on the narrowed list opens the row it is actually on")
	}
	cleared, _ := press(t, typed, "esc")
	if cleared.lists[0].active() || len(cleared.shownTables()) != 2 {
		t.Error("esc clears the filter")
	}
}

// The two lists are searched separately, because looking for an index is not
// looking for a table.
func TestEachCatalogueListKeepsItsOwnSearch(t *testing.T) {
	m := loaded(t, twoSchemas())
	m.width, m.height = 120, 30
	tables, _ := press(t, m, "s")
	tables, _ = press(t, tables, "f")
	tables, _ = press(t, tables, "d")
	tables, _ = press(t, tables, "enter")
	indexes, _ := press(t, tables, "i")
	if indexes.lists[1].active() {
		t.Error("the other list must not inherit the search")
	}
	back, _ := press(t, indexes, "s")
	if back.lists[0].needle() != "d" {
		t.Errorf("and this one must keep its own: %q", back.lists[0].needle())
	}
}

func TestTheCatalogueListsAreSorted(t *testing.T) {
	m := loaded(t, twoSchemas())
	m.width, m.height = 120, 30
	list, _ := press(t, m, "s")
	if list.lists[0].column != 0 || list.lists[0].reversed {
		t.Fatal("tables start in the order of their names")
	}
	if first := list.shownTables()[0]; first.Name != "users" && first.Schema != "public" {
		t.Errorf("first = %+v", first)
	}
	sorted, _ := press(t, list, "o")
	if sorted.lists[0].column != 1 || !sorted.lists[0].reversed {
		t.Errorf("o moves to the next column, largest first: %+v", sorted.lists[0])
	}
	if first := sorted.shownTables()[0]; first.Rows != 9 {
		t.Errorf("sorted by rows, the largest is first: %+v", first)
	}
	flipped, _ := press(t, sorted, "O")
	if flipped.lists[0].reversed {
		t.Error("shift+o turns it round")
	}
	if first := flipped.shownTables()[0]; first.Rows != 2 {
		t.Errorf("first = %+v", first)
	}
	round := flipped
	for range columns {
		round, _ = press(t, round, "o")
	}
	if round.lists[0].column != flipped.lists[0].column {
		t.Error("the columns wrap")
	}
	indexes, _ := press(t, m, "i")
	if indexes.lists[1].column != 2 || !indexes.lists[1].reversed {
		t.Errorf("indexes start with the largest: %+v", indexes.lists[1])
	}
}
