package app

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/sonquer/tui4db/src/cli/internal/ui"
)

func TestEveryWayToRunAStatement(t *testing.T) {
	keys := newKeymap()
	for _, name := range []string{"ctrl+enter", "super+enter", "ctrl+r", "f5"} {
		if !key.Matches(keyMsg(name), keys.Run) {
			t.Errorf("%q must run the statement", name)
		}
	}
	if key.Matches(keyMsg("enter"), keys.Run) {
		t.Error("a bare enter belongs to the editor")
	}
}

func TestTheAdvertisedRunKeyFollowsTheTerminal(t *testing.T) {
	keys := newKeymap()
	if keys.Run.Help().Key != ui.Keystroke("ctrl+r") {
		t.Errorf("without enhancements the footer must offer a key every terminal sends, got %q", keys.Run.Help().Key)
	}
	if !strings.Contains(keys.commandNote(), ui.Keystroke("ctrl+r")) {
		t.Errorf("note = %q", keys.commandNote())
	}

	enhanced := keys.withEnhancements(tea.KeyboardEnhancementsMsg{Flags: 1})
	if enhanced.Run.Help().Key != ui.Keystroke("ctrl+enter") {
		t.Errorf("with enhancements the footer must offer ctrl+enter, got %q", enhanced.Run.Help().Key)
	}
	if !strings.Contains(enhanced.commandNote(), ui.Keystroke("super+enter")) {
		t.Errorf("the command key must be named where it works: %q", enhanced.commandNote())
	}
	for _, name := range []string{"ctrl+enter", "ctrl+r"} {
		if !key.Matches(keyMsg(name), enhanced.Run) {
			t.Errorf("%q must keep working", name)
		}
	}
}

func TestTheKeymapIsTheOnlySourceOfHelp(t *testing.T) {
	keys := newKeymap()
	for _, group := range keys.FullHelp() {
		for _, binding := range group {
			if binding.Help().Key == "" || binding.Help().Desc == "" {
				t.Errorf("every key must explain itself: %+v", binding.Help())
			}
			if len(binding.Keys()) == 0 {
				t.Errorf("%q is advertised but bound to nothing", binding.Help().Desc)
			}
		}
	}
	for _, screen := range []view{viewDashboard, viewQuery, viewSwitch, viewCatalog, viewHelp, viewSchema} {
		footer := keys.footer(screen, false)
		if len(footer) == 0 {
			t.Errorf("%s has no footer", screen)
		}
		if len(footer.FullHelp()) != 1 {
			t.Errorf("%s renders one row of keys", screen)
		}
		if screen == viewDashboard {
			continue
		}
		if !offers(footer, "dashboard") {
			t.Errorf("%s must offer the way back: %+v", screen, footer)
		}
	}
	if completing := keys.footer(viewQuery, true); completing[0].Help().Desc != "accept" {
		t.Errorf("a list of suggestions owns the keys: %+v", completing[0].Help())
	}
}

func offers(footer screenKeys, desc string) bool {
	for _, binding := range footer {
		if binding.Help().Desc == desc {
			return true
		}
	}
	return false
}

func TestTheModelTakesTheTerminalsWord(t *testing.T) {
	m := loaded(t, healthy())
	updated, cmd := m.Update(tea.KeyboardEnhancementsMsg{Flags: 1})
	if cmd != nil {
		t.Error("learning about the terminal changes nothing else")
	}
	shown := updated.(Model)
	if !shown.keys.enhanced {
		t.Fatal("the model must remember what the terminal can do")
	}
	editing, _ := press(t, shown, "e")
	if !strings.Contains(plain(editing.content()), ui.Keystroke("ctrl+enter")) {
		t.Errorf("the footer must follow the terminal:\n%s", plain(editing.content()))
	}
}
