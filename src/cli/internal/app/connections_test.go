package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/sonquer/tui4db/src/cli/internal/cli"
	"github.com/sonquer/tui4db/src/cli/internal/config"
	"github.com/sonquer/tui4db/src/cli/internal/driver"
)

type fakeWorkspace struct {
	profiles config.Profiles
	list     error
	open     error
	remove   error
	removed  []string
	opened   []string
	closed   int
	setup    cli.Setup
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
	if cmd == nil {
		t.Fatal("the connections screen must read the profiles")
	}
	updated, _ := listed.Update(cmd())
	return updated.(Model)
}

func TestTheConnectionsScreenListsTheProfiles(t *testing.T) {
	workspace := workspaceWith(t)
	m := browsing(t, workspace)
	view := plain(m.content())
	for _, want := range []string{"production-eu", "staging", "db.example.com:5432", "read only", "open"} {
		if !strings.Contains(view, want) {
			t.Errorf("the list must show %q:\n%s", want, view)
		}
	}
	if !strings.Contains(view, "production-eu ·") {
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

func TestTheConnectionsScreenIsEmptyWithoutProfiles(t *testing.T) {
	workspace := workspaceWith(t)
	workspace.profiles = config.Profiles{}
	m := browsing(t, workspace)
	if !strings.Contains(plain(m.content()), "no connections are configured") {
		t.Errorf("content = %s", plain(m.content()))
	}
	if _, cmd := press(t, m, "enter"); cmd != nil {
		t.Error("there is nothing to open")
	}
	if _, cmd := press(t, m, "d"); cmd != nil {
		t.Error("there is nothing to remove")
	}
	if moved, _ := press(t, m, "down"); moved.list.cursor != 0 {
		t.Errorf("there is nothing to move through: %d", moved.list.cursor)
	}
}

func TestMovingThroughTheConnections(t *testing.T) {
	m := browsing(t, workspaceWith(t))
	down, _ := press(t, m, "down")
	if down.list.cursor != 1 {
		t.Fatalf("cursor = %d", down.list.cursor)
	}
	wrapped, _ := press(t, down, "j")
	if wrapped.list.cursor != 0 {
		t.Fatalf("the list must wrap: %d", wrapped.list.cursor)
	}
	up, _ := press(t, wrapped, "k")
	if up.list.cursor != 1 {
		t.Fatalf("cursor = %d", up.list.cursor)
	}
	back, _ := press(t, up, "esc")
	if back.view != viewDashboard {
		t.Fatalf("view = %s", back.view)
	}
	closed, _ := press(t, up, "ctrl+p")
	if closed.view != viewDashboard {
		t.Fatalf("view = %s", closed.view)
	}
}

func TestSwitchingToAnotherConnection(t *testing.T) {
	workspace := workspaceWith(t)
	m := browsing(t, workspace)
	chosen, _ := press(t, m, "down")
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
	loadedAgain, _ := m.Update(m.load()())
	if len(loadedAgain.(Model).tables) == 0 {
		t.Error("the new connection must be read")
	}
	m.release()
	if workspace.closed != 1 {
		t.Errorf("leaving must close the connection the program opened: %d", workspace.closed)
	}
}

func TestSwitchingToTheSameConnectionChangesNothing(t *testing.T) {
	workspace := workspaceWith(t)
	m := browsing(t, workspace)
	same, cmd := press(t, m, "enter")
	if cmd != nil || same.view != viewDashboard || same.loading {
		t.Error("the connection in use is already open")
	}
	if len(workspace.opened) != 0 {
		t.Errorf("opened = %v", workspace.opened)
	}
}

func TestAFailedSwitchKeepsTheCurrentConnection(t *testing.T) {
	workspace := workspaceWith(t)
	workspace.open = errors.New("password not found in the keychain")
	m := browsing(t, workspace)
	chosen, _ := press(t, m, "down")
	switching, cmd := press(t, chosen, "enter")
	failed, _ := switching.Update(runFirst(t, cmd))
	m = failed.(Model)
	if m.session.Connection.Name != "production-eu" {
		t.Fatalf("the old connection must stand: %q", m.session.Connection.Name)
	}
	if m.view != viewSwitch || m.loading {
		t.Fatalf("view = %s loading = %v", m.view, m.loading)
	}
	if !strings.Contains(plain(m.content()), "password not found in the keychain") {
		t.Errorf("content = %s", plain(m.content()))
	}
}

func TestRemovingAConnectionNeedsItsNameTypedBack(t *testing.T) {
	workspace := workspaceWith(t)
	m := browsing(t, workspace)
	asked, cmd := press(t, m, "d")
	if cmd == nil || !asked.list.removing() {
		t.Fatal("removal must ask first")
	}
	view := plain(asked.content())
	if !strings.Contains(view, "remove production-eu and its password?") || !strings.Contains(view, "type the name to confirm") {
		t.Errorf("content = %s", view)
	}
	if refused, cmd := press(t, asked, "enter"); cmd != nil || !refused.list.removing() {
		t.Fatal("an empty confirmation must not remove anything")
	}

	typed := asked
	for _, key := range strings.Split("production-eu", "") {
		typed, _ = press(t, typed, key)
	}
	confirmed, cmd := press(t, typed, "enter")
	if cmd == nil || confirmed.list.removing() {
		t.Fatal("a matching name must remove the connection")
	}
	removed, _ := confirmed.Update(cmd())
	m = removed.(Model)
	if len(workspace.removed) != 1 || workspace.removed[0] != "production-eu" {
		t.Fatalf("removed = %v", workspace.removed)
	}
	relisted, _ := m.Update(runFirst(t, m.profiles()))
	if !strings.Contains(plain(relisted.(Model).content()), "production-eu is gone") {
		t.Errorf("content = %s", plain(relisted.(Model).content()))
	}
	if len(relisted.(Model).list.profiles) != 1 {
		t.Errorf("the list must be read again: %+v", relisted.(Model).list.profiles)
	}
}

func TestRemovalCanBeCalledOff(t *testing.T) {
	workspace := workspaceWith(t)
	m := browsing(t, workspace)
	asked, _ := press(t, m, "d")
	called, _ := press(t, asked, "esc")
	if called.list.removing() {
		t.Fatal("esc must call the removal off")
	}
	if len(workspace.removed) != 0 {
		t.Errorf("removed = %v", workspace.removed)
	}
}

func TestAFailedRemovalIsReported(t *testing.T) {
	workspace := workspaceWith(t)
	workspace.remove = errors.New("the keychain is locked")
	m := browsing(t, workspace)
	asked, _ := press(t, m, "d")
	typed := asked
	for _, key := range strings.Split("production-eu", "") {
		typed, _ = press(t, typed, key)
	}
	confirmed, cmd := press(t, typed, "enter")
	failed, _ := confirmed.Update(cmd())
	if !strings.Contains(plain(failed.(Model).content()), "the keychain is locked") {
		t.Errorf("content = %s", plain(failed.(Model).content()))
	}
}

func TestQuittingFromTheConnectionsScreen(t *testing.T) {
	m := browsing(t, workspaceWith(t))
	quit, cmd := press(t, m, "ctrl+c")
	if cmd == nil || !quit.quitting {
		t.Error("ctrl+c must leave")
	}
	if _, cmd := press(t, m, "x"); cmd != nil {
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
	if !strings.Contains(plain(adding.content()), "tui4db setup") {
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
	editing, _ := press(t, m, "e")
	listed, cmd := press(t, editing, "ctrl+p")
	if cmd == nil || listed.view != viewSwitch {
		t.Fatalf("view = %s", listed.view)
	}
	if !strings.Contains(plain(listed.footer(0)), "open") {
		t.Errorf("the list footer must name its own keys: %s", plain(listed.footer(0)))
	}
}
