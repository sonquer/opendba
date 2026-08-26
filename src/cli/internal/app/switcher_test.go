package app

import (
	"errors"
	"strings"
	"testing"

	"github.com/sonquer/opendba/src/cli/internal/config"
	"github.com/sonquer/opendba/src/cli/internal/driver"
)

// config4Test is one profile, told apart by whichever of its two names it has.
func config4Test(id, name string) config.Connection {
	return config.Connection{ID: id, Name: name}
}

// The switcher is one dialog over whatever you were looking at: the sessions
// that are open, then the connections that are configured.
func TestTheSwitcherShowsWhatIsOpenAndWhatIsConfigured(t *testing.T) {
	m := browsing(t, workspaceWith(t))
	m.width, m.height = 110, 32
	view := plain(m.content())
	for _, want := range []string{"connections", "OPEN", "CONFIGURED", "in use", "staging"} {
		if !strings.Contains(view, want) {
			t.Errorf("the switcher must show %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "enter goes to") || strings.Contains(view, "enter opens") {
		t.Errorf("what enter does is said once, at the foot:\n%s", view)
	}
}

// It opens from every screen, because where you are working is a question you
// can have anywhere.
func TestTheSwitcherOpensFromEveryScreen(t *testing.T) {
	for _, want := range []struct {
		name string
		keys []string
	}{
		{"the dashboard", nil},
		{"the editor", []string{"e"}},
		{"the tables", []string{"s"}},
		{"the indexes", []string{"i"}},
		{"the history", []string{"ctrl+g"}},
	} {
		t.Run(want.name, func(t *testing.T) {
			m := loadedWith(t, healthy(), workspaceWith(t))
			m.width, m.height = 110, 32
			for _, key := range want.keys {
				m, _ = press(t, m, key)
			}
			opened, cmd := press(t, m, "ctrl+p")
			if opened.switcher == nil || cmd == nil {
				t.Fatalf("ctrl+p must open the switcher from %s", want.name)
			}
		})
	}
}

// A digit reaches an open session straight, which is what makes a session as
// cheap to get back to as a tab. The digit is drawn on the row, so it is not a
// secret.
func TestADigitReachesAnOpenSession(t *testing.T) {
	m, _ := twoConnections(t)
	listed, cmd := press(t, m, "ctrl+p")
	browsed := pump(t, listed, cmd)
	if !strings.Contains(plain(browsed.content()), "1") {
		t.Error("the key that reaches a session is drawn beside it")
	}
	if browsed.session.Connection.Name != "staging" {
		t.Fatalf("the test starts on %q", browsed.session.Connection.Name)
	}

	went, _ := press(t, browsed, "1")
	if went.switcher != nil {
		t.Error("reaching a session closes the dialog that reached it")
	}
	if went.session.Connection.Name != "production-eu" {
		t.Errorf("connection = %q, the first session is the first one",
			went.session.Connection.Name)
	}
	if past, _ := press(t, browsed, "9"); past.switcher == nil {
		t.Error("a digit no session answers to does nothing")
	}
}

// Enter means one thing in each section, and the section says which.
func TestEnterGoesToASessionAndOpensAConfiguredConnection(t *testing.T) {
	m, workspace := twoConnections(t)
	listed, cmd := press(t, m, "ctrl+p")
	browsed := pump(t, listed, cmd)

	onSession := on4Switch(t, browsed, key4Session(browsed.links[0].id))
	went, _ := press(t, onSession, "enter")
	if went.session.Connection.Name != "production-eu" || len(went.links) != 2 {
		t.Errorf("enter on a session goes to it: %q with %d links",
			went.session.Connection.Name, len(went.links))
	}
	if len(workspace.opened) != 1 {
		t.Errorf("and opens nothing: %v", workspace.opened)
	}
}

// Enter on a connection that is already open opens another session on it, which
// is the whole point of the two sections.
func TestEnterOnAnOpenProfileOpensAnotherSession(t *testing.T) {
	m := browsing(t, workspaceWith(t))
	m.width, m.height = 110, 32
	on := on4Switch(t, m, key4Profile("1"))
	opening, cmd := press(t, on, "enter")
	if cmd == nil {
		t.Fatal("enter in the configured section opens a connection")
	}
	opened, _ := opening.Update(runFirst(t, cmd))
	twice := opened.(Model)
	if len(twice.links) != 2 {
		t.Fatalf("links = %d", len(twice.links))
	}
	if !strings.Contains(twice.label4Link(twice.link), "#2") {
		t.Errorf("label = %q, the second session says which it is",
			twice.label4Link(twice.link))
	}
}

// The filter is a mode rather than the default, so a connection named after a
// key is still reachable by that key.
func TestTheFilterIsAModeOfItsOwn(t *testing.T) {
	m := browsing(t, workspaceWith(t))
	m.width, m.height = 110, 32
	typing, cmd := press(t, m, "f")
	if !typing.switcher.typing || cmd == nil {
		t.Fatal("f opens the filter")
	}
	for _, key := range strings.Split("stag", "") {
		typing, _ = press(t, typing, key)
	}
	if got := len(typing.rows4Switch()); got != 1 {
		t.Errorf("rows = %d, only staging is named:\n%s", got, plain(typing.content()))
	}
	back, _ := press(t, typing, "esc")
	if back.switcher == nil || back.switcher.typing {
		t.Error("esc gives the keyboard back rather than closing the dialog")
	}
	if len(back.rows4Switch()) == 1 {
		t.Error("and the list is whole again")
	}
}

// A connection that will not open says so on its own row, and the row is where
// you are left looking.
func TestAConnectionThatWillNotOpenSaysSoOnItsRow(t *testing.T) {
	workspace := workspaceWith(t)
	workspace.open = errors.New("password not found in the keychain")
	m := browsing(t, workspaceWith(t))
	m.width, m.height = 110, 32
	m.workspace = workspace

	on := on4Switch(t, m, key4Profile("2"))
	opening, cmd := press(t, on, "enter")
	failed, _ := opening.Update(runFirst(t, cmd))
	shown := failed.(Model)
	if shown.switcher == nil {
		t.Fatal("the list comes back so the row can be read")
	}
	view := plain(shown.content())
	if !strings.Contains(view, "password not found") {
		t.Errorf("content = %s", view)
	}
	if !strings.Contains(view, "production-eu") {
		t.Errorf("and the ones that did open are still listed:\n%s", view)
	}
}

// What is running is counted per connection, and said on the row it belongs to.
func TestTheSwitcherSaysWhatEachConnectionIsDoing(t *testing.T) {
	m := browsing(t, workspaceWith(t))
	m.width, m.height = 110, 32
	m.running = m.running.withSessions(sessionsMsg{
		sessions: []driver.Session{{ID: "1"}, {ID: "2"}}, on: m.id,
	}, 100)
	if !strings.Contains(plain(m.content()), "2 running") {
		t.Errorf("content = %s", plain(m.content()))
	}
}

// A tab holding something is never the one that moves, so what is opened lands
// in a tab of its own without being asked for one. A key that said "in a new
// tab" was a second way to ask for what enter already does.
func TestWhatIsOpenedLandsInATabOfItsOwn(t *testing.T) {
	m := loadedWith(t, healthy(), workspaceWith(t))
	m.width, m.height = 110, 32
	editing, _ := press(t, m, "e")
	typed := typeInto(t, editing, "SELECT 1")
	listed, cmd := press(t, typed, "ctrl+p")
	browsed := pump(t, listed, cmd)

	on := on4Switch(t, browsed, key4Profile("2"))
	opening, cmd := press(t, on, "enter")
	opened, _ := opening.Update(runFirst(t, cmd))
	tabs := opened.(Model)
	if len(tabs.sheets) != 2 {
		t.Fatalf("tabs = %d, the tab holding a statement keeps it", len(tabs.sheets))
	}
	if tabs.sheets[0].editor.Value() != "SELECT 1" {
		t.Error("and keeps what it held")
	}
	if tabs.on != tabs.id {
		t.Error("while the new tab belongs to what was opened")
	}
}

// A tab with nothing in it is the one that moves, rather than piling up empty
// tabs nobody asked for.
func TestAnEmptyTabIsTheOneThatMoves(t *testing.T) {
	m := browsing(t, workspaceWith(t))
	m.width, m.height = 110, 32
	on := on4Switch(t, m, key4Profile("2"))
	opening, cmd := press(t, on, "enter")
	opened, _ := opening.Update(runFirst(t, cmd))
	tabs := opened.(Model)
	if len(tabs.sheets) != 1 {
		t.Errorf("tabs = %d, an empty tab is reused", len(tabs.sheets))
	}
	if tabs.on != tabs.id {
		t.Error("and belongs to what was opened")
	}
}

// A profile with a session standing on it is not removed out from under it.
func TestAProfileWithASessionIsNotRemoved(t *testing.T) {
	m := browsing(t, workspaceWith(t))
	m.width, m.height = 110, 32
	on := on4Switch(t, m, key4Profile("1"))
	refused, cmd := press(t, on, "d")
	if refused.modal != nil || cmd == nil {
		t.Fatal("it must be refused, and said out loud")
	}
	if !strings.Contains(refused.text(), "disconnect it first") {
		t.Errorf("said = %q", refused.text())
	}
}

// Closing a session that costs nothing costs nothing: no dialog, just gone.
// Closing one that holds a tab says what goes with it first.
func TestClosingASessionIsAsCheapAsItIs(t *testing.T) {
	m, workspace := twoConnections(t)
	listed, cmd := press(t, m, "ctrl+p")
	browsed := pump(t, listed, cmd)
	on := on4Switch(t, browsed, key4Session(browsed.id))

	held, _ := press(t, on, "x")
	if held.modal == nil {
		t.Fatal("a session holding a tab must be asked about")
	}
	if !strings.Contains(plain(held.modal.view(110)), "1 tab") {
		t.Errorf("and say what goes with it: %s", plain(held.modal.view(110)))
	}

	quiet := browsed
	quiet.sheets[quiet.sheet].on = quiet.links[0].id
	quiet.on = quiet.links[0].id
	gone, cmd := press(t, on4Switch(t, quiet, key4Session(quiet.id)), "x")
	if gone.modal != nil || cmd == nil {
		t.Fatal("a session no tab is on is not worth a dialog")
	}
	if len(gone.links) != 1 {
		t.Errorf("links = %d, it must just go", len(gone.links))
	}
	if workspace.closed != 1 {
		t.Errorf("closed = %d, and be given back", workspace.closed)
	}
	if !strings.Contains(gone.text(), "disconnected") {
		t.Errorf("said = %q", gone.text())
	}
}

// The dialog that asks stacks over the list, and answering no leaves the list
// where it was.
func TestADialogStacksOverTheSwitcher(t *testing.T) {
	m, _ := twoConnections(t)
	listed, cmd := press(t, m, "ctrl+p")
	browsed := pump(t, listed, cmd)
	asked, _ := press(t, on4Switch(t, browsed, key4Session(browsed.id)), "x")
	if asked.modal == nil || asked.switcher == nil {
		t.Fatal("the dialog is over the list, not instead of it")
	}
	back, _ := press(t, asked, "esc")
	if back.modal != nil || back.switcher == nil {
		t.Error("esc answers the dialog and leaves the list open")
	}
}

// The only session open is not something this closes.
func TestTheOnlySessionCannotBeDisconnected(t *testing.T) {
	m := browsing(t, workspaceWith(t))
	m.width, m.height = 110, 32
	kept, cmd := press(t, on4Switch(t, m, key4Session(m.id)), "x")
	if kept.modal != nil || cmd == nil {
		t.Fatal("it stays open, and says so")
	}
	if !strings.Contains(kept.text(), "only connection") {
		t.Errorf("said = %q", kept.text())
	}
}

// Two sessions on one connection are two places to work: both reachable, told
// apart wherever they are named, and closed one at a time.
func TestTwoSessionsOnOneConnection(t *testing.T) {
	m := loadedWith(t, healthy(), workspaceWith(t))
	m.width, m.height = 110, 32
	editing, _ := press(t, m, "e")
	typed := typeInto(t, editing, "SELECT 1")
	listed, cmd := press(t, typed, "ctrl+p")
	browsed := pump(t, listed, cmd)
	opening, cmd := press(t, on4Switch(t, browsed, key4Profile("1")), "enter")
	opened, _ := opening.Update(runFirst(t, cmd))
	twice := opened.(Model)

	if len(twice.links) != 2 || len(twice.sheets) != 2 {
		t.Fatalf("links = %d tabs = %d", len(twice.links), len(twice.sheets))
	}
	if twice.links[0].id == twice.links[1].id {
		t.Fatal("two sessions must be told apart")
	}
	if got := twice.label4Link(twice.links[1]); !strings.Contains(got, "#2") {
		t.Errorf("label = %q", got)
	}
	strip := plain(twice.tabBar(110))
	if strings.Count(strip, "production-eu") != 2 || !strings.Contains(strip, "#2") {
		t.Errorf("the strip must name both:\n%s", strip)
	}

	back, _ := press(t, twice, "e")
	first, _ := press(t, back, "ctrl+1")
	if first.id != twice.links[0].id {
		t.Error("the first tab is still on the first session")
	}
	second, _ := press(t, first, "ctrl+2")
	if second.id != twice.links[1].id {
		t.Error("and the second on the second")
	}

	gone, _ := second.disconnected(disconnectMsg{on: twice.links[1].id})
	left := gone.(Model)
	if len(left.links) != 1 || left.id != twice.links[0].id {
		t.Errorf("links = %d left on %d", len(left.links), left.id)
	}
	if len(left.sheets) != 1 {
		t.Errorf("tabs = %d, the tab on the session that went goes with it", len(left.sheets))
	}
}

// A session numbered second keeps its number when the first one goes, so the
// label of a row you are not looking at does not change under you.
func TestASessionKeepsItsNumber(t *testing.T) {
	m := loaded(t, healthy())
	m.links = []link{m.link, named4Link(t, 2, "production-eu")}
	m.links[1].seq = 2
	if got := m.label4Link(m.links[1]); !strings.Contains(got, "#2") {
		t.Fatalf("label = %q", got)
	}
	gone, _ := m.disconnected(disconnectMsg{on: m.links[0].id})
	left := gone.(Model)
	if got := left.label4Link(left.link); !strings.Contains(got, "#2") {
		t.Errorf("label = %q, closing the first must not renumber the second", got)
	}
	if left.next4Seq(left.session.Connection) != 1 {
		t.Error("but the number it left behind is free again")
	}
}

// Past the ninth session there is no digit left, and the row says nothing rather
// than a key that does nothing.
func TestOnlyNineSessionsGetADigit(t *testing.T) {
	if cap4Session(0) == "" {
		t.Error("the first session has a key")
	}
	if cap4Session(maxJumpTabs) != "" {
		t.Error("the tenth has none")
	}
}

// A profile with no identifier of its own is known by its name, which is what an
// older profiles.toml holds.
func TestAProfileWithNoIdentifierIsKnownByItsName(t *testing.T) {
	if got := profile4Link(config4Test("", "local")); got != "local" {
		t.Errorf("profile = %q", got)
	}
	if got := profile4Link(config4Test("7", "local")); got != "7" {
		t.Errorf("profile = %q", got)
	}
}

// A tab naming a session this program no longer holds reads the one in front
// rather than nothing at all.
func TestATabOnASessionThatIsGoneReadsTheOneInFront(t *testing.T) {
	m := loaded(t, healthy())
	if got := m.linked4Sheet(sessionID(404)); got.id != m.id {
		t.Errorf("link = %d, want the one in front", got.id)
	}
}

// What closing a connection costs is counted from the tabs on it.
func TestDisconnectingSaysWhatItCosts(t *testing.T) {
	m, _ := twoConnections(t)
	at, _ := m.linkOf(m.id)
	if got := m.cost4Disconnect(at); !strings.Contains(got, "1 tab") {
		t.Errorf("cost = %q", got)
	}
	m.inflight = true
	if got := m.cost4Disconnect(at); !strings.Contains(got, "will not arrive") {
		t.Errorf("cost = %q", got)
	}
	m.inflight = false
	m.on = sessionID(404)
	if got := m.cost4Disconnect(at); !strings.Contains(got, "nothing is lost") {
		t.Errorf("cost = %q", got)
	}
}

// named4Link is one more open connection, told apart by the name of its profile
// and given an id of its own.
func named4Link(t *testing.T, id sessionID, name string) link {
	t.Helper()
	opened := session(healthy())
	opened.Connection.ID, opened.Connection.Name = name, name
	return newLink(id, 1, opened, config.DefaultSettings(), func() {})
}

// sheet4Test is one tab holding one statement, on one connection.
func sheet4Test(t *testing.T, m Model, statement string, on sessionID) worksheet {
	t.Helper()
	sheet := newWorksheet(m.theme, sheetQuery, "", on)
	sheet.editor.SetValue(statement)
	return sheet
}

// Disconnecting one connection leaves whichever was in front in front, whether
// it sat before or after the one that went.
func TestDisconnectingKeepsTheConnectionInFront(t *testing.T) {
	for _, want := range []struct {
		name   string
		gone   int
		linked int
		front  string
	}{
		{"the one before it", 0, 2, "third"},
		{"the one after it", 2, 1, "second"},
		{"the one in front", 1, 1, "third"},
	} {
		t.Run(want.name, func(t *testing.T) {
			m := loaded(t, healthy())
			m.links = []link{m.link, named4Link(t, 2, "second"), named4Link(t, 3, "third")}
			m.linked, m.link = want.linked, m.links[want.linked]
			m.on = m.id
			m.sheets[0].on = m.on

			gone, _ := m.disconnected(disconnectMsg{on: m.links[want.gone].id})
			left := gone.(Model)
			if len(left.links) != 2 {
				t.Fatalf("links = %d", len(left.links))
			}
			if got := left.session.Connection.Name; got != want.front {
				t.Errorf("front = %q, want %q", got, want.front)
			}
			if left.id != left.eachLink()[left.linked].id {
				t.Error("and the index must point at the connection in front")
			}
		})
	}
}

// The tab being worked in is still the tab being worked in after a connection
// goes, wherever its tabs sat in the strip.
func TestDisconnectingKeepsTheTabInFront(t *testing.T) {
	for _, want := range []struct {
		name      string
		gone      int
		sheet     int
		statement string
	}{
		{"a tab before it", 0, 1, "SELECT 2"},
		{"a tab after it", 2, 1, "SELECT 2"},
		{"the tab in front", 1, 1, "SELECT 1"},
	} {
		t.Run(want.name, func(t *testing.T) {
			m := loaded(t, healthy())
			m.width, m.height = 110, 32
			other := named4Link(t, 2, "other")
			m.links = []link{m.link, other}
			mine, theirs := m.id, other.id

			m.sheets = []worksheet{
				sheet4Test(t, m, "SELECT 1", mine),
				sheet4Test(t, m, "SELECT 2", mine),
				sheet4Test(t, m, "SELECT 3", mine),
			}
			m.sheets[want.gone].on = theirs
			m.sheet = want.sheet
			m.worksheet = m.sheets[want.sheet]

			left := m.closed4Link(theirs)
			if len(left.sheets) != 2 {
				t.Fatalf("tabs = %d", len(left.sheets))
			}
			if got := left.statement(); got != want.statement {
				t.Errorf("statement = %q, want %q", got, want.statement)
			}
			if left.editor.Value() != left.sheets[left.sheet].editor.Value() {
				t.Error("and the index must point at the tab in front")
			}
		})
	}
}

// The filter takes the keyboard while it is open: enter answers with what is
// under the cursor, and the arrows still move.
func TestTheFilterKeepsTheArrowsAndTheAnswer(t *testing.T) {
	m := browsing(t, workspaceWith(t))
	m.width, m.height = 110, 32
	typing, _ := press(t, m, "f")
	down, _ := press(t, typing, "down")
	if down.switcher.cursor != 1 {
		t.Errorf("cursor = %d, the arrows still move", down.switcher.cursor)
	}
	up, _ := press(t, down, "up")
	if up.switcher.cursor != 0 {
		t.Errorf("cursor = %d", up.switcher.cursor)
	}
	answered, _ := press(t, up, "enter")
	if answered.switcher != nil {
		t.Error("enter answers with what is under the cursor")
	}
}

// A row naming a session or a profile this program no longer knows names
// nothing, rather than the wrong one.
func TestARowNamingNothingAnswersNothing(t *testing.T) {
	m := browsing(t, workspaceWith(t))
	if _, ok := m.session4Row(row{key: rowProfile + "1"}); ok {
		t.Error("a profile row is not a session")
	}
	if _, ok := m.session4Row(row{key: rowSession + "nine"}); ok {
		t.Error("a session row that is not a number is not a session")
	}
	if _, ok := m.session4Row(row{key: rowSession + "404"}); ok {
		t.Error("a session nothing holds is not a session")
	}
	if _, ok := m.profile4Row(row{key: rowProfile + "404"}); ok {
		t.Error("and a profile nothing lists is not a profile")
	}
	empty := m
	empty.switcher.cursor = 99
	if _, ok := empty.chosen4Switch(); ok {
		t.Error("a cursor past the end is on nothing")
	}
	if _, cmd := press(t, empty, "enter"); cmd != nil {
		t.Error("so enter does nothing")
	}
}

// The assistant belongs to the connection, not to the program. It used to be
// built once over whichever connection the program started on, so after a switch
// it ran its SQL against the wrong server, in that server's access mode.
func TestTheAssistantBelongsToItsConnection(t *testing.T) {
	m, _ := twoConnections(t)
	if len(m.links) != 2 {
		t.Fatalf("links = %d", len(m.links))
	}
	for i, open := range m.eachLink() {
		if open.build == nil {
			continue
		}
		if open.session.Conn != m.links[i].session.Conn {
			t.Error("a conversation must be built over its own connection")
		}
	}
	if m.links[0].talk.exchanges != nil && m.links[1].talk.exchanges != nil {
		t.Error("two connections are two conversations")
	}
}

// An answer that arrives while you are working somewhere else lands in the
// conversation that asked for it.
func TestAnAnswerLandsInTheConversationThatAskedForIt(t *testing.T) {
	m, _ := twoConnections(t)
	background := m.links[0].id
	if background == m.id {
		t.Fatal("the test needs the other connection in front")
	}
	m.links[0].talk.token = 7

	landed, _ := m.Update(askEndedMsg{token: 7, on: background})
	arrived := landed.(Model)
	if arrived.id != m.id {
		t.Error("an answer for another connection must not move you")
	}
	if arrived.links[0].talk.busy {
		t.Error("and the turn it belongs to is the one that ends")
	}
}

// A turn nobody is waiting for any more is refused rather than left hanging, or
// the agent behind it never returns.
func TestATurnForAConnectionThatIsGoneIsRefused(t *testing.T) {
	m := loaded(t, healthy())
	answer := make(chan error, 1)
	m.Update(askApprovalMsg{request: approval{answer: answer}, token: 1, on: sessionID(404)})
	if err := <-answer; err == nil {
		t.Error("it must be answered, whatever the answer is")
	}
}

// The session being worked in says so in words. It used to wear a bar of its
// own beside the cursor's, and two bars in two columns read as a broken screen
// rather than as two different things.
func TestTheSessionBeingWorkedInSaysSo(t *testing.T) {
	m, _ := twoConnections(t)
	listed, cmd := press(t, m, "ctrl+p")
	browsed := pump(t, listed, cmd)

	view := plain(browsed.content())
	if strings.Count(view, "in use") != 1 {
		t.Errorf("one session is in use, and one row says it:\n%s", view)
	}
	for _, item := range browsed.rows4Switch() {
		if item.mark != "" {
			t.Errorf("no row draws a bar of its own: %+v", item)
		}
	}
	elsewhere := on4Switch(t, browsed, key4Session(browsed.links[0].id))
	if strings.Count(plain(elsewhere.content()), "in use") != 1 {
		t.Error("and moving the cursor does not move what is in use")
	}
}

// How many tabs are on a session is said on its row, or "open in a new tab"
// names something the dialog never shows.
func TestASessionSaysHowManyTabsAreOnIt(t *testing.T) {
	m, _ := twoConnections(t)
	listed, cmd := press(t, m, "ctrl+p")
	view := plain(pump(t, listed, cmd).content())
	if !strings.Contains(view, "1 tab") {
		t.Errorf("the tabs on a session must be counted:\n%s", view)
	}
	if !strings.Contains(view, "new tab") {
		t.Errorf("and the key that opens one named:\n%s", view)
	}
}
