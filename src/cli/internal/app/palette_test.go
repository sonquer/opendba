package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/sonquer/opendba/src/cli/internal/ui"
)

func opened(t *testing.T) Model {
	t.Helper()
	m := loaded(t, healthy())
	m.width, m.height = 100, 44
	shown, cmd := press(t, m, "/")
	if cmd == nil || shown.palette == nil {
		t.Fatal("/ must open the commands")
	}
	return shown
}

func TestThePaletteListsEveryCommand(t *testing.T) {
	m := opened(t)
	view := plain(m.content())
	for _, want := range []string{"query editor", "tables", "indexes", "connections", "quit"} {
		if !strings.Contains(view, want) {
			t.Errorf("the palette must offer %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "more") {
		t.Errorf("a tall window shows every command:\n%s", view)
	}
	if !strings.Contains(view, "production-eu") {
		t.Error("the palette must sit on top of the screen it was opened from")
	}
}

func TestThePaletteFilters(t *testing.T) {
	m := opened(t)
	for _, key := range []string{"t", "a", "b"} {
		m, _ = press(t, m, key)
	}
	view := plain(m.content())
	if !strings.Contains(view, "tables") || strings.Contains(view, "health report") {
		t.Errorf("the filter must narrow the list:\n%s", view)
	}

	empty := m
	for _, key := range []string{"z", "z"} {
		empty, _ = press(t, empty, key)
	}
	if !strings.Contains(plain(empty.content()), "nothing matches") {
		t.Errorf("content = %s", plain(empty.content()))
	}
	if _, ok := empty.palette.selected(); ok {
		t.Error("nothing can be chosen from an empty list")
	}
	if chosen, cmd := press(t, empty, "enter"); cmd != nil || chosen.palette != nil {
		t.Error("enter on an empty list just closes the palette")
	}
}

func TestThePaletteMoves(t *testing.T) {
	m := opened(t)
	down, _ := press(t, m, "down")
	if down.palette.cursor != 1 {
		t.Fatalf("cursor = %d", down.palette.cursor)
	}
	up, _ := press(t, down, "up")
	if up.palette.cursor != 0 {
		t.Fatalf("cursor = %d", up.palette.cursor)
	}
	wrapped, _ := press(t, up, "up")
	if wrapped.palette.cursor != len(wrapped.palette.items)-1 {
		t.Errorf("the list wraps, got %d", wrapped.palette.cursor)
	}
}

func TestThePaletteRunsWhatWasChosen(t *testing.T) {
	m := opened(t)
	moved := m
	for {
		item, ok := moved.palette.selected()
		if !ok {
			t.Fatal("the palette must offer the tables")
		}
		if item.title == "tables" {
			break
		}
		moved, _ = press(t, moved, "down")
	}
	chosen, cmd := press(t, moved, "enter")
	if chosen.palette != nil {
		t.Fatal("choosing a command closes the palette")
	}
	if cmd == nil {
		t.Fatal("choosing a command must do something")
	}
	switched, _ := chosen.Update(cmd())
	if switched.(Model).view != viewSchema {
		t.Errorf("view = %v", switched.(Model).view)
	}
}

func TestThePaletteCloses(t *testing.T) {
	for _, key := range []string{"esc", "ctrl+k"} {
		closed, cmd := press(t, opened(t), key)
		if closed.palette != nil || cmd != nil {
			t.Errorf("%q must close the palette", key)
		}
	}
}

func TestEveryCommandInThePaletteWorks(t *testing.T) {
	m := opened(t)
	for _, item := range m.palette.items {
		updated, _ := m.Update(item.msg)
		model := updated.(Model)
		switch item.msg.(type) {
		case gotoMsg:
			if model.view == viewDashboard && item.title != "dashboard" {
				t.Errorf("%q did not switch the view", item.title)
			}
		case reloadMsg:
			if !model.loading {
				t.Errorf("%q must read the server again", item.title)
			}
		case newConnectionMsg:
			if model.wizard == nil {
				t.Errorf("%q must open the wizard", item.title)
			}
		case quitMsg:
			if !model.quitting {
				t.Errorf("%q must quit", item.title)
			}
		}
	}
}

func TestThePaletteFitsASmallWindow(t *testing.T) {
	m := opened(t)
	m.width, m.height = 30, 12
	view := plain(m.content())
	if !strings.Contains(view, "query editor") {
		t.Errorf("content = %s", view)
	}
	if !strings.Contains(view, "more") {
		t.Errorf("a short window says how much is hidden:\n%s", view)
	}
	if paletteRows(4) != minPaletteRows {
		t.Errorf("the list keeps a floor, got %d", paletteRows(4))
	}
	if paletteInner(400) != paletteWidth {
		t.Error("the palette does not grow with a wide terminal")
	}
	if paletteInner(10) != ui.TextWidth(10)-6 {
		t.Error("the palette shrinks with the window")
	}
}

func TestThePaletteIsBlindToOtherMessages(t *testing.T) {
	m := opened(t)
	same, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if same.(Model).palette == nil {
		t.Error("resizing must not close the palette")
	}
}

func TestASlashInAStatementIsASlash(t *testing.T) {
	m := loaded(t, healthy())
	m.width, m.height = 100, 32
	editing, _ := press(t, m, "e")
	typed, _ := press(t, editing, "/")
	if typed.palette != nil {
		t.Fatal("a slash in the editor is division, not a command list")
	}
	if got := typed.editor.Value(); got != "/" {
		t.Errorf("editor = %q", got)
	}
	commands, _ := press(t, typed, "ctrl+k")
	if commands.palette == nil {
		t.Error("the alias must still reach the commands from the editor")
	}
}
