package app

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// The database and its schemas are a level inside the connection they belong
// to, so they are reached through the list of connections: ctrl+d opens that,
// and enter on the connection already in use goes in.
func browsingCatalog(t *testing.T, conn *fakeConn) Model {
	t.Helper()
	m := loadedWith(t, conn, workspaceWith(t))
	m.width, m.height = 100, 32
	list, cmd := press(t, m, "ctrl+d")
	if list.view != viewSwitch {
		t.Fatalf("ctrl+d must open the connections, got %s", list.view)
	}
	listed, _ := list.Update(runFirst(t, cmd))
	opened, cmd := press(t, listed.(Model), "enter")
	if cmd == nil || opened.view != viewCatalog {
		t.Fatalf("enter on the connection in use must go in, got %s", opened.view)
	}
	read, _ := opened.Update(cmd())
	return read.(Model)
}

func TestTheCatalogListsDatabasesAndSchemas(t *testing.T) {
	m := browsingCatalog(t, healthy())
	view := plain(m.content())
	for _, want := range []string{
		"DATABASES", "● app", "in use", "○ reporting",
		"SCHEMAS", "several are fine", "▢ public", "1 table",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("the catalog must show %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "pg_catalog") {
		t.Error("system schemas are noise")
	}
	footer := plain(m.footer(0))
	for _, want := range []string{"pick", "save", "cancel"} {
		if !strings.Contains(footer, want) {
			t.Errorf("footer must offer %q: %s", want, footer)
		}
	}
	if strings.Contains(footer, "remove") {
		t.Errorf("a database is not something this program removes: %s", footer)
	}
}

// onSchema walks the form down to its first schema, which is where the ticking
// happens.
func onSchema(t *testing.T, m Model) Model {
	t.Helper()
	for range len(m.catalog.rows) {
		chosen, ok := m.catalog.selected()
		if ok && chosen.section == sectionSchemas {
			return m
		}
		m, _ = press(t, m, "down")
	}
	t.Fatal("the form has no schema to tick")
	return m
}

func TestTheFormTicksSeveralSchemas(t *testing.T) {
	m := browsingCatalog(t, twoSchemas())
	ticked := onSchema(t, m)
	ticked, cmd := press(t, ticked, " ")
	if cmd != nil {
		t.Error("ticking a box reads nothing and reconnects to nothing")
	}
	if ticked.view != viewCatalog {
		t.Error("the form stays open until it is saved or left")
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
	if cmd == nil || !saved.loading || saved.view != viewDashboard {
		t.Fatalf("enter must save the form: loading=%v view=%s", saved.loading, saved.view)
	}
	if got := saved.session.Connection.Filter(); !reflect.DeepEqual(got, []string{"public"}) {
		t.Errorf("filter = %v", got)
	}
}

func TestTheFormReadsEveryTickedSchema(t *testing.T) {
	m := browsingCatalog(t, twoSchemas())
	m.session.Connection.Schemas = []string{"public", "reporting"}
	loaded := runFirst(t, m.readCatalogue())
	msg, ok := loaded.(loadedMsg)
	if !ok {
		t.Fatalf("msg = %T", loaded)
	}
	seen := map[string]bool{}
	for _, table := range msg.tables {
		seen[table.Schema] = true
	}
	if !seen["public"] || !seen["reporting"] {
		t.Errorf("both schemas must be read: %v", seen)
	}

	m.session.Connection.Schemas = []string{"reporting"}
	msg = runFirst(t, m.readCatalogue()).(loadedMsg)
	for _, table := range msg.tables {
		if table.Schema != "reporting" {
			t.Errorf("one schema means one schema: %+v", table)
		}
	}
}

func TestTheCatalogReportsWhatItCannotRead(t *testing.T) {
	conn := healthy()
	conn.failOn = "databases"
	m := browsingCatalog(t, conn)
	if !strings.Contains(plain(m.content()), "databases failed") {
		t.Errorf("content = %s", plain(m.content()))
	}

	conn = healthy()
	conn.failOn = "schemas"
	m = browsingCatalog(t, conn)
	if !strings.Contains(plain(m.content()), "schemas failed") {
		t.Errorf("content = %s", plain(m.content()))
	}
}

func TestSwitchingDatabaseReconnects(t *testing.T) {
	workspace := workspaceWith(t)
	m := browsingCatalog(t, healthy())
	m.workspace = workspace

	down, _ := press(t, m, "down")
	if _, ok := down.catalog.selected(); !ok {
		t.Fatal("the second database must be selectable")
	}
	picked, _ := press(t, down, " ")
	if picked.catalog.database != "reporting" {
		t.Fatalf("space must move the dot: %q", picked.catalog.database)
	}
	switching, cmd := press(t, picked, "enter")
	if cmd == nil || !switching.loading || switching.view != viewDashboard {
		t.Fatalf("choosing a database must reconnect: loading=%v view=%s", switching.loading, switching.view)
	}
	switched, _ := switching.Update(runFirst(t, cmd))
	if got := switched.(Model).session.Info.Database; got != "reporting" {
		t.Errorf("database = %q", got)
	}
	if !strings.Contains(plain(switched.(Model).content()), "reporting") {
		t.Error("the header must name the database in use")
	}
}

func TestSwitchingSchemaJustReloads(t *testing.T) {
	m := browsingCatalog(t, healthy())
	schema, _ := press(t, onSchema(t, m), " ")
	reloading, cmd := press(t, schema, "enter")
	if cmd == nil || !reloading.loading {
		t.Fatal("a schema change must read the server again")
	}
	if reloading.session.Connection.DefaultSchema != "public" {
		t.Errorf("schema = %q", reloading.session.Connection.DefaultSchema)
	}
	if !strings.Contains(plain(reloading.content()), "reading public") {
		t.Errorf("the change must be announced:\n%s", plain(reloading.content()))
	}
	tables, _ := press(t, reloading, "s")
	if !strings.Contains(plain(tables.content()), "public") {
		t.Errorf("the table list must name the schema it read:\n%s", plain(tables.content()))
	}
}

func TestTheCatalogLeavesTheCurrentPlaceAlone(t *testing.T) {
	m := browsingCatalog(t, healthy())
	same, cmd := press(t, m, "enter")
	if cmd != nil || same.loading || same.view != viewDashboard {
		t.Error("choosing what is already open does nothing")
	}

	empty := browsingCatalog(t, healthy())
	empty.catalog.picker = empty.catalog.withRows(nil)
	if _, cmd := press(t, empty, " "); cmd != nil {
		t.Error("there is nothing to tick")
	}
	if _, cmd := press(t, empty, "enter"); cmd != nil {
		t.Error("there is nothing to choose")
	}
	if moved, _ := press(t, empty, "down"); moved.catalog.cursor != 0 {
		t.Error("there is nothing to move through")
	}
	if !strings.Contains(plain(empty.content()), "reading the server") {
		t.Errorf("content = %s", plain(empty.content()))
	}
}

func TestLeavingTheCatalog(t *testing.T) {
	back, _ := press(t, browsingCatalog(t, healthy()), "esc")
	if back.view != viewSwitch {
		t.Errorf("esc goes back a level, to the connection this belongs to, got %s", back.view)
	}
	out, _ := press(t, browsingCatalog(t, healthy()), "ctrl+d")
	if out.view != viewDashboard {
		t.Errorf("ctrl+d closes the whole thing, got %s", out.view)
	}
	asked, _ := press(t, browsingCatalog(t, healthy()), "q")
	if asked.modal == nil {
		t.Error("q must ask before it leaves")
	}
	if same, cmd := press(t, browsingCatalog(t, healthy()), "x"); cmd != nil || same.view != viewCatalog {
		t.Error("an unknown key does nothing")
	}
}

func TestAFailedDatabaseSwitchKeepsTheOldOne(t *testing.T) {
	workspace := workspaceWith(t)
	workspace.open = errors.New("database \"reporting\" does not exist")
	m := browsingCatalog(t, healthy())
	m.workspace = workspace
	down, _ := press(t, m, "down")
	picked, _ := press(t, down, " ")
	switching, cmd := press(t, picked, "enter")
	failed, _ := switching.Update(runFirst(t, cmd))
	if !strings.Contains(plain(failed.(Model).content()), "does not exist") {
		t.Errorf("content = %s", plain(failed.(Model).content()))
	}
}

func TestTheQueryEditorReachesTheConnections(t *testing.T) {
	m := loadedWith(t, healthy(), workspaceWith(t))
	editing, _ := press(t, m, "e")
	opened, cmd := press(t, editing, "ctrl+d")
	if cmd == nil || opened.view != viewSwitch {
		t.Fatalf("view = %s", opened.view)
	}
}

func TestSwitchingIsRemembered(t *testing.T) {
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
	if remembered.(Model).session.Connection.DefaultSchema != "public" {
		t.Error("the session must be where it was asked to be")
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
	if !strings.Contains(plain(reported.(Model).content()), "read only") {
		t.Errorf("content = %s", plain(reported.(Model).content()))
	}
	if quiet, cmd := m.Update(rememberedMsg{}); cmd != nil || quiet.(Model).text != "" {
		t.Error("a write that worked says nothing")
	}
}
