package app

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func browsingCatalog(t *testing.T, conn *fakeConn) Model {
	t.Helper()
	m := loadedWith(t, conn, workspaceWith(t))
	m.width, m.height = 100, 32
	opened, cmd := press(t, m, "ctrl+d")
	if cmd == nil || opened.view != viewCatalog {
		t.Fatal("ctrl+d must open the databases")
	}
	read, _ := opened.Update(cmd())
	return read.(Model)
}

func TestTheCatalogListsDatabasesAndSchemas(t *testing.T) {
	m := browsingCatalog(t, healthy())
	view := plain(m.content())
	for _, want := range []string{"DATABASES", "app ·", "reporting", "SCHEMAS", "public", "1 table"} {
		if !strings.Contains(view, want) {
			t.Errorf("the catalog must show %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "pg_catalog") {
		t.Error("system schemas are noise")
	}
	footer := plain(m.footer(0))
	if !strings.Contains(footer, "open") {
		t.Errorf("footer = %s", footer)
	}
	if strings.Contains(footer, "remove") {
		t.Errorf("a database is not something this program removes: %s", footer)
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
	m := loadedWith(t, healthy(), workspace)
	m.width, m.height = 100, 32
	opened, cmd := press(t, m, "ctrl+d")
	read, _ := opened.Update(cmd())
	m = read.(Model)

	down, _ := press(t, m, "down")
	if _, ok := down.catalog.selected(); !ok {
		t.Fatal("the second database must be selectable")
	}
	switching, cmd := press(t, down, "enter")
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
	schema := m
	for {
		chosen, ok := schema.catalog.selected()
		if !ok {
			t.Fatal("no schema to choose")
		}
		if chosen.section == sectionSchemas {
			break
		}
		schema, _ = press(t, schema, "down")
	}
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
	for _, key := range []string{"esc", "ctrl+d"} {
		back, _ := press(t, browsingCatalog(t, healthy()), key)
		if back.view != viewDashboard {
			t.Errorf("%q must go back, got %s", key, back.view)
		}
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
	switching, cmd := press(t, down, "enter")
	failed, _ := switching.Update(runFirst(t, cmd))
	if !strings.Contains(plain(failed.(Model).content()), "does not exist") {
		t.Errorf("content = %s", plain(failed.(Model).content()))
	}
}

func TestTheQueryEditorReachesTheCatalog(t *testing.T) {
	m := loadedWith(t, healthy(), workspaceWith(t))
	editing, _ := press(t, m, "e")
	opened, cmd := press(t, editing, "ctrl+d")
	if cmd == nil || opened.view != viewCatalog {
		t.Fatalf("view = %s", opened.view)
	}
}

func TestSwitchingIsRemembered(t *testing.T) {
	workspace := workspaceWith(t)
	m := browsingCatalog(t, healthy())
	m.workspace = workspace

	schema := m
	for {
		chosen, ok := schema.catalog.selected()
		if !ok {
			t.Fatal("no schema to choose")
		}
		if chosen.section == sectionSchemas {
			break
		}
		schema, _ = press(t, schema, "down")
	}
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
