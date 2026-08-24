package app

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sonquer/tui4db/src/cli/internal/config"
	"github.com/sonquer/tui4db/src/cli/internal/history"
	"github.com/sonquer/tui4db/src/cli/internal/ui"
)

// keepingHistory builds a model that keeps what it runs, in a store of its own.
func keepingHistory(t *testing.T, settings config.HistorySettings) Model {
	t.Helper()
	store, err := history.Open(filepath.Join(t.TempDir(), "history.db"), settings)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	m := loadedWith(t, healthy(), workspaceWith(t))
	m.width, m.height = 110, 34
	m.session.History = store
	m.session.Settings.History = settings
	return m
}

func kept(t *testing.T, m Model) []history.Entry {
	t.Helper()
	entries, err := m.session.History.Recent(context.Background(), "", 50)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	return entries
}

// What has been run is written down, with what it did.
func TestWhatHasBeenRunIsWrittenDown(t *testing.T) {
	m := keepingHistory(t, config.HistorySettings{Enabled: true, StoreSQL: true, Limit: 50})
	editing, _ := press(t, m, "e")
	typed := typeInto(t, editing, "SELECT 1")
	ran, cmd := press(t, typed, "ctrl+r")
	pump(t, ran, cmd)

	entries := kept(t, m)
	if len(entries) != 1 {
		t.Fatalf("entries = %d", len(entries))
	}
	if entries[0].Statement != "SELECT 1" {
		t.Errorf("statement = %q", entries[0].Statement)
	}
	if entries[0].Rows != 2 || !entries[0].Succeeded() {
		t.Errorf("entry = %+v, it must say what the statement did", entries[0])
	}
	if entries[0].ConnectionName == "" {
		t.Error("and which connection it was run against")
	}
}

// A profile that says not to keep the statement keeps that it ran and no more.
func TestAStatementCanBeKeptWithoutKeepingTheStatement(t *testing.T) {
	m := keepingHistory(t, config.HistorySettings{Enabled: true, StoreSQL: false, Limit: 50})
	editing, _ := press(t, m, "e")
	typed := typeInto(t, editing, "SELECT 1")
	ran, cmd := press(t, typed, "ctrl+r")
	pump(t, ran, cmd)

	entries := kept(t, m)
	if len(entries) != 1 {
		t.Fatalf("entries = %d", len(entries))
	}
	if entries[0].Statement != history.Redacted() {
		t.Errorf("statement = %q, it was not meant to be kept", entries[0].Statement)
	}
}

// Nothing is written down when there is nowhere to write it.
func TestNothingIsWrittenDownWithNowhereToWriteIt(t *testing.T) {
	m := loadedWith(t, healthy(), workspaceWith(t))
	if m.keeping() {
		t.Fatal("there is no store")
	}
	if cmd := m.wroteDown(queriedMsg{statement: "SELECT 1"}); cmd != nil {
		t.Error("nothing to write it to means nothing to do")
	}
	kept := keepingHistory(t, config.HistorySettings{Enabled: true})
	if cmd := kept.wroteDown(queriedMsg{statement: "  "}); cmd != nil {
		t.Error("and an empty statement is not a statement")
	}
}

// The screen lists what has been run, searches it, and says when there is
// nothing there.
func TestTheHistoryScreenListsAndSearches(t *testing.T) {
	m := keepingHistory(t, config.HistorySettings{Enabled: true, StoreSQL: true, Limit: 50})
	for i, statement := range []string{"SELECT 1", "SELECT * FROM users"} {
		editing, _ := press(t, m, "e")
		if i > 0 {
			editing, _ = press(t, editing, "ctrl+n")
		}
		typed := typeInto(t, editing, statement)
		ran, cmd := press(t, typed, "ctrl+r")
		m = pump(t, ran, cmd)
	}

	opened, cmd := m.show(viewHistory)
	shown := settle(t, opened.(Model), cmd)
	view := plain(shown.content())
	for _, want := range []string{"HISTORY", "SELECT 1", "SELECT * FROM users", "type to search"} {
		if !strings.Contains(view, want) {
			t.Errorf("the screen must show %q:\n%s", want, view)
		}
	}

	searched := shown
	for _, key := range []string{"u", "s", "e", "r"} {
		next, cmd := press(t, searched, key)
		searched = settle(t, next, cmd)
	}
	found := plain(searched.content())
	if !strings.Contains(found, "SELECT * FROM users") || strings.Contains(found, "▌ SELECT 1") {
		t.Errorf("searching must narrow it:\n%s", found)
	}

	cleared, cmd := press(t, searched, "esc")
	back := settle(t, cleared, cmd)
	if back.view != viewHistory || back.recall.term != "" {
		t.Error("esc clears the search before it leaves the screen")
	}
	away, _ := press(t, back, "esc")
	if away.view != viewDashboard {
		t.Errorf("view = %v, and then it leaves", away.view)
	}
}

// Enter puts a statement back in a tab of its own, so nothing being written is
// lost to going back for something older.
func TestAStatementComesBackInATabOfItsOwn(t *testing.T) {
	m := keepingHistory(t, config.HistorySettings{Enabled: true, StoreSQL: true, Limit: 50})
	editing, _ := press(t, m, "e")
	typed := typeInto(t, editing, "SELECT * FROM users")
	ran, cmd := press(t, typed, "ctrl+r")
	m = pump(t, ran, cmd)
	writing := typeInto(t, m, " WHERE id = 1")

	opened, cmd := writing.show(viewHistory)
	shown := settle(t, opened.(Model), cmd)
	reopened, cmd := press(t, shown, "enter")
	back := settle(t, reopened, cmd)

	if back.view != viewQuery {
		t.Fatalf("view = %v", back.view)
	}
	if len(back.sheets) != 2 {
		t.Fatalf("tabs = %d, it must open a tab of its own", len(back.sheets))
	}
	if back.statement() != "SELECT * FROM users" {
		t.Errorf("statement = %q", back.statement())
	}
	if back.sheets[0].editor.Value() != "SELECT * FROM users WHERE id = 1" {
		t.Errorf("what was being written must be untouched: %q",
			back.sheets[0].editor.Value())
	}
}

// A statement that was not kept cannot be brought back, and says so.
func TestAStatementThatWasNotKeptCannotComeBack(t *testing.T) {
	m := keepingHistory(t, config.HistorySettings{Enabled: true, StoreSQL: false, Limit: 50})
	editing, _ := press(t, m, "e")
	typed := typeInto(t, editing, "SELECT 1")
	ran, cmd := press(t, typed, "ctrl+r")
	m = pump(t, ran, cmd)

	opened, cmd := m.show(viewHistory)
	shown := settle(t, opened.(Model), cmd)
	refused, cmd := press(t, shown, "enter")
	if cmd == nil {
		t.Fatal("it has to say why not")
	}
	if !strings.Contains(refused.text(), "was not kept") {
		t.Errorf("text = %q", refused.text())
	}
}

// Space keeps a statement past the point the rest are trimmed away.
func TestSpaceKeepsAStatement(t *testing.T) {
	m := keepingHistory(t, config.HistorySettings{Enabled: true, StoreSQL: true, Limit: 50})
	editing, _ := press(t, m, "e")
	typed := typeInto(t, editing, "SELECT 1")
	ran, cmd := press(t, typed, "ctrl+r")
	m = pump(t, ran, cmd)

	opened, cmd := m.show(viewHistory)
	shown := settle(t, opened.(Model), cmd)
	favoured, cmd := press(t, shown, "space")
	pump(t, favoured, cmd)

	entries := kept(t, m)
	if len(entries) != 1 || !entries[0].Favorite {
		t.Errorf("entry = %+v, space must keep it", entries)
	}
}

// With the history switched off the screen says so rather than looking broken.
func TestTheHistoryScreenSaysWhenItIsSwitchedOff(t *testing.T) {
	m := loadedWith(t, healthy(), workspaceWith(t))
	m.width, m.height = 110, 34
	opened, cmd := m.show(viewHistory)
	if cmd != nil {
		t.Error("there is nothing to read")
	}
	view := plain(opened.(Model).content())
	for _, want := range []string{"HISTORY", "switched off", "history.enabled"} {
		if !strings.Contains(view, want) {
			t.Errorf("the screen must show %q:\n%s", want, view)
		}
	}
	moved, _ := press(t, opened.(Model), "down")
	if moved.recall.cursor != 0 {
		t.Error("and there is nothing to move through")
	}
}

// The list says what each statement did, in the words somebody looking for it
// would remember.
func TestTheListSaysWhatEachStatementDid(t *testing.T) {
	at := time.Date(2026, 8, 23, 14, 5, 0, 0, time.UTC)
	list := newRecall(ui.Default())
	for _, want := range []struct {
		name  string
		entry history.Entry
		said  []string
	}{
		{"it worked", history.Entry{RanAt: at, Rows: 3}, []string{"14:05", "3 rows"}},
		{"it failed", history.Entry{RanAt: at, Failure: "boom"}, []string{"14:05", "failed"}},
		{"it is kept", history.Entry{RanAt: at, Rows: 1, Favorite: true},
			[]string{"1 row", "kept"}},
	} {
		t.Run(want.name, func(t *testing.T) {
			note := list.note(want.entry)
			for _, phrase := range want.said {
				if !strings.Contains(note, phrase) {
					t.Errorf("note = %q, want something about %q", note, phrase)
				}
			}
		})
	}
}

// A history that cannot be read says why rather than looking empty.
func TestAHistoryThatCannotBeReadSaysWhy(t *testing.T) {
	m := keepingHistory(t, config.HistorySettings{Enabled: true, StoreSQL: true, Limit: 50})
	broken, _ := m.Update(recalledMsg{err: errors.New("the file is locked")})
	shown := broken.(Model)
	shown.view = viewHistory
	if !strings.Contains(plain(shown.content()), "the file is locked") {
		t.Errorf("the screen must say what went wrong:\n%s", plain(shown.content()))
	}
	mended, _ := shown.Update(recalledMsg{entries: []history.Entry{{Statement: "SELECT 1"}}})
	if mended.(Model).recall.trouble != "" {
		t.Error("and stop saying it once it works")
	}
}

// A history with nothing in it says so.
func TestAnEmptyHistorySaysSo(t *testing.T) {
	m := keepingHistory(t, config.HistorySettings{Enabled: true, StoreSQL: true, Limit: 50})
	opened, cmd := m.show(viewHistory)
	shown := settle(t, opened.(Model), cmd)
	if !strings.Contains(plain(shown.content()), "nothing here") {
		t.Errorf("an empty history says so:\n%s", plain(shown.content()))
	}
	if _, ok := shown.recall.selected(); ok {
		t.Error("and has nothing to open")
	}
	if moved := shown.recall.move(1); moved.cursor != 0 {
		t.Error("and nothing to move through")
	}
	refused, _ := press(t, shown, "enter")
	if refused.view != viewHistory {
		t.Error("enter on nothing goes nowhere")
	}
	if _, cmd := shown.favourite(); cmd != nil {
		t.Error("and nothing to keep")
	}
}

// Backspace takes the search back a letter, and does nothing when there is
// nothing to take back.
func TestBackspaceTakesTheSearchBack(t *testing.T) {
	m := keepingHistory(t, config.HistorySettings{Enabled: true, StoreSQL: true, Limit: 50})
	opened, cmd := m.show(viewHistory)
	shown := settle(t, opened.(Model), cmd)

	empty, cmd := press(t, shown, "backspace")
	if cmd != nil || empty.recall.term != "" {
		t.Error("there is nothing to take back yet")
	}
	typed, cmd := press(t, shown, "u")
	typed = settle(t, typed, cmd)
	if typed.recall.term != "u" {
		t.Fatalf("term = %q", typed.recall.term)
	}
	back, cmd := press(t, typed, "backspace")
	back = settle(t, back, cmd)
	if back.recall.term != "" {
		t.Errorf("term = %q, backspace must take it back", back.recall.term)
	}
	ignored, _ := press(t, back, "f5")
	if ignored.recall.term != "" {
		t.Error("a key that is not a letter is not searched for")
	}
}
