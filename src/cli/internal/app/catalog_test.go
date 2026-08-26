package app

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// The databases and the schemas are a dialog over the session in front, not a
// screen you go to: ctrl+d opens it and it never connects to anything.
func browsingCatalog(t *testing.T, conn *fakeConn) Model {
	t.Helper()
	m := loadedWith(t, conn, workspaceWith(t))
	m.width, m.height = 110, 32
	opened, cmd := press(t, m, "ctrl+d")
	if cmd == nil || opened.catalog == nil {
		t.Fatal("ctrl+d must open the databases of the session in front")
	}
	return pump(t, opened, cmd)
}

func TestTheCatalogListsDatabasesAndSchemas(t *testing.T) {
	m := browsingCatalog(t, healthy())
	view := plain(m.content())
	for _, want := range []string{"databases and schemas", "production-eu",
		"● app", "○ reporting", "▢ public", "1 table", "pick", "save", "cancel"} {
		if !strings.Contains(view, want) {
			t.Errorf("the catalog must show %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "pg_catalog") {
		t.Error("system schemas are noise")
	}
}

// onSchema walks the form down to its first schema, which is where the ticking
// happens.
func onSchema(t *testing.T, m Model) Model {
	t.Helper()
	for range len(m.catalog.rows4Catalog()) {
		if chosen, ok := m.catalog.selected(); ok && strings.HasPrefix(chosen.key, rowSchema) {
			return m
		}
		m, _ = press(t, m, "down")
	}
	t.Fatal("the form has no schema to tick")
	return m
}

// onDatabase walks the form to one database.
func onDatabase(t *testing.T, m Model, database string) Model {
	t.Helper()
	for range len(m.catalog.rows4Catalog()) {
		if chosen, ok := m.catalog.selected(); ok && chosen.key == rowDatabase+database {
			return m
		}
		m, _ = press(t, m, "down")
	}
	t.Fatalf("there is no row for %s", database)
	return m
}

func TestTheCatalogTicksSeveralSchemas(t *testing.T) {
	m := browsingCatalog(t, twoSchemas())
	ticked, cmd := press(t, onSchema(t, m), " ")
	if cmd != nil {
		t.Error("ticking a box reads nothing and reconnects to nothing")
	}
	if ticked.catalog == nil {
		t.Fatal("the form stays open until it is saved or left")
	}
	down, _ := press(t, ticked, "down")
	both, _ := press(t, down, " ")
	if got := both.catalog.chosen(); !reflect.DeepEqual(got, []string{"public", "reporting"}) {
		t.Fatalf("chosen = %v", got)
	}
	if view := plain(both.content()); !strings.Contains(view, "▣ public") ||
		!strings.Contains(view, "▣ reporting") {
		t.Errorf("both boxes must be ticked:\n%s", view)
	}
	off, _ := press(t, both, " ")
	if got := off.catalog.chosen(); !reflect.DeepEqual(got, []string{"public"}) {
		t.Errorf("space must untick as well: %v", got)
	}

	saved, cmd := press(t, off, "enter")
	if cmd == nil || !saved.loading || saved.catalog != nil {
		t.Fatalf("enter must apply it: loading=%v", saved.loading)
	}
	if got := saved.session.Connection.Filter(); !reflect.DeepEqual(got, []string{"public"}) {
		t.Errorf("filter = %v", got)
	}
}

func TestChoosingADatabaseReconnects(t *testing.T) {
	workspace := workspaceWith(t)
	m := browsingCatalog(t, healthy())
	m.workspace = workspace

	picked, _ := press(t, onDatabase(t, m, "reporting"), " ")
	if picked.catalog.database != "reporting" {
		t.Fatalf("space must move the dot: %q", picked.catalog.database)
	}
	switching, cmd := press(t, picked, "enter")
	if cmd == nil || !switching.loading || switching.catalog != nil {
		t.Fatalf("choosing a database must reconnect: loading=%v", switching.loading)
	}
	switched, _ := switching.Update(runFirst(t, cmd))
	if got := switched.(Model).session.Info.Database; got != "reporting" {
		t.Errorf("database = %q", got)
	}
}

func TestChoosingASchemaJustReloads(t *testing.T) {
	m := browsingCatalog(t, healthy())
	schema, _ := press(t, onSchema(t, m), " ")
	reloading, cmd := press(t, schema, "enter")
	if cmd == nil || !reloading.loading {
		t.Fatal("a schema change must read the server again")
	}
	if reloading.session.Connection.DefaultSchema != "public" {
		t.Errorf("schema = %q", reloading.session.Connection.DefaultSchema)
	}
	if !strings.Contains(reloading.text(), "reading public") {
		t.Errorf("the change must be announced: %q", reloading.text())
	}
}

func TestApplyingTheSameSchemasDoesNothing(t *testing.T) {
	m := browsingCatalog(t, healthy())
	quiet, cmd := press(t, onSchema(t, m), "enter")
	if cmd != nil {
		t.Error("nothing was ticked, so nothing is read again")
	}
	if quiet.catalog != nil {
		t.Error("and the form is done with")
	}
}

func TestTheCatalogSaysWhatItCannotRead(t *testing.T) {
	for _, part := range []string{"databases", "schemas"} {
		t.Run(part, func(t *testing.T) {
			conn := healthy()
			conn.failOn = part
			m := browsingCatalog(t, conn)
			if !strings.Contains(plain(m.content()), part+" failed") {
				t.Errorf("content = %s", plain(m.content()))
			}
		})
	}
}

func TestLeavingTheCatalog(t *testing.T) {
	for _, key := range []string{"esc", "ctrl+d"} {
		t.Run(key, func(t *testing.T) {
			out, _ := press(t, browsingCatalog(t, healthy()), key)
			if out.catalog != nil {
				t.Errorf("%s closes it", key)
			}
		})
	}
	asked, _ := press(t, browsingCatalog(t, healthy()), "ctrl+c")
	if asked.modal == nil {
		t.Error("quitting must ask, even from a dialog")
	}
	if same, cmd := press(t, browsingCatalog(t, healthy()), "z"); cmd != nil || same.catalog == nil {
		t.Error("an unknown key does nothing")
	}
}

func TestChoosingIsRemembered(t *testing.T) {
	workspace := workspaceWith(t)
	m := browsingCatalog(t, healthy())
	m.workspace = workspace

	schema, _ := press(t, onSchema(t, m), " ")
	reloading, cmd := press(t, schema, "enter")
	remembered := tea.Model(reloading)
	for _, msg := range runAll(t, cmd) {
		remembered, _ = remembered.Update(msg)
	}
	if len(workspace.remembered) == 0 || !strings.HasSuffix(workspace.remembered[0], "/public") {
		t.Errorf("a schema must be written back to the profile: %v", workspace.remembered)
	}
}

func TestAProfileThatCannotBeWrittenIsOnlyAnnounced(t *testing.T) {
	workspace := workspaceWith(t)
	workspace.remember = errors.New("profiles.toml is read only")
	m := browsingCatalog(t, healthy())
	m.workspace = workspace

	reported, cmd := m.Update(rememberedMsg{err: workspace.remember})
	if cmd == nil {
		t.Fatal("a failed write must be said out loud")
	}
	if !strings.Contains(reported.(Model).text(), "read only") {
		t.Errorf("said = %q", reported.(Model).text())
	}
	if quiet, cmd := m.Update(rememberedMsg{}); cmd != nil || quiet.(Model).text() != "" {
		t.Error("a write that worked says nothing")
	}
}

// The form belongs to the session it was opened on, whatever moves in front of
// it while it is being read.
func TestTheCatalogBelongsToTheSessionItWasOpenedOn(t *testing.T) {
	m, _ := twoConnections(t)
	opened, cmd := press(t, m, "ctrl+d")
	if opened.catalog.on != opened.id {
		t.Fatalf("the form is pinned to %d, in front is %d", opened.catalog.on, opened.id)
	}
	read := pump(t, opened, cmd)
	elsewhere, _ := read.Update(catalogMsg{on: sessionID(404)})
	if elsewhere.(Model).catalog.trouble != "" || len(elsewhere.(Model).catalog.known.databases) == 0 {
		t.Error("a read for another session must not land in this form")
	}
}

// A form over a server that lists nothing has nothing to move through, nothing
// to tick and nothing to save.
func TestAnEmptyCatalogDoesNothing(t *testing.T) {
	m := browsingCatalog(t, healthy())
	m.catalog.known = catalogMsg{}
	if _, ok := m.catalog.selected(); ok {
		t.Error("there is no row")
	}
	if moved, _ := press(t, m, "down"); moved.catalog.cursor != 0 {
		t.Error("there is nothing to move through")
	}
	if picked, _ := press(t, m, " "); picked.catalog.database != m.catalog.database {
		t.Error("there is nothing to tick")
	}
	if !strings.Contains(plain(m.content()), "lists no databases") {
		t.Errorf("content = %s", plain(m.content()))
	}
	if quiet, cmd := press(t, m, "enter"); cmd != nil || quiet.catalog != nil {
		t.Error("saving nothing changes nothing")
	}
}

// The cursor wraps, which is what every list in this program does at its ends.
func TestTheCatalogCursorWraps(t *testing.T) {
	m := browsingCatalog(t, healthy())
	rows := len(m.catalog.rows4Catalog())
	if rows < 2 {
		t.Fatalf("rows = %d", rows)
	}
	up, _ := press(t, m, "up")
	if up.catalog.cursor != rows-1 {
		t.Errorf("cursor = %d, up from the first is the last", up.catalog.cursor)
	}
}

// A form whose session has been disconnected under it saves nothing.
func TestAFormWhoseSessionWentSavesNothing(t *testing.T) {
	m := browsingCatalog(t, healthy())
	m.catalog.on = sessionID(404)
	quiet, cmd := press(t, m, "enter")
	if cmd != nil || quiet.catalog != nil {
		t.Error("there is nothing to save it to")
	}
}

// Only the session in front writes where you left off. A session in the
// background is not where you left off, and two sessions on one profile writing
// at once is each of them undoing the other.
func TestASessionInTheBackgroundDoesNotWriteWhereYouLeftOff(t *testing.T) {
	m, workspace := twoConnections(t)
	background := m.links[0]
	if background.id == m.id {
		t.Fatal("the test needs the other connection in front")
	}
	opened, cmd := press(t, m, "ctrl+d")
	held := pump(t, opened, cmd)
	held.catalog.on = background.id
	held.catalog.schemas = map[string]bool{"public": true}

	applied, _ := press(t, held, "enter")
	settle(t, applied, nil)
	for _, written := range workspace.remembered {
		if strings.HasPrefix(written, background.session.Connection.Name) {
			t.Errorf("a session in the background wrote %q", written)
		}
	}
}
