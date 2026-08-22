package app

import (
	"strings"
	"testing"

	"github.com/sonquer/tui4db/src/cli/internal/ui"
)

func TestThePickerMovesAndWraps(t *testing.T) {
	list := newPicker(ui.Default(), "nothing here").withRows([]row{
		{key: "a", label: "alpha"},
		{key: "b", label: "beta"},
	})
	if _, ok := list.selected(); !ok {
		t.Fatal("the first row is selected")
	}
	if list.move(1).cursor != 1 {
		t.Error("down must move")
	}
	if list.move(-1).cursor != 1 {
		t.Error("up from the first row must wrap to the last")
	}
	if list.move(2).cursor != 0 {
		t.Error("a full turn comes back")
	}
	if got := list.at("b"); got.cursor != 1 {
		t.Errorf("at() must find a row by its key: %d", got.cursor)
	}
	if got := list.at("nope"); got.cursor != list.cursor {
		t.Error("at() leaves the cursor alone when the key is gone")
	}
}

func TestThePickerHandlesAnEmptyList(t *testing.T) {
	list := newPicker(ui.Default(), "nothing here")
	if _, ok := list.selected(); ok {
		t.Error("an empty list has nothing selected")
	}
	if list.move(1).cursor != 0 {
		t.Error("an empty list does not move")
	}
	if !strings.Contains(plain(list.view(40)), "nothing here") {
		t.Errorf("view = %q", plain(list.view(40)))
	}
}

func TestThePickerGroupsAndMarks(t *testing.T) {
	list := newPicker(ui.Default(), "").withRows([]row{
		{key: "1", label: "app", section: "databases", current: true},
		{key: "2", label: "reporting", section: "databases", note: "cold"},
		{key: "3", label: "public", section: "schemas", depth: 1},
	})
	view := plain(list.view(40))
	if !strings.Contains(view, "DATABASES") || !strings.Contains(view, "SCHEMAS") {
		t.Errorf("sections must announce themselves:\n%s", view)
	}
	if strings.Count(view, "DATABASES") != 1 {
		t.Error("a section announces itself once")
	}
	if !strings.Contains(view, "app ·") {
		t.Errorf("what is in use must be marked:\n%s", view)
	}
	if !strings.Contains(view, "cold") {
		t.Errorf("a note belongs on its row:\n%s", view)
	}
	if !strings.Contains(view, "▌") {
		t.Errorf("the cursor must be visible:\n%s", view)
	}
}

func TestThePickerShrinksTheCursorWithTheList(t *testing.T) {
	list := newPicker(ui.Default(), "").withRows([]row{{key: "a"}, {key: "b"}, {key: "c"}})
	list.cursor = 2
	shorter := list.withRows([]row{{key: "a"}})
	if shorter.cursor != 0 {
		t.Errorf("cursor = %d", shorter.cursor)
	}
	if empty := list.withRows(nil); empty.cursor != 0 {
		t.Errorf("cursor = %d", empty.cursor)
	}
}
