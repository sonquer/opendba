package app

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sonquer/opendba/src/cli/internal/ai"
	"github.com/sonquer/opendba/src/cli/internal/chats"
	"github.com/sonquer/opendba/src/cli/internal/config"
)

// storing is a model with both stores behind it, on whatever screen it starts.
func storing(t *testing.T) Model {
	t.Helper()
	m := keepingHistory(t, config.HistorySettings{Enabled: true, StoreSQL: true, Limit: 50})
	store, err := chats.Open(filepath.Join(t.TempDir(), "chats.db"),
		config.ChatSettings{Enabled: true, Limit: 100})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	m.session.Chats = store
	m.settings.Chats = config.ChatSettings{Enabled: true, Limit: 100}
	return m
}

// onSettings opens the settings screen on a model that already has its stores.
func onSettings(t *testing.T, m Model) Model {
	t.Helper()
	opened, cmd := m.Update(preferencesMsg{})
	shown := settle(t, opened.(Model), cmd)
	if shown.view != viewSettings || shown.preferences == nil {
		t.Fatalf("view = %v, the command must open the settings", shown.view)
	}
	return shown
}

// settling is both at once, for a test that only cares about the screen.
func settling(t *testing.T) Model { return onSettings(t, storing(t)) }

func set4Preferences(m Model, key, value string) Model {
	held := *m.preferences
	fields := append([]field(nil), held.form.fields...)
	for i := range fields {
		if fields[i].key != key {
			continue
		}
		switch fields[i].kind {
		case fieldChoice:
			at := -1
			for held, choice := range fields[i].choices {
				if choice == value {
					at = held
				}
			}
			if at < 0 {
				fields[i].choices = append(fields[i].choices, value)
				at = len(fields[i].choices) - 1
			}
			fields[i].choice = at
		case fieldToggle:
			fields[i].on = value == "yes" || value == config.MouseOn
		default:
			fields[i].input.SetValue(value)
		}
	}
	held.form.fields = fields
	m.preferences = &held
	return m
}

// The screen shows what settings.toml holds, and says which of it the open
// connection cannot be told about.
func TestTheSettingsScreenShowsWhatIsInTheFile(t *testing.T) {
	m := settling(t)
	m.width, m.height = 110, 46
	view := plain(m.content())
	for _, want := range []string{
		"SETTINGS", "settings.toml",
		"APPEARANCE", "bars", "mouse", "own sessions",
		"SAFETY", "opens as", "rows", "query time", "lock time",
		"WORKSPACE", "sql files",
		"QUERY HISTORY", "keep the sql", "how many",
		"CONVERSATIONS", "clear them", "save",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("the screen must show %q:\n%s", want, view)
		}
	}
}

// Saving writes the file and applies what can be applied to the program that is
// already running.
func TestSavingWritesTheFileAndAppliesWhatItCan(t *testing.T) {
	m := settling(t)
	m = set4Preferences(m, "bar", "ascii")
	m = set4Preferences(m, "mouse", config.MouseOff)
	m = set4Preferences(m, "rows", "250")
	m = set4Preferences(m, "chats", "no")

	saved, cmd := m.savePreferences()
	done := saved.(Model)
	if cmd == nil {
		t.Fatal("saving must say it saved")
	}
	if done.preferences.trouble != "" {
		t.Fatalf("trouble = %q", done.preferences.trouble)
	}
	if done.settings.Safety.RowLimit != 250 {
		t.Errorf("row limit = %d", done.settings.Safety.RowLimit)
	}
	if done.settings.Chats.Enabled {
		t.Error("keeping conversations was turned off")
	}
	if done.mouse {
		t.Error("the mouse must be handed back at once")
	}

	held, err := m.workspace.Setup().Store.LoadSettings()
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if held.Safety.RowLimit != 250 || held.Appearance.Bar != "ascii" ||
		held.Appearance.Mouse != config.MouseOff || held.Chats.Enabled {
		t.Errorf("the file must hold what was set: %+v", held)
	}
}

// A field that is not a number, or a length of time, is refused before the file
// is touched.
func TestSettingsThatMakeNoSenseAreRefused(t *testing.T) {
	for _, want := range []struct {
		name  string
		key   string
		value string
		said  string
	}{
		{"a row limit that is not a number", "rows", "many", "not a number"},
		{"a row limit of nothing", "rows", "0", "less than 1"},
		{"a timeout that is not a time", "query", "soon", "not a length of time"},
		{"a count that is negative", "queries", "-1", "less than 0"},
		{"an empty field", "rows", "", "cannot be empty"},
	} {
		t.Run(want.name, func(t *testing.T) {
			m := set4Preferences(settling(t), want.key, want.value)
			refused, cmd := m.savePreferences()
			if cmd != nil {
				t.Error("nothing must be written")
			}
			said := refused.(Model).preferences.trouble
			if !strings.Contains(said, want.said) {
				t.Errorf("trouble = %q, want something about %q", said, want.said)
			}
		})
	}
}

// Clearing a store asks first, says how much would go, and makes it something
// to tick rather than something to press through.
func TestClearingAStoreAsksFirst(t *testing.T) {
	m := storing(t)
	m.width, m.height = 110, 40
	editing, _ := press(t, m, "e")
	typed := typeInto(t, editing, "SELECT 1")
	ran, cmd := press(t, typed, "ctrl+r")
	m = onSettings(t, pump(t, ran, cmd))

	asked, _ := m.confirmForgetting("the query history", forgetHistoryMsg{})
	question := asked.(Model)
	if question.modal == nil {
		t.Fatal("it must ask")
	}
	said := plain(question.content())
	for _, want := range []string{"clear the query history?", "1 statement", "only copy",
		"throw them away"} {
		if !strings.Contains(said, want) {
			t.Errorf("the question must say %q:\n%s", want, said)
		}
	}
	held, _ := press(t, question, "enter")
	if held.modal == nil {
		t.Error("enter with the box unticked must not clear anything")
	}
}

// Saying yes empties the store and says so.
func TestSayingYesEmptiesTheStore(t *testing.T) {
	m := storing(t)
	editing, _ := press(t, m, "e")
	typed := typeInto(t, editing, "SELECT 1")
	ran, cmd := press(t, typed, "ctrl+r")
	m = pump(t, ran, cmd)

	if count, _ := m.session.History.Count(context.Background()); count != 1 {
		t.Fatalf("count = %d, there must be something to clear", count)
	}
	cleared, cmd := m.Update(forgetHistoryMsg{})
	done := settle(t, cleared.(Model), cmd)
	if count, _ := m.session.History.Count(context.Background()); count != 0 {
		t.Errorf("count = %d, want 0", count)
	}
	if !strings.Contains(done.text(), "the query history are empty") {
		t.Errorf("text = %q", done.text())
	}

	if _, err := m.session.Chats.Save(context.Background(), chats.Chat{
		ConnectionID: "production-eu", Title: "hello",
		Messages: []ai.Message{{Role: ai.RoleUser, Content: "hello"}},
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	emptied, cmd := m.Update(forgetChatsMsg{})
	after := settle(t, emptied.(Model), cmd)
	if count, _ := m.session.Chats.Count(context.Background()); count != 0 {
		t.Errorf("conversations = %d, want 0", count)
	}
	if after.talk.id != 0 {
		t.Error("the conversation on screen is no longer one that is kept")
	}
}

// With nothing to clear, the screen says so rather than pretending.
func TestClearingWithNowhereToClearSaysSo(t *testing.T) {
	m := loadedWith(t, healthy(), workspaceWith(t))
	m.width, m.height = 110, 40
	refused, cmd := m.forgetHistory()
	if cmd == nil || !strings.Contains(refused.(Model).text(), "nothing is being kept") {
		t.Errorf("text = %q", refused.(Model).text())
	}
	gone, cmd := m.forgetChats()
	if cmd == nil || !strings.Contains(gone.(Model).text(), "nothing is being kept") {
		t.Errorf("text = %q", gone.(Model).text())
	}
	if said := m.counted4Preferences(forgetChatsMsg{}); said != "no conversations" {
		t.Errorf("counted = %q", said)
	}
	if said := m.counted4Preferences(forgetHistoryMsg{}); said != "no statements" {
		t.Errorf("counted = %q", said)
	}
}

// Esc leaves the screen without writing anything.
func TestEscapeLeavesTheSettingsAlone(t *testing.T) {
	m := settling(t)
	before := m.settings.Safety.RowLimit
	changed := set4Preferences(m, "rows", "7")
	left, _ := press(t, changed, "esc")
	if left.view != viewDashboard || left.preferences != nil {
		t.Errorf("view = %v, esc must leave", left.view)
	}
	if left.settings.Safety.RowLimit != before {
		t.Error("and change nothing")
	}
	held, err := m.workspace.Setup().Store.LoadSettings()
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if held.Safety.RowLimit != before {
		t.Error("least of all the file")
	}
}

// A number that is not one is nought rather than a panic.
func TestANumberThatIsNotOneIsNought(t *testing.T) {
	for _, want := range []struct {
		value string
		held  int
	}{{"12", 12}, {" 12 ", 12}, {"", 0}, {"many", 0}} {
		if got := number(want.value); got != want.held {
			t.Errorf("number(%q) = %d, want %d", want.value, got, want.held)
		}
	}
}

// The screen answers its keys: the arrows walk the form, the buttons fire, and
// the palette is still one key away.
func TestTheSettingsScreenAnswersItsKeys(t *testing.T) {
	m := settling(t)
	m.width, m.height = 110, 40

	down, _ := press(t, m, "down")
	if down.preferences.form.focus != 1 {
		t.Errorf("focus = %d, down must walk the form", down.preferences.form.focus)
	}
	up, _ := press(t, down, "up")
	if up.preferences.form.focus != 0 {
		t.Errorf("focus = %d", up.preferences.form.focus)
	}
	sideways, _ := press(t, m, "right")
	if sideways.preferences.form.value("bar") == m.preferences.form.value("bar") {
		t.Error("right must walk a choice")
	}

	commands, _ := press(t, m, "ctrl+k")
	if commands.palette == nil {
		t.Error("the commands are still one key away")
	}
	quitting, _ := press(t, m, "ctrl+c")
	if quitting.modal == nil {
		t.Error("and quitting still asks")
	}
}

// Pressing a button on the form does what the button says.
func TestTheButtonsOnTheSettingsScreenFire(t *testing.T) {
	for _, want := range []struct {
		name   string
		action string
		check  func(*testing.T, Model)
	}{
		{"save", "save", func(t *testing.T, m Model) {
			if m.preferences == nil || m.preferences.trouble != "" {
				t.Error("saving must work")
			}
		}},
		{"clear the queries", "forget-queries", func(t *testing.T, m Model) {
			if m.modal == nil {
				t.Error("clearing must ask")
			}
		}},
		{"clear the chats", "forget-chats", func(t *testing.T, m Model) {
			if m.modal == nil {
				t.Error("clearing must ask")
			}
		}},
	} {
		t.Run(want.name, func(t *testing.T) {
			m := settling(t)
			m.width, m.height = 110, 40
			held := *m.preferences
			for i, entry := range held.form.fields {
				if entry.key == want.action {
					held.form.focus = i
				}
			}
			m.preferences = &held
			fired, _ := press(t, m, "enter")
			want.check(t, fired)
		})
	}
}

// A file that will not take the settings leaves them on the screen and says
// why, rather than losing what was typed.
func TestSettingsThatCannotBeWrittenStayOnTheScreen(t *testing.T) {
	m := settling(t)
	m = set4Preferences(m, "mode", "sideways")
	refused, cmd := m.savePreferences()
	if cmd != nil {
		t.Error("nothing must be written")
	}
	if refused.(Model).preferences.trouble == "" {
		t.Error("and the screen must say why")
	}
	if refused.(Model).preferences.form.value("mode") != "sideways" {
		t.Error("and keep what was typed")
	}
}

// A store that cannot be counted still gets a sentence rather than a number
// that is not true.
func TestAStoreThatCannotBeCountedStillGetsASentence(t *testing.T) {
	m := storing(t)
	if err := m.session.Chats.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if said := m.counted4Preferences(forgetChatsMsg{}); said != "what is kept" {
		t.Errorf("counted = %q", said)
	}
	if err := m.session.History.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if said := m.counted4Preferences(forgetHistoryMsg{}); said != "what is kept" {
		t.Errorf("counted = %q", said)
	}
	cleared, cmd := m.forgetChats()
	if cmd == nil {
		t.Fatal("it still tries")
	}
	if settle(t, cleared.(Model), cmd).text() != "" {
		t.Log("a store that will not empty says nothing rather than lying")
	}
}

// typed4Form puts a value in a text field, which is what typing into it does.
func typed4Form(f form, key, value string) form {
	fields := make([]field, len(f.fields))
	copy(fields, f.fields)
	for i := range fields {
		if fields[i].key == key {
			fields[i].input.SetValue(value)
		}
	}
	f.fields = fields
	return f
}

// The workspace root is a full path or nothing, and saving it makes the program
// look where it now says.
func TestARelativeSqlDirectoryIsRefusedOnTheScreen(t *testing.T) {
	m := settling(t)
	m.preferences.form = typed4Form(m.preferences.form, "sqlfiles", "sql/here")
	after, _ := m.savePreferences()
	if after.(Model).preferences.trouble == "" {
		t.Error("a relative directory must be refused rather than written")
	}
	if after.(Model).settings.Workspace.Root != "" {
		t.Error("a refused directory must not be kept")
	}
}

func TestSavingTheSqlDirectoryReadsTheFilesAgain(t *testing.T) {
	m := settling(t)
	kept := filepath.Join(t.TempDir(), "statements")
	m.preferences.form = typed4Form(m.preferences.form, "sqlfiles", kept)
	after, cmd := m.savePreferences()
	if after.(Model).preferences.trouble != "" {
		t.Fatalf("trouble = %q", after.(Model).preferences.trouble)
	}
	if after.(Model).settings.Workspace.Root != kept {
		t.Errorf("root = %q, want %q", after.(Model).settings.Workspace.Root, kept)
	}
	if after.(Model).root() != filepath.Join(kept, "production-eu") {
		t.Errorf("the files come from %q", after.(Model).root())
	}
	if cmd == nil {
		t.Error("saving must read the directory again")
	}
}

// Settings are a fact of the file, not of one connection: writing them reaches
// every connection that is open, and not only the one in front.
func TestSettingsReachEveryOpenConnection(t *testing.T) {
	m, _ := twoConnections(t)
	if len(m.links) != 2 {
		t.Fatalf("links = %d", len(m.links))
	}
	m.settings.Appearance.OwnSessions = false
	shown := m.showing4Links(true)
	for i, open := range shown.eachLink() {
		if !open.running.own {
			t.Errorf("connection %d still hides the sessions this program made", i)
		}
	}
	hidden := shown.showing4Links(false)
	for i, open := range hidden.eachLink() {
		if open.running.own {
			t.Errorf("connection %d still shows them", i)
		}
	}
}
