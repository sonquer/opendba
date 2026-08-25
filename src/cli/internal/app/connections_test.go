package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/sonquer/opendba/src/cli/internal/cli"
	"github.com/sonquer/opendba/src/cli/internal/config"
	"github.com/sonquer/opendba/src/cli/internal/driver"
)

type fakeWorkspace struct {
	profiles   config.Profiles
	list       error
	open       error
	remove     error
	remember   error
	remembered []string
	removed    []string
	opened     []string
	closed     int
	setup      cli.Setup
}

func (w *fakeWorkspace) Profiles() (config.Profiles, error) { return w.profiles, w.list }

func (w *fakeWorkspace) Open(_ context.Context, name string) (cli.Session, func(), error) {
	w.opened = append(w.opened, name)
	if w.open != nil {
		return cli.Session{}, nil, w.open
	}
	opened := session(healthy())
	connection, ok := w.profiles.ByName(name)
	if ok {
		opened.Connection = connection
	}
	opened.Info = driver.ServerInfo{Driver: connection.Driver, Version: "3.45"}
	return opened, func() { w.closed++ }, nil
}

func (w *fakeWorkspace) OpenDatabase(ctx context.Context, name, database string) (cli.Session, func(), error) {
	session, cleanup, err := w.Open(ctx, name)
	if err != nil {
		return session, cleanup, err
	}
	session.Connection.Database = database
	session.Info.Database = database
	return session, cleanup, nil
}

func (w *fakeWorkspace) Remember(profile, database, schema string, schemas []string) error {
	if w.remember != nil {
		return w.remember
	}
	line := profile + "/" + database + "/" + schema
	if len(schemas) > 0 {
		line += "/" + strings.Join(schemas, "+")
	}
	w.remembered = append(w.remembered, line)
	return nil
}

func (w *fakeWorkspace) Remove(_ context.Context, name string) error {
	if w.remove != nil {
		return w.remove
	}
	w.removed = append(w.removed, name)
	w.profiles.Connections = slicesWithout(w.profiles.Connections, name)
	return nil
}

func (w *fakeWorkspace) Setup() cli.Setup { return w.setup }

func slicesWithout(connections []config.Connection, name string) []config.Connection {
	kept := make([]config.Connection, 0, len(connections))
	for _, connection := range connections {
		if connection.Name != name {
			kept = append(kept, connection)
		}
	}
	return kept
}

func workspaceWith(t *testing.T) *fakeWorkspace {
	t.Helper()
	setup, _ := newSetup(t)
	return &fakeWorkspace{
		profiles: config.Profiles{Connections: []config.Connection{
			{ID: "1", Name: "production-eu", Driver: "postgres", Mode: config.ReadOnly, Color: "red", Host: "db.example.com", Port: 5432},
			{ID: "2", Name: "staging", Driver: "sqlite", Mode: config.ReadWrite, Color: "amber", File: "staging.db"},
		}},
		setup: setup,
	}
}

func browsing(t *testing.T, workspace cli.Workspace) Model {
	t.Helper()
	m := loadedWith(t, healthy(), workspace)
	listed, cmd := press(t, m, "ctrl+p")
	if cmd == nil || listed.switcher == nil {
		t.Fatal("ctrl+p must open the switcher and read the profiles")
	}
	updated, _ := listed.Update(cmd())
	return updated.(Model)
}

func TestTheConnectionsScreenListsTheProfiles(t *testing.T) {
	workspace := workspaceWith(t)
	m := browsing(t, workspace)
	view := plain(m.content())
	for _, want := range []string{"production-eu", "staging", "db.example.com:5432", "READ ONLY", "open"} {
		if !strings.Contains(view, want) {
			t.Errorf("the list must show %q:\n%s", want, view)
		}
	}
	if !strings.Contains(view, "in use") {
		t.Error("the connection in use must be marked")
	}
}

func TestTheConnectionsScreenReportsAStoreItCannotRead(t *testing.T) {
	workspace := workspaceWith(t)
	workspace.list = errors.New("profiles.toml is world readable")
	m := browsing(t, workspace)
	if !strings.Contains(plain(m.content()), "world readable") {
		t.Errorf("content = %s", plain(m.content()))
	}
}

// With nothing configured there is still somewhere you are working, so the
// switcher shows the session you are on and nothing to open beside it.
func TestTheSwitcherShowsTheSessionEvenWithNothingConfigured(t *testing.T) {
	workspace := workspaceWith(t)
	workspace.profiles = config.Profiles{}
	m := browsing(t, workspace)
	view := plain(m.content())
	if !strings.Contains(view, "production-eu") || !strings.Contains(view, "in use") {
		t.Errorf("the session you are on is still a row:\n%s", view)
	}
	if len(m.rows4Switch()) != 1 {
		t.Errorf("rows = %d, nothing is configured to open", len(m.rows4Switch()))
	}
	if _, cmd := press(t, m, "d"); cmd != nil {
		t.Error("a session is not something this removes")
	}
	if moved, _ := press(t, m, "down"); moved.switcher.cursor != 0 {
		t.Errorf("one row is nowhere to move to: %d", moved.switcher.cursor)
	}
}

func TestMovingThroughTheConnections(t *testing.T) {
	m := browsing(t, workspaceWith(t))
	rows := len(m.rows4Switch())
	if rows < 2 {
		t.Fatalf("rows = %d", rows)
	}
	down, _ := press(t, m, "down")
	if down.switcher.cursor != 1 {
		t.Fatalf("cursor = %d", down.switcher.cursor)
	}
	wrapped := down
	for range rows - 1 {
		wrapped, _ = press(t, wrapped, "j")
	}
	if wrapped.switcher.cursor != 0 {
		t.Fatalf("the list must wrap: %d", wrapped.switcher.cursor)
	}
	up, _ := press(t, wrapped, "k")
	if up.switcher.cursor != rows-1 {
		t.Fatalf("cursor = %d", up.switcher.cursor)
	}
	if back, _ := press(t, up, "esc"); back.switcher != nil {
		t.Error("esc closes it")
	}
	if closed, _ := press(t, up, "ctrl+p"); closed.switcher != nil {
		t.Error("and so does the key that opened it")
	}
}

func TestSwitchingToAnotherConnection(t *testing.T) {
	workspace := workspaceWith(t)
	m := browsing(t, workspace)
	chosen := on4Switch(t, m, key4Profile("2"))
	switching, cmd := press(t, chosen, "enter")
	if cmd == nil || !switching.loading || switching.view != viewDashboard {
		t.Fatalf("switching must open the connection: loading=%v view=%s", switching.loading, switching.view)
	}
	switched, _ := switching.Update(runFirst(t, cmd))
	m = switched.(Model)
	if m.session.Connection.Name != "staging" {
		t.Fatalf("connection = %q", m.session.Connection.Name)
	}
	if workspace.closed != 0 {
		t.Error("the session the program was started with is closed by the caller")
	}
	if !strings.Contains(plain(m.content()), "now on staging") {
		t.Errorf("a switch must be announced:\n%s", plain(m.content()))
	}
	if len(m.tables) != 0 {
		t.Error("the new connection has not been read yet, and must not show the old one's")
	}
	loadedAgain := settle(t, m, m.load())
	if len(loadedAgain.tables) == 0 {
		t.Error("the new connection must be read")
	}
	m.release()
	if workspace.closed != 1 {
		t.Errorf("leaving must close the connection the program opened: %d", workspace.closed)
	}
}

func TestAFailedSwitchKeepsTheCurrentConnection(t *testing.T) {
	workspace := workspaceWith(t)
	workspace.open = errors.New("password not found in the keychain")
	m := browsing(t, workspace)
	chosen := on4Switch(t, m, key4Profile("2"))
	switching, cmd := press(t, chosen, "enter")
	failed, _ := switching.Update(runFirst(t, cmd))
	m = failed.(Model)
	if m.session.Connection.Name != "production-eu" {
		t.Fatalf("the old connection must stand: %q", m.session.Connection.Name)
	}
	if m.switcher == nil || m.loading {
		t.Fatalf("view = %s loading = %v", m.view, m.loading)
	}
	if !strings.Contains(plain(m.content()), "password not found in the keychain") {
		t.Errorf("the row must say why:\n%s", plain(m.content()))
	}
}

func TestRemovingAConnectionNeedsItsNameTypedBack(t *testing.T) {
	workspace := workspaceWith(t)
	m := on4Switch(t, browsing(t, workspace), key4Profile("2"))
	asked, cmd := press(t, m, "d")
	if cmd == nil || asked.modal == nil {
		t.Fatal("removal must ask first")
	}
	view := plain(asked.content())
	if !strings.Contains(view, "remove staging?") || !strings.Contains(view, "type the name to") {
		t.Errorf("content = %s", view)
	}
	if refused, cmd := press(t, asked, "enter"); cmd != nil || refused.modal == nil {
		t.Fatal("an empty confirmation must not remove anything")
	}

	typed := asked
	for _, key := range strings.Split("staging", "") {
		typed, _ = press(t, typed, key)
	}
	confirmed, cmd := press(t, typed, "enter")
	if cmd == nil || confirmed.modal != nil {
		t.Fatal("a matching name must remove the connection")
	}
	asking, _ := confirmed.Update(cmd())
	m = asking.(Model)
	removing, cmd := m, m.remove("staging")
	removed, _ := removing.Update(cmd())
	m = removed.(Model)
	if len(workspace.removed) != 1 || workspace.removed[0] != "staging" {
		t.Fatalf("removed = %v", workspace.removed)
	}
	relisted, _ := m.Update(runFirst(t, m.profiles()))
	if !strings.Contains(relisted.(Model).text(), "staging is gone") {
		t.Errorf("said = %q", relisted.(Model).text())
	}
	if len(relisted.(Model).rows4Switch()) != 2 {
		t.Errorf("the list must be read again: %+v", relisted.(Model).rows4Switch())
	}
}

func TestAFailedRemovalIsReported(t *testing.T) {
	workspace := workspaceWith(t)
	workspace.remove = errors.New("the keychain is locked")
	m := on4Switch(t, browsing(t, workspace), key4Profile("2"))
	asked, _ := press(t, m, "d")
	typed := asked
	for _, key := range strings.Split("staging", "") {
		typed, _ = press(t, typed, key)
	}
	confirmed, cmd := press(t, typed, "enter")
	asking, _ := confirmed.Update(cmd())
	failed, _ := asking.(Model).Update(runFirst(t, asking.(Model).remove("staging")))
	if !strings.Contains(failed.(Model).text(), "the keychain is locked") {
		t.Errorf("said = %q", failed.(Model).text())
	}
}

func TestQuittingFromTheConnectionsScreen(t *testing.T) {
	m := browsing(t, workspaceWith(t))
	asked, _ := press(t, m, "ctrl+c")
	if asked.modal == nil {
		t.Error("ctrl+c must ask before it leaves")
	}
	if _, cmd := press(t, m, "z"); cmd != nil {
		t.Error("an unknown key does nothing")
	}
}

func TestTheWizardOpensInsideTheProgram(t *testing.T) {
	workspace := workspaceWith(t)
	m := browsing(t, workspace)
	adding, _ := press(t, m, "n")
	if adding.wizard == nil {
		t.Fatal("n must open the wizard")
	}
	if !strings.Contains(plain(adding.content()), "opendba setup") {
		t.Errorf("content = %s", plain(adding.content()))
	}
	sized, _ := adding.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	if sized.(Model).wizard.width != 100 {
		t.Errorf("the wizard must follow the window: %d", sized.(Model).wizard.width)
	}
	typed, _ := press(t, sized.(Model), "right")
	if typed.wizard.form.value("driver") != "SQLite" {
		t.Errorf("the wizard must take the keys: %q", typed.wizard.form.value("driver"))
	}

	left, cmd := press(t, typed, "ctrl+c")
	if left.wizard != nil || left.quitting {
		t.Fatal("leaving the wizard must not leave the program")
	}
	if cmd == nil {
		t.Fatal("closing the wizard must read the profiles again")
	}
}

func TestASavedConnectionIsOpenedStraightAway(t *testing.T) {
	workspace := workspaceWith(t)
	m := browsing(t, workspace)
	adding, _ := press(t, m, "n")
	saved := SetupDone{Connection: config.Connection{Name: "staging"}, Saved: true}
	opening, cmd := adding.Update(saved)
	if opening.(Model).wizard != nil || !opening.(Model).loading {
		t.Fatal("a saved connection must be opened")
	}
	switched, _ := opening.(Model).Update(runFirst(t, cmd))
	if switched.(Model).session.Connection.Name != "staging" {
		t.Fatalf("connection = %q", switched.(Model).session.Connection.Name)
	}
}

func TestAWizardThatSavedNothingReturnsToTheList(t *testing.T) {
	m := browsing(t, workspaceWith(t))
	adding, _ := press(t, m, "n")
	back, cmd := adding.Update(SetupDone{})
	if back.(Model).wizard != nil || back.(Model).loading {
		t.Fatal("nothing was saved, so nothing is opened")
	}
	if cmd == nil {
		t.Fatal("the list must be read again")
	}
}

func TestTheConnectionsScreenIsReachedFromTheEditor(t *testing.T) {
	m := loadedWith(t, healthy(), workspaceWith(t))
	m.width, m.height = 110, 32
	editing, _ := press(t, m, "e")
	editing = typeInto(t, editing, "SELECT 1")
	listed, cmd := press(t, editing, "ctrl+p")
	if cmd == nil || listed.switcher == nil {
		t.Fatal("ctrl+p must open the switcher over the editor")
	}
	if listed.view != viewQuery {
		t.Errorf("view = %s, an overlay does not take you anywhere", listed.view)
	}
	shown := plain(settle(t, listed, cmd).content())
	if !strings.Contains(shown, "connections") || !strings.Contains(shown, "SELECT") {
		t.Errorf("it is drawn over the editor it was opened from:\n%s", shown)
	}
}

// A host, a port or an access mode can be changed without removing the
// connection and starting again.
func TestAConnectionCanBeEdited(t *testing.T) {
	workspace := workspaceWith(t)
	m := browsing(t, workspace)
	editing, cmd := press(t, m, "e")
	if editing.wizard == nil || cmd == nil {
		t.Fatal("e must open the profile under the cursor")
	}
	if editing.wizard.editing.Name != "production-eu" {
		t.Errorf("editing = %+v", editing.wizard.editing)
	}
	if editing.wizard.stage != stageDetails {
		t.Error("the driver is not a thing an existing connection changes")
	}
	view := plain(editing.wizard.content())
	for _, want := range []string{"configuration", "production-eu"} {
		if !strings.Contains(view, want) {
			t.Errorf("the screen must say what it is editing %q:\n%s", want, view)
		}
	}
	if editing.wizard.form.value("name") != "production-eu" {
		t.Errorf("every field must be seeded: %q", editing.wizard.form.value("name"))
	}
}

// Saving a profile that is not the one in use writes it and stays where you
// are. Saving the one in use opens it again, because the change is about the
// connection you are on.
func TestSavingAnEditedProfile(t *testing.T) {
	workspace := workspaceWith(t)
	m := browsing(t, workspace)
	editing, _ := press(t, m, "e")

	elsewhere := SetupDone{Connection: config.Connection{ID: "another", Name: "staging"}, Saved: true}
	stayed, _ := editing.Update(elsewhere)
	if stayed.(Model).loading {
		t.Error("saving somebody else's profile is not a reason to reopen yours")
	}
	if !strings.Contains(stayed.(Model).text(), "staging is saved") {
		t.Errorf("but it must say it saved: %q", stayed.(Model).text())
	}

	mine := SetupDone{Connection: editing.session.Connection, Saved: true}
	reopened, cmd := editing.Update(mine)
	if !reopened.(Model).loading || cmd == nil {
		t.Error("the connection in use must be opened again")
	}
}

// The list of connections says what is known about each one, and what the one
// in use is doing.
func TestTheConnectionListSaysWhatIsKnown(t *testing.T) {
	m := loaded(t, healthy())
	m.width, m.height = 120, 32
	m.running = m.running.withSessions(sessionsMsg{
		sessions: []driver.Session{
			{ID: "1", Application: "psql", State: "active"},
			{ID: "2", Application: "psql", State: "idle"},
		},
	}, 100)

	browsing, cmd := m.openSwitcher()
	shown := settle(t, browsing.(Model), cmd)
	view := plain(shown.content())
	for _, want := range []string{"production-eu", "sqlite", "READ ONLY", "2 running"} {
		if !strings.Contains(view, want) {
			t.Errorf("the list must say %q:\n%s", want, view)
		}
	}
}

// A connection nobody is using says nothing about what it is doing, and the
// count is per connection now that several of them are open at once.
func TestAConnectionNobodyIsUsingSaysNothing(t *testing.T) {
	m := loaded(t, healthy())
	if len(m.busy4Links()) != 0 {
		t.Errorf("busy = %v, nothing is running", m.busy4Links())
	}
	m.running = m.running.withSessions(sessionsMsg{
		sessions: []driver.Session{{ID: "1"}, {ID: "2"}, {ID: "3"}},
	}, 100)
	if got := m.busy4Links()[m.id]; got != "3 running" {
		t.Errorf("busy = %v", m.busy4Links())
	}
}

// Opening another connection keeps the first one open. It used to be closed on
// the way, which is why a tab could only ever be about one server.
func TestOpeningAnotherConnectionKeepsTheFirstOne(t *testing.T) {
	workspace := workspaceWith(t)
	m := browsing(t, workspace)
	first := m.session.Conn

	chosen := on4Switch(t, m, key4Profile("2"))
	switching, cmd := press(t, chosen, "enter")
	switched, _ := switching.Update(runFirst(t, cmd))
	m = switched.(Model)

	if len(m.links) != 2 {
		t.Fatalf("links = %d, both connections stay open", len(m.links))
	}
	if m.links[0].session.Conn != first {
		t.Error("the first connection is still the first one")
	}
	if workspace.closed != 0 {
		t.Errorf("closed = %d, nothing is closed on the way", workspace.closed)
	}
	if m.linked != 1 || m.session.Connection.Name != "staging" {
		t.Errorf("linked = %d on %q", m.linked, m.session.Connection.Name)
	}
}

// Opening a connection this program already holds opens another session on it.
// It used to be given straight back, which is why two windows onto one server
// were impossible.
func TestOpeningAProfileThatIsAlreadyOpenOpensASecondSession(t *testing.T) {
	workspace := workspaceWith(t)
	m := browsing(t, workspace)
	held := m.session.Conn

	again, _ := m.Update(switchedMsg{
		session: m.session,
		cleanup: func() { workspace.closed++ },
		profile: "production-eu",
	})
	m = again.(Model)

	if len(m.links) != 2 {
		t.Fatalf("links = %d, opening it again is a session of its own", len(m.links))
	}
	if workspace.closed != 0 {
		t.Errorf("closed = %d, the handle it opened is kept", workspace.closed)
	}
	if m.links[0].id == m.links[1].id {
		t.Error("and the two must be told apart")
	}
	if m.links[0].session.Conn != held {
		t.Error("the one that was already open is untouched")
	}
	if m.links[1].seq != 2 {
		t.Errorf("seq = %d, the second session on a database is the second", m.links[1].seq)
	}
	if !strings.Contains(m.label4Link(m.links[1]), "#2") {
		t.Errorf("label = %q, and says so", m.label4Link(m.links[1]))
	}
}

// Every connection this program opened is closed when it leaves, and the one it
// started on is left to the caller that owns it.
func TestEveryConnectionThisProgramOpenedIsClosedWhenItLeaves(t *testing.T) {
	workspace := workspaceWith(t)
	m := browsing(t, workspace)
	chosen, _ := press(t, m, "down")
	switching, cmd := press(t, chosen, "enter")
	switched, _ := switching.Update(runFirst(t, cmd))

	switched.(Model).release()
	if workspace.closed != 1 {
		t.Errorf("closed = %d, one of the two was opened here", workspace.closed)
	}
}

// twoConnections is a program with a tab on each of two servers: the first tab
// stays on the connection it started on, and the second is moved to the other.
func twoConnections(t *testing.T) (Model, *fakeWorkspace) {
	t.Helper()
	workspace := workspaceWith(t)
	m := loadedWith(t, healthy(), workspace)
	m.width, m.height = 110, 32
	editing, _ := press(t, m, "e")
	first := typeInto(t, editing, "SELECT 1")

	second, _ := press(t, first, "ctrl+n")
	listed, cmd := press(t, second, "ctrl+p")
	browsed, _ := listed.Update(cmd())
	chosen := on4Switch(t, browsed.(Model), key4Profile("2"))
	switching, cmd := press(t, chosen, "enter")
	switched, _ := switching.Update(runFirst(t, cmd))
	back, _ := press(t, switched.(Model), "e")
	return back, workspace
}

// A tab belongs to a connection, and coming back to it comes back to that
// connection: the header, the mode and the schema beside it all follow the tab.
func TestATabRemembersWhichConnectionItIsOn(t *testing.T) {
	m, _ := twoConnections(t)
	if m.session.Connection.Name != "staging" || m.on != m.link.key() {
		t.Fatalf("the tab that moved is on %q", m.session.Connection.Name)
	}
	if len(m.links) != 2 {
		t.Fatalf("links = %d", len(m.links))
	}

	back, _ := press(t, m, "ctrl+1")
	if back.session.Connection.Name != "production-eu" {
		t.Errorf("connection = %q, the first tab never left it", back.session.Connection.Name)
	}
	if back.statement() != "SELECT 1" {
		t.Errorf("statement = %q", back.statement())
	}
	if !strings.Contains(plain(back.header()), "production-eu") {
		t.Errorf("the header follows the tab in front:\n%s", plain(back.header()))
	}
	forward, _ := press(t, back, "ctrl+2")
	if forward.session.Connection.Name != "staging" {
		t.Errorf("connection = %q, and so does going back the other way",
			forward.session.Connection.Name)
	}
}

// Two tabs on two connections are two access modes, and each statement answers
// to the mode of the connection its own tab is worked through.
func TestTwoTabsOnDifferentConnectionsClassifyAgainstTheirOwn(t *testing.T) {
	m, _ := twoConnections(t)
	writable := typeInto(t, m, "DELETE FROM users")
	if !writable.Verdict().NeedsConfirmation() {
		t.Fatalf("staging is READ / WRITE: %+v", writable.Verdict())
	}
	asked, _ := press(t, writable, "f5")
	if asked.modal == nil {
		t.Error("so a write is asked about rather than refused")
	}

	back, _ := press(t, writable, "ctrl+1")
	refusing := typeInto(t, back, "DELETE FROM users")
	if !refusing.Verdict().Blocked() {
		t.Fatalf("production-eu is READ ONLY: %+v", refusing.Verdict())
	}
	refused, _ := press(t, refusing, "f5")
	if refused.modal != nil {
		t.Error("and a write there is refused rather than asked about")
	}
}

// The strip says which connection a tab is on only once the tabs disagree, so
// the usual case of one connection costs it nothing.
func TestTheStripNamesAConnectionOnlyWhenTabsDisagree(t *testing.T) {
	one := workbench(t)
	one.width = 110
	if strings.Contains(plain(one.tabBar(110)), "production-eu") {
		t.Errorf("one connection needs no naming:\n%s", plain(one.tabBar(110)))
	}

	m, _ := twoConnections(t)
	strip := plain(m.tabBar(110))
	for _, want := range []string{"production-eu", "staging"} {
		if !strings.Contains(strip, want) {
			t.Errorf("the strip must name %q once they differ:\n%s", want, strip)
		}
	}
}

// Coming back to a connection that has been read reads nothing again, because a
// tab switch that costs a catalogue sweep is a tab switch nobody makes twice.
func TestSwitchingTabsDoesNotReadTheServerAgain(t *testing.T) {
	m, _ := twoConnections(t)
	back := settle(t, m, nil)
	first, cmd := press(t, back, "ctrl+1")
	settle(t, first, cmd)
	before := first.links[0].session.Conn.(*fakeConn).counted()["tables"]

	again, cmd := press(t, first, "ctrl+2")
	settle(t, again, cmd)
	returned, cmd := press(t, again, "ctrl+1")
	settle(t, returned, cmd)
	if got := first.links[0].session.Conn.(*fakeConn).counted()["tables"]; got != before {
		t.Errorf("tables read %d times, want %d: coming back reads nothing", got, before)
	}
}

// A result arriving for a tab on another connection is written down under that
// connection, not under whichever one happens to be in front.
func TestABackgroundResultIsFiledUnderItsOwnConnection(t *testing.T) {
	m, _ := twoConnections(t)
	first := m.sheets[0]
	first.token, first.inflight = 42, true
	m.sheets[0] = first

	landed, _ := m.Update(queriedMsg{
		statement: "SELECT 1", columns: []string{"n"},
		rows: [][]any{{int64(1)}}, token: 42,
	})
	arrived := landed.(Model)
	if arrived.session.Connection.Name != "staging" {
		t.Fatalf("the front tab must not have moved: %q", arrived.session.Connection.Name)
	}
	if !arrived.sheets[0].results.present {
		t.Error("and the result belongs to the tab that asked")
	}
}

// What the server is doing belongs to the connection it was asked of. The list
// used to land on whichever connection was in front when it came back.
func TestSessionsLandOnTheConnectionTheyWereReadFrom(t *testing.T) {
	m, _ := twoConnections(t)
	background := m.links[0].key()
	if background == m.link.key() {
		t.Fatal("the test needs the other connection in front")
	}

	landed, _ := m.Update(sessionsMsg{
		sessions: []driver.Session{{ID: "1"}, {ID: "2"}},
		on:       background,
	})
	arrived := landed.(Model)
	if len(arrived.running.sessions) != 0 {
		t.Errorf("the connection in front asked for nothing: %d", len(arrived.running.sessions))
	}
	if got := len(arrived.links[0].running.sessions); got != 2 {
		t.Errorf("sessions = %d, the connection that was read must have them", got)
	}
}

// on4Switch walks the switcher to one row.
func on4Switch(t *testing.T, m Model, wanted string) Model {
	t.Helper()
	for range len(m.rows4Switch()) {
		if chosen, ok := m.chosen4Switch(); ok && chosen.key == wanted {
			return m
		}
		m, _ = press(t, m, "down")
	}
	t.Fatalf("there is no row %q", wanted)
	return m
}

// TestAConnectionRowWearsItsAccessModeAsABadge pins the one thing on the row
// that says what this program will let you do, in the same badge the header
// carries it in.
func TestAConnectionRowWearsItsAccessModeAsABadge(t *testing.T) {
	workspace := workspaceWith(t)
	m := browsing(t, workspace)
	view := plain(m.content())
	for _, want := range []string{"READ ONLY", "READ / WRITE"} {
		if !strings.Contains(view, want) {
			t.Errorf("the list must badge %q:\n%s", want, view)
		}
	}
	rows := strings.Split(view, "\n")
	for _, line := range rows {
		if !strings.Contains(line, "READ") {
			continue
		}
		if strings.Contains(line, " · ") {
			t.Errorf("a connection row separates with room, not punctuation: %q", line)
		}
	}
}
