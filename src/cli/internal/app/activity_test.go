package app

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/sonquer/opendba/src/cli/internal/cli"
	"github.com/sonquer/opendba/src/cli/internal/config"
	"github.com/sonquer/opendba/src/cli/internal/driver"
	"github.com/sonquer/opendba/src/cli/internal/ui"
)

func busy() *fakeConn {
	conn := healthy()
	conn.sessions = []driver.Session{
		{ID: "40218", User: "api", State: "active", Wait: "Lock",
			Duration: 72 * time.Second, Statement: "UPDATE orders SET total = 1"},
		{ID: "40219", User: "api", State: "idle in transaction", Duration: 44 * time.Second},
		{ID: "40220", User: "postgres", State: "active", Duration: time.Second,
			Statement: "SELECT 1", Mine: true},
	}
	return conn
}

func watching(t *testing.T, conn *fakeConn, open func(driver.Conn) cli.Session) Model {
	t.Helper()
	m := NewModel(open(conn), workspaceWith(t))
	m.width, m.height = 110, 32
	loaded := settle(t, m, m.load())
	shown, _ := loaded.Update(runFirst(t, loaded.readSessions()))
	return shown.(Model)
}

// The list reached the screen at all is the test that was missing when it
// shipped empty.
func TestWhatIsRunningReachesTheScreen(t *testing.T) {
	m := watching(t, busy(), session)
	if len(m.running.sessions) != 3 {
		t.Fatalf("sessions = %+v", m.running.sessions)
	}
	view := plain(m.content())
	for _, want := range []string{"RUNNING", "40218", "api", "UPDATE orders", "1m12s", "Lock"} {
		if !strings.Contains(view, want) {
			t.Errorf("the list must show %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "nothing is running") {
		t.Error("three sessions are not nothing")
	}
	if strings.Contains(view, "updated") {
		t.Error("a list that refreshes itself has nothing to say about when it last did")
	}
	if !strings.Contains(view, "3 sessions") {
		t.Errorf("the heading says how much of the server this is:\n%s", view)
	}
}

// A server with nothing running still shows the table, with its column names
// and no rows. A sentence where the table was makes the screen jump every time
// the last statement finishes.
func TestAQuietServerStillShowsTheTable(t *testing.T) {
	m := watching(t, healthy(), session)
	view := plain(m.content())
	if strings.Contains(view, "nothing is running") {
		t.Errorf("an empty table says it better than a sentence:\n%s", view)
	}
	for _, want := range activityHeaders {
		if !strings.Contains(view, want) {
			t.Errorf("the column names must stay on screen, missing %q:\n%s", want, view)
		}
	}
}

func TestADriverWithoutSessionsShowsNoList(t *testing.T) {
	conn := busy()
	quiet := session(conn)
	quiet.Capabilities.Sessions = false
	m := NewModel(quiet, workspaceWith(t))
	if m.readSessions() != nil {
		t.Fatal("a driver without sessions is not asked for them")
	}
	if strings.Contains(plain(m.content()), "RUNNING") {
		t.Error("a driver without sessions has no list")
	}
}

func TestSessionsThatCannotBeReadAreReported(t *testing.T) {
	conn := busy()
	conn.failOn = "sessions"
	m := watching(t, conn, session)
	if !strings.Contains(plain(m.content()), "sessions failed") {
		t.Errorf("content = %s", plain(m.content()))
	}
}

func TestALongStatementIsCalledOut(t *testing.T) {
	m := watching(t, busy(), session)
	slow := m.running
	slow.sessions[0].State = "active"
	if legend := plain(slow.legend()); !strings.Contains(legend, "past 30.00s") {
		t.Errorf("a statement past the slow mark must be counted: %q", legend)
	}
	quiet := m.running
	quiet.sessions = quiet.sessions[2:]
	if legend := quiet.legend(); legend != "" {
		t.Errorf("a server with nothing slow says nothing: %q", legend)
	}
	if got := m.running.severity(m.running.sessions[0]); got != ui.SevWarn {
		t.Errorf("severity = %v", got)
	}
	stuck := m.running.sessions[0]
	stuck.Duration = 10 * time.Minute
	if got := m.running.severity(stuck); got != ui.SevCritical {
		t.Errorf("severity = %v", got)
	}
	idle := m.running.sessions[1]
	if got := m.running.severity(idle); got != ui.SevInactive {
		t.Errorf("a session that is not running is not slow: %v", got)
	}
}

func TestWalkingTheSessions(t *testing.T) {
	m := watching(t, busy(), session)
	on, _ := press(t, m, "tab")
	if !on.onSessions {
		t.Fatal("tab must reach the list")
	}
	down, _ := press(t, on, "down")
	if chosen, _ := down.running.selected(); chosen.ID != "40219" {
		t.Errorf("selected = %+v", chosen)
	}
	up, _ := press(t, down, "up")
	if chosen, _ := up.running.selected(); chosen.ID != "40218" {
		t.Errorf("selected = %+v", chosen)
	}
	off, _ := press(t, on, "tab")
	if off.onSessions {
		t.Error("tab must leave the list again")
	}
}

// A read only profile is warned rather than refused: the danger is spelled out
// and the way through is a box that has to be ticked on purpose.
func TestAReadOnlyConnectionIsWarnedNotRefused(t *testing.T) {
	conn := busy()
	m := watching(t, conn, session)
	on, _ := press(t, m, "tab")
	asked, _ := press(t, on, "c")
	if asked.modal == nil {
		t.Fatal("read only must ask, not refuse")
	}
	view := plain(asked.content())
	for _, want := range []string{"read only", "do it anyway", "space"} {
		if !strings.Contains(view, want) {
			t.Errorf("the dialog must say %q:\n%s", want, view)
		}
	}
	if refused, cmd := press(t, asked, "enter"); cmd != nil || refused.modal == nil {
		t.Fatal("enter does nothing until the box is ticked")
	}

	ticked, _ := press(t, asked, "space")
	if !ticked.modal.ticked {
		t.Fatal("space must tick the box")
	}
	answered, cmd := press(t, ticked, "enter")
	if cmd == nil || answered.modal != nil {
		t.Fatal("a ticked box lets it through")
	}
	stopping, cmd := answered.Update(cmd())
	stopping.(Model).Update(runFirst(t, cmd))
	if len(conn.stopped) != 1 {
		t.Fatalf("stopped = %v", conn.stopped)
	}
}

func TestAWritableConnectionIsNotWarned(t *testing.T) {
	m := watching(t, busy(), writable)
	on, _ := press(t, m, "tab")
	asked, _ := press(t, on, "c")
	if asked.modal == nil {
		t.Fatal("stopping still asks")
	}
	if asked.modal.needs != "" {
		t.Error("a profile that may write has nothing to tick")
	}
	if strings.Contains(plain(asked.content()), "do it anyway") {
		t.Error("the warning belongs to read only profiles")
	}
}

func TestCancellingAStatement(t *testing.T) {
	conn := busy()
	m := watching(t, conn, writable)
	on, _ := press(t, m, "tab")
	asked, _ := press(t, on, "c")
	if asked.modal == nil {
		t.Fatal("cancelling must ask first")
	}
	if !strings.Contains(plain(asked.content()), "cancel what 40218 is running?") {
		t.Errorf("content = %s", plain(asked.content()))
	}
	answered, cmd := press(t, asked, "enter")
	if cmd == nil || answered.modal != nil {
		t.Fatal("enter must answer")
	}
	stopping, cmd := answered.Update(cmd())
	done, _ := stopping.(Model).Update(runFirst(t, cmd))
	if len(conn.stopped) != 1 || conn.stopped[0] != "cancelled 40218" {
		t.Fatalf("stopped = %v", conn.stopped)
	}
	if !strings.Contains(plain(done.(Model).content()), "cancelled session 40218") {
		t.Errorf("content = %s", plain(done.(Model).content()))
	}
}

// Closing a session is one deliberate answer, not two. The dialog says what it
// costs, in the colour of something that costs, and takes yes or no.
func TestClosingASessionIsAskedOnce(t *testing.T) {
	conn := busy()
	m := watching(t, conn, writable)
	on, _ := press(t, m, "tab")
	asked, _ := press(t, on, "x")
	if asked.modal == nil {
		t.Fatal("closing must ask first")
	}
	if !asked.modal.danger || asked.modal.typing() {
		t.Errorf("the dialog warns rather than asking for a word back: %+v", asked.modal)
	}
	view := plain(asked.modal.view(100))
	if !strings.Contains(view, "40218") || !strings.Contains(view, "loses its connection") {
		t.Errorf("the dialog must say what it does:\n%s", view)
	}
	answered, cmd := press(t, asked, "enter")
	stopping, cmd := answered.Update(cmd())
	stopping.(Model).Update(runFirst(t, cmd))
	if len(conn.stopped) != 1 || conn.stopped[0] != "terminated 40218" {
		t.Fatalf("stopped = %v", conn.stopped)
	}
}

func TestThisProgramCannotStopItself(t *testing.T) {
	m := watching(t, busy(), writable)
	on, _ := press(t, m, "tab")
	mine := on
	for {
		chosen, ok := mine.running.selected()
		if !ok {
			t.Fatal("our own session must be in the list")
		}
		if chosen.Mine {
			break
		}
		mine, _ = press(t, mine, "down")
	}
	refused, cmd := press(t, mine, "x")
	if refused.modal != nil {
		t.Fatal("nobody gets to pull the rug from under themselves")
	}
	if cmd == nil {
		t.Fatal("it must say why")
	}
	if !strings.Contains(plain(refused.content()), "this program") {
		t.Errorf("content = %s", plain(refused.content()))
	}
}

func TestAFailedStopIsReported(t *testing.T) {
	conn := busy()
	conn.failOn = "stop"
	m := watching(t, conn, writable)
	on, _ := press(t, m, "tab")
	asked, _ := press(t, on, "c")
	answered, cmd := press(t, asked, "enter")
	stopping, cmd := answered.Update(cmd())
	failed, _ := stopping.(Model).Update(runFirst(t, cmd))
	if !strings.Contains(plain(failed.(Model).content()), "stop failed") {
		t.Errorf("content = %s", plain(failed.(Model).content()))
	}
}

func TestTheDashboardRefreshesItself(t *testing.T) {
	m := watching(t, busy(), session)
	ticked, cmd := m.Update(tickMsg{generation: m.generation})
	if cmd == nil {
		t.Fatal("a tick on the dashboard must read the server again")
	}
	if _, ok := ticked.(Model); !ok {
		t.Fatal("the model must survive a tick")
	}

	elsewhere, _ := press(t, m, "e")
	if _, cmd := elsewhere.Update(tickMsg{generation: elsewhere.generation}); cmd != nil {
		t.Error("nothing polls while another screen is in front")
	}
	if _, cmd := m.Update(tickMsg{generation: m.generation - 1}); cmd != nil {
		t.Error("a tick from a previous visit is ignored")
	}
}

func TestUnknownStopFailures(t *testing.T) {
	m := watching(t, healthy(), writable)
	on, _ := press(t, m, "tab")
	if same, cmd := press(t, on, "c"); cmd != nil || same.modal != nil {
		t.Error("an empty list has nothing to cancel")
	}
	if _, err := errors.New("x"), error(nil); err != nil {
		t.Fatal(err)
	}
}

// A beat reads what the server is doing, not what it holds. A size sweep of
// every table three times a minute is what made the dashboard jump.
func TestTheBeatReadsSessionsAndNotTheCatalogue(t *testing.T) {
	conn := busy()
	m := watching(t, conn, session)
	before := conn.counted()

	live, cmds := m.beats()
	live = settle(t, live, tea.Batch(cmds...))
	if got := conn.counted()["sessions"] - before["sessions"]; got != 1 {
		t.Errorf("the sessions were read %d times on a beat, want 1", got)
	}
	for _, step := range []string{"tables", "indexes", "health"} {
		if got := conn.counted()[step] - before[step]; got != 0 {
			t.Errorf("%s was read %d times on the first beat, want 0", step, got)
		}
	}

	for range healthEvery - 1 {
		var next []tea.Cmd
		live, next = live.beats()
		live = settle(t, live, tea.Batch(next...))
	}
	if got := conn.counted()["health"] - before["health"]; got != 1 {
		t.Errorf("health must be read once every %d beats, got %d", healthEvery, got)
	}
	for _, step := range []string{"tables", "indexes"} {
		if got := conn.counted()[step] - before[step]; got != 0 {
			t.Errorf("%s is the shape of the database, not its weather: read %d times", step, got)
		}
	}
	if live.beat != healthEvery {
		t.Errorf("beat = %d", live.beat)
	}
}

// A stale beat belongs to a screen that has been left, and reads nothing.
func TestAStaleBeatReadsNothing(t *testing.T) {
	conn := busy()
	m := watching(t, conn, session)
	before := conn.counted()
	if _, cmd := m.refreshed(tickMsg{generation: m.generation + 1}); cmd != nil {
		t.Error("a beat from a connection that has been left must be dropped")
	}
	away := m
	away.view = viewQuery
	if _, cmd := away.refreshed(tickMsg{generation: away.generation}); cmd != nil {
		t.Error("a beat while the dashboard is not on screen must be dropped")
	}
	if got := conn.counted()["sessions"] - before["sessions"]; got != 0 {
		t.Errorf("a dropped beat read the server %d times", got)
	}
	live, cmd := m.refreshed(tickMsg{generation: m.generation})
	if cmd == nil || live.(Model).beat != 1 {
		t.Errorf("a live beat must keep beating: beat = %d", live.(Model).beat)
	}
}

// A refresh keeps the screen it already has. Swapping a drawn dashboard for a
// spinner and back again is what a blink is.
func TestARefreshDoesNotBlankTheScreen(t *testing.T) {
	m := watching(t, busy(), session)
	if m.blank() {
		t.Fatal("this model has already read the server")
	}
	m.loading = true
	if strings.Contains(plain(m.content()), "reading the server") {
		t.Errorf("a refresh must leave the screen alone:\n%s", plain(m.content()))
	}
	if !strings.Contains(plain(m.content()), "RUNNING") {
		t.Errorf("the screen must still be there:\n%s", plain(m.content()))
	}
	fresh := NewModel(session(busy()), workspaceWith(t))
	fresh.width, fresh.height = 110, 32
	fresh.loading = true
	if !strings.Contains(plain(fresh.content()), "reading the server") {
		t.Error("a first read has nothing to draw and must say it is working")
	}
}

// The dashboard leaves out the sessions this program made, and says how many it
// left out rather than quietly shortening the list.
func TestTheDashboardLeavesOutItsOwnSessions(t *testing.T) {
	held := []driver.Session{
		{ID: "1", Application: driver.AppName, State: "active", Statement: "SELECT pg_stat"},
		{ID: "2", Application: driver.AppName + "/production-eu", State: "active"},
		{ID: "3", Application: "psql", State: "active", Statement: "SELECT 1"},
		{ID: "4", Application: "", State: "idle"},
	}
	hiding := newActivity(ui.Default(), config.DefaultSettings())
	hiding = hiding.withSessions(sessionsMsg{sessions: held, at: time.Now()}, 100)
	if len(hiding.sessions) != 2 {
		t.Fatalf("sessions = %d, the two of ours must go", len(hiding.sessions))
	}
	if hiding.ours != 2 {
		t.Errorf("ours = %d, want 2", hiding.ours)
	}
	if said := plain(hiding.count()); !strings.Contains(said, "2 of ours hidden") {
		t.Errorf("count = %q, hiding rows must be said out loud", said)
	}

	settings := config.DefaultSettings()
	settings.Appearance.OwnSessions = true
	showing := newActivity(ui.Default(), settings)
	showing = showing.withSessions(sessionsMsg{sessions: held, at: time.Now()}, 100)
	if len(showing.sessions) != 4 || showing.ours != 0 {
		t.Errorf("sessions = %d ours = %d, asking for them shows them",
			len(showing.sessions), showing.ours)
	}
	if said := plain(showing.count()); strings.Contains(said, "hidden") {
		t.Errorf("count = %q, nothing is hidden", said)
	}

	turned := hiding.showingOwn(true)
	if !turned.own {
		t.Error("the setting must reach the dashboard")
	}
}

// An application name says whether a session is one of ours.
func TestAnApplicationNameSaysWhoseSessionItIs(t *testing.T) {
	for _, want := range []struct {
		name        string
		application string
		ours        bool
	}{
		{"this program", driver.AppName, true},
		{"this program on a profile", driver.AppName + "/production-eu", true},
		{"something else", "psql", false},
		{"nothing at all", "", false},
		{"something that only starts the same", "opendbaeaver", false},
	} {
		t.Run(want.name, func(t *testing.T) {
			if got := driver.Ours(want.application); got != want.ours {
				t.Errorf("Ours(%q) = %v, want %v", want.application, got, want.ours)
			}
		})
	}
	for _, want := range []struct {
		name        string
		config      driver.Config
		application string
	}{
		{"with a profile", driver.Config{Application: "production-eu"},
			"opendba/production-eu"},
		{"without one", driver.Config{}, "opendba"},
	} {
		t.Run(want.name, func(t *testing.T) {
			if got := want.config.ApplicationName(); got != want.application {
				t.Errorf("ApplicationName = %q, want %q", got, want.application)
			}
		})
	}
}
