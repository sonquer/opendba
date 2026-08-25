package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// withWorkspace seeds the directory this connection keeps its statements in and
// reads it, which is what the model does on load.
func withWorkspace(t *testing.T, m Model, files map[string]string) (Model, string) {
	t.Helper()
	root := m.root()
	if root == "" {
		t.Fatal("the test model must have somewhere to keep files")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return settle(t, m, m.readFiles()), root
}

func seeded(t *testing.T) (Model, string) {
	t.Helper()
	return withWorkspace(t, workbench(t), map[string]string{
		"daily.sql":   "SELECT 1",
		"monthly.sql": "SELECT 2",
	})
}

// pressed sends a key and settles the two rounds of messages the file paths
// take, stopping before the clock that takes a sentence back off the screen.
func pressed(t *testing.T, m Model, key string) Model {
	t.Helper()
	m, cmd := press(t, m, key)
	for range 2 {
		messages := runAll(t, cmd)
		var next []tea.Cmd
		for _, msg := range messages {
			updated, produced := m.Update(msg)
			m = updated.(Model)
			if produced != nil {
				next = append(next, produced)
			}
		}
		if len(next) == 0 {
			return m
		}
		cmd = tea.Batch(next...)
	}
	return m
}

// complained sends a key and settles one round, which is where what the program
// said is still to be found: a sentence takes itself off after three seconds,
// and a test that lets the clock run loses it.
func complained(t *testing.T, m Model, key string) Model {
	t.Helper()
	updated, cmd := press(t, m, key)
	return settle(t, updated, cmd)
}

// listed says whether the sidebar has a statement of that name in it.
func listed(m Model, name string) bool {
	for _, item := range m.sidebar.rows {
		if item.key == "file:"+name {
			return true
		}
	}
	return false
}

// reread is the directory read again, which the program does for itself after a
// write and which a test does on its own to keep a clock out of it.
func reread(t *testing.T, m Model) Model {
	t.Helper()
	return settle(t, m, m.readFiles())
}

// onFile puts the sidebar cursor on a named statement.
func onFile(t *testing.T, m Model, name string) Model {
	t.Helper()
	side, _ := press(t, m, "tab")
	for i, item := range side.sidebar.rows {
		if item.key == "file:"+name {
			side.sidebar.cursor = i
			return side
		}
	}
	t.Fatalf("the sidebar has no %q:\n%v", name, side.sidebar.rows)
	return side
}

func TestTheSidebarListsTheSqlFiles(t *testing.T) {
	m, _ := seeded(t)
	view := plain(m.content())
	for _, want := range []string{"TABLES", "users", "FILES", "daily.sql", "monthly.sql"} {
		if !strings.Contains(view, want) {
			t.Errorf("the sidebar must show %q:\n%s", want, view)
		}
	}
	if strings.Index(view, "FILES") < strings.Index(view, "TABLES") {
		t.Error("the files belong under the tables")
	}
}

func TestAnEmptyWorkspaceSaysSo(t *testing.T) {
	m, _ := withWorkspace(t, workbench(t), nil)
	view := plain(m.content())
	if !strings.Contains(view, "FILES") || !strings.Contains(view, "nothing yet") {
		t.Errorf("an empty workspace must still say where files would go:\n%s", view)
	}
}

func TestAWorkspaceThatCannotBeReadSaysSoWithoutLosingTheTables(t *testing.T) {
	m := workbench(t)
	root := m.root()
	if err := os.MkdirAll(filepath.Dir(root), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(root, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	read := settle(t, m, m.readFiles())
	view := plain(read.content())
	if !strings.Contains(view, "cannot be read") {
		t.Errorf("a workspace that is not a directory must say so:\n%s", view)
	}
	if !strings.Contains(view, "users") {
		t.Errorf("the tables must survive it:\n%s", view)
	}
}

func TestAWorkspaceWithNowhereToKeepItSaysSo(t *testing.T) {
	m := workbench(t)
	m.settings.Workspace.Root = ""
	setup := m.workspace.Setup()
	setup.Store.Paths.Data = ""
	m.workspace.(*fakeWorkspace).setup = setup
	read := settle(t, m, m.readFiles())
	if !strings.Contains(plain(read.content()), "nowhere to keep them") {
		t.Errorf("content = %s", plain(read.content()))
	}
	saved, cmd := read.saveSheet()
	if cmd == nil || saved.(Model).modal != nil {
		t.Error("saving with nowhere to keep it must complain rather than ask for a name")
	}
}

func TestTheCursorRunsFromTheTablesIntoTheFiles(t *testing.T) {
	m, _ := seeded(t)
	side, _ := press(t, m, "tab")
	reached := false
	for range len(side.sidebar.rows) {
		side, _ = press(t, side, "down")
		if item, ok := side.sidebar.selected(); ok && item.section == sectionFiles {
			reached = true
			break
		}
	}
	if !reached {
		t.Fatal("down must reach the files")
	}
}

func TestAFileRowInsertsNothing(t *testing.T) {
	m, _ := seeded(t)
	side := onFile(t, m, "daily.sql")
	after, _ := press(t, side, "i")
	if after.editor.Value() != "" {
		t.Errorf("a file name is not part of a statement, got %q", after.editor.Value())
	}
}

func TestEnterOnAFileOpensItInATabOfItsOwn(t *testing.T) {
	m, _ := seeded(t)
	side := onFile(t, m, "daily.sql")
	opened := pressed(t, side, "enter")
	if opened.kind != sheetFile || opened.file != "daily.sql" {
		t.Fatalf("the tab is %v %q", opened.kind, opened.file)
	}
	if opened.editor.Value() != "SELECT 1" {
		t.Errorf("the tab must hold the file, got %q", opened.editor.Value())
	}
	if opened.dirty() {
		t.Error("a file just opened has not changed")
	}
	if !strings.Contains(plain(opened.content()), "daily.sql") {
		t.Error("the tab must be named after the file")
	}
}

func TestAFileThatIsAlreadyOpenIsFocusedRatherThanOpenedTwice(t *testing.T) {
	m, _ := seeded(t)
	first := pressed(t, onFile(t, m, "daily.sql"), "enter")
	second := pressed(t, onFile(t, first, "monthly.sql"), "enter")
	tabs := len(second.sheets)
	again := pressed(t, onFile(t, second, "daily.sql"), "enter")
	if len(again.sheets) != tabs {
		t.Errorf("a file already open must not open twice, tabs went %d to %d", tabs, len(again.sheets))
	}
	if again.file != "daily.sql" {
		t.Errorf("the tab in front is %q", again.file)
	}
}

func TestAFileThatWentAwayIsSaidRatherThanOpened(t *testing.T) {
	m, root := seeded(t)
	side := onFile(t, m, "daily.sql")
	if err := os.Remove(filepath.Join(root, "daily.sql")); err != nil {
		t.Fatal(err)
	}
	after := complained(t, side, "enter")
	if len(after.sheets) != 1 {
		t.Errorf("a file that is not there must not open a tab, got %d", len(after.sheets))
	}
	if len(after.notes) == 0 {
		t.Error("a file that is not there must be said")
	}
	if listed(reread(t, after), "daily.sql") {
		t.Error("the list must stop showing what is gone")
	}
}

func TestSavingATabWithAFileWritesIt(t *testing.T) {
	m, root := seeded(t)
	side := onFile(t, m, "daily.sql")
	opened := pressed(t, side, "enter")
	typed := typeInto(t, opened, " WHERE x")
	if !typed.dirty() {
		t.Fatal("a tab that has been typed into is not what its file holds")
	}
	saved := pressed(t, typed, "ctrl+s")
	held, err := os.ReadFile(filepath.Join(root, "daily.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if string(held) != typed.editor.Value() {
		t.Errorf("the file holds %q, want %q", held, typed.editor.Value())
	}
	if saved.dirty() {
		t.Error("a tab that has just been written matches its file")
	}
}

func TestSavingATabWithNoFileAsksForAName(t *testing.T) {
	m, root := seeded(t)
	typed := typeInto(t, m, "SELECT 3")
	asked := pressed(t, typed, "ctrl+s")
	if asked.modal == nil || !asked.modal.typing() {
		t.Fatal("a tab with no file must be asked what to call it")
	}
	named := typeInto(t, asked, "fresh")
	written := pressed(t, named, "enter")
	held, err := os.ReadFile(filepath.Join(root, "fresh.sql"))
	if err != nil {
		t.Fatalf("the file was not written: %v", err)
	}
	if string(held) != "SELECT 3" {
		t.Errorf("the file holds %q", held)
	}
	if written.file != "fresh.sql" || written.kind != sheetFile {
		t.Errorf("the tab is %q %v", written.file, written.kind)
	}
}

// Saving under a name that is already taken asks whether to write over it. The
// name was typed on purpose, so refusing outright answered a question nobody
// had asked and left the statement nowhere.
func TestSavingOverAFileThatIsAlreadyThereAsksFirst(t *testing.T) {
	m, root := seeded(t)
	typed := typeInto(t, m, "SELECT 3")
	asked := pressed(t, typed, "ctrl+s")
	named := typeInto(t, asked, "daily")
	questioned := pressed(t, named, "enter")

	if questioned.modal == nil {
		t.Fatal("a name that is taken must be asked about")
	}
	view := plain(questioned.modal.view(80))
	if !strings.Contains(view, "overwrite daily.sql?") {
		t.Errorf("dialog = %s", view)
	}
	if !questioned.modal.danger {
		t.Error("and asked in the colour of something that costs")
	}
	held, err := os.ReadFile(filepath.Join(root, "daily.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if string(held) != "SELECT 1" {
		t.Errorf("nothing is written until it is answered, holds %q", held)
	}

	left := pressed(t, questioned, "esc")
	if left.modal != nil || left.file != "" {
		t.Error("esc leaves the file and the tab alone")
	}

	written := pressed(t, questioned, "enter")
	after, err := os.ReadFile(filepath.Join(root, "daily.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != "SELECT 3" {
		t.Errorf("enter writes over it, holds %q", after)
	}
	if written.file != "daily.sql" || written.kind != sheetFile {
		t.Errorf("and the tab is now that file: %q %v", written.file, written.kind)
	}
}

func TestANameThatIsAPathIsRefusedOnSave(t *testing.T) {
	m, root := seeded(t)
	typed := typeInto(t, m, "SELECT 3")
	asked := pressed(t, typed, "ctrl+s")
	named := typeInto(t, asked, "../escape")
	written := pressed(t, named, "enter")
	if _, err := os.Stat(filepath.Join(filepath.Dir(root), "escape.sql")); err == nil {
		t.Fatal("a name that is a path must not write outside the workspace")
	}
	if written.file != "" {
		t.Error("a refused save must not claim the file")
	}
}

func TestDeletingAFileAsksFirst(t *testing.T) {
	m, root := seeded(t)
	side := onFile(t, m, "daily.sql")
	asked, _ := press(t, side, "ctrl+x")
	if asked.modal == nil {
		t.Fatal("removing a file must be asked about first")
	}
	kept, _ := press(t, asked, "esc")
	if _, err := os.Stat(filepath.Join(root, "daily.sql")); err != nil {
		t.Fatalf("esc must leave the file alone: %v", err)
	}
	again, _ := press(t, kept, "ctrl+x")
	removed := pressed(t, again, "enter")
	if _, err := os.Stat(filepath.Join(root, "daily.sql")); err == nil {
		t.Error("enter must remove the file")
	}
	if listed(reread(t, removed), "daily.sql") {
		t.Error("the list must stop showing what is gone")
	}
}

func TestRemovingAFileThatIsNotThereIsSaid(t *testing.T) {
	m, root := seeded(t)
	side := onFile(t, m, "daily.sql")
	asked, _ := press(t, side, "ctrl+x")
	if err := os.Remove(filepath.Join(root, "daily.sql")); err != nil {
		t.Fatal(err)
	}
	after := pressed(t, asked, "enter")
	if len(after.notes) == 0 {
		t.Error("a removal that failed must be said")
	}
}

func TestTheFilesAreReadAgainAfterASave(t *testing.T) {
	m, _ := withWorkspace(t, workbench(t), nil)
	typed := typeInto(t, m, "SELECT 3")
	asked := pressed(t, typed, "ctrl+s")
	named := typeInto(t, asked, "fresh")
	written := reread(t, pressed(t, named, "enter"))
	if !strings.Contains(plain(written.content()), "fresh.sql") {
		t.Errorf("the list must show what was just written:\n%s", plain(written.content()))
	}
}
