package app

import (
	"context"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/sonquer/tui4db/src/cli/internal/config"
	"github.com/sonquer/tui4db/src/cli/internal/driver"
	"github.com/sonquer/tui4db/src/cli/internal/ui"
)

const (
	refreshEvery    = 3 * time.Second
	activityRows    = 6
	statementLength = 60
)

type sessionsMsg struct {
	sessions []driver.Session
	at       time.Time
	err      error
}

type stoppedMsg struct {
	id        string
	terminate bool
	err       error
}

type stopMsg struct {
	id        string
	terminate bool
}

type tickMsg struct{ generation int }

// activity is what the server is doing right now, and the means to stop it.
type activity struct {
	theme    *ui.Theme
	sessions []driver.Session
	table    table.Model
	cursor   int
	updated  time.Time
	focused  bool
	width    int
	failure  string
	slow     time.Duration
	stuck    time.Duration
}

func newActivity(theme *ui.Theme, safety config.SafetySettings) activity {
	return activity{
		theme: theme,
		slow:  duration(safety.SlowQuery, 30*time.Second),
		stuck: duration(safety.StuckQuery, 5*time.Minute),
	}
}

func duration(value string, fallback time.Duration) time.Duration {
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

// focus rebuilds the table, because the cursor row is painted at build time and
// a table nobody is driving should not claim to have a selection.
func (a activity) focus(on bool) activity {
	a.focused = on
	return a.rebuild()
}

func (a activity) resize(width int) activity {
	a.width = width
	return a.rebuild()
}

func (a activity) rebuild() activity {
	if len(a.sessions) == 0 {
		return a
	}
	at := a.cursor
	a.table = table.New(
		table.WithColumns(columnsFor(activityHeaders,
			naturalWidths(activityHeaders, a.rows()), a.width)),
		table.WithRows(rowsFor(a.rows())),
		table.WithHeight(min(activityRows, max(len(a.sessions), 1))),
		table.WithWidth(a.width),
		table.WithStyles(tableStyles(a.theme, a.focused)),
	)
	a.table.SetCursor(at)
	return a
}

func (a activity) withSessions(msg sessionsMsg, width int) activity {
	a.failure = ""
	a.width = width
	if msg.err != nil {
		a.failure = msg.err.Error()
		return a
	}
	a.sessions, a.updated = msg.sessions, msg.at
	if a.cursor >= len(a.sessions) {
		a.cursor = max(0, len(a.sessions)-1)
	}
	return a.rebuild()
}

var activityHeaders = []string{"pid", "user", "state", "waiting", "time", "statement"}

func (a activity) rows() [][]string {
	rows := make([][]string, 0, len(a.sessions))
	for _, session := range a.sessions {
		rows = append(rows, []string{
			a.who(session),
			session.User,
			session.State,
			or(session.Wait, "—"),
			driver.Duration(session.Duration),
			ui.Truncate(strings.Join(strings.Fields(session.Statement), " "), statementLength),
		})
	}
	return rows
}

func (a activity) who(session driver.Session) string {
	if session.Mine {
		return session.ID + " ·"
	}
	return session.ID
}

func or(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

// severity colours a session by how long it has been at it, which is the one
// number that tells a passer by whether something is wrong.
func (a activity) severity(session driver.Session) ui.Severity {
	switch {
	case !session.Running():
		return ui.SevInactive
	case session.Duration >= a.stuck:
		return ui.SevCritical
	case session.Duration >= a.slow:
		return ui.SevWarn
	default:
		return ui.SevOK
	}
}

func (a activity) selected() (driver.Session, bool) {
	if a.cursor < 0 || a.cursor >= len(a.sessions) {
		return driver.Session{}, false
	}
	return a.sessions[a.cursor], true
}

func (a activity) move(step int) activity {
	if len(a.sessions) == 0 {
		return a
	}
	a.cursor = (a.cursor + step + len(a.sessions)) % len(a.sessions)
	a.table.SetCursor(a.cursor)
	return a
}

func (a activity) view(width int, focused bool) string {
	_ = focused
	head := a.theme.Section("running", a.count(), width)
	switch {
	case a.failure != "":
		return head + "\n\n" + a.theme.Error.Render("  ✗ "+a.failure)
	case len(a.sessions) == 0:
		return head + "\n\n" + a.theme.Muted.Render("  nothing is running")
	}
	rendered := a.table.View()
	if !focused {
		rendered = plainCursor(rendered)
	}
	return head + "\n\n" + lipgloss.NewStyle().MaxWidth(width).Render(rendered) + "\n" + a.legend()
}

// count says how much of the server this list is, which is worth knowing and is
// the only thing worth putting beside the heading. How long ago the list was
// read is not: it refreshes on its own every few seconds, so the answer is
// always the same and always nothing.
func (a activity) count() string {
	if len(a.sessions) == 0 {
		return ""
	}
	return a.theme.Muted.Render(ui.Plural(len(a.sessions), "session", "sessions"))
}

// plainCursor takes the highlight off the row under the cursor while the table
// is not the pane being driven.
func plainCursor(rendered string) string {
	return rendered
}

func (a activity) legend() string {
	slow, stuck := 0, 0
	for _, session := range a.sessions {
		switch a.severity(session) {
		case ui.SevCritical:
			stuck++
		case ui.SevWarn:
			slow++
		}
	}
	if slow+stuck == 0 {
		return ""
	}
	parts := []string{}
	if stuck > 0 {
		parts = append(parts, a.theme.Severity(ui.SevCritical).
			Render(ui.Plural(stuck, "statement", "statements")+" past "+driver.Duration(a.stuck)))
	}
	if slow > 0 {
		parts = append(parts, a.theme.Severity(ui.SevWarn).
			Render(ui.Plural(slow, "statement", "statements")+" past "+driver.Duration(a.slow)))
	}
	return "  " + strings.Join(parts, a.theme.Muted.Render(" · "))
}

func (m Model) readSessions() tea.Cmd {
	if !m.session.Capabilities.Sessions {
		return nil
	}
	conn := m.session.Conn
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), catalogTimeout)
		defer cancel()

		sessions, err := conn.Sessions(ctx)
		return sessionsMsg{sessions: sessions, at: time.Now(), err: err}
	}
}

// tick keeps the dashboard fresh while it is the screen in front, and carries
// the generation it belongs to so a tick from a previous visit is ignored.
func (m Model) tick() tea.Cmd {
	generation := m.generation
	return tea.Tick(refreshEvery, func(time.Time) tea.Msg { return tickMsg{generation: generation} })
}

// refreshed answers the beat. Sessions are read every time, because what a
// server is running changes by the second; the health of the server is read
// every fifth beat, because none of it moves that fast; the catalogue is not
// read at all, because it is the shape of the database rather than its weather
// and a size sweep of every table three times a minute is what made the
// dashboard jump.
func (m Model) refreshed(msg tickMsg) (tea.Model, tea.Cmd) {
	if msg.generation != m.generation || m.view != viewDashboard {
		return m, nil
	}
	next, cmds := m.beats()
	return next, tea.Batch(append(cmds, next.tick())...)
}

// beats is what one refresh asks the server for, apart from the next beat: the
// sessions every time, the health of the server every fifth time, and the
// catalogue never.
func (m Model) beats() (Model, []tea.Cmd) {
	m.beat++
	cmds := []tea.Cmd{m.readSessions()}
	if m.beat%healthEvery == 0 {
		cmds = append(cmds, m.readHealth())
	}
	return m, cmds
}

// healthEvery is how many session beats pass between two reads of the health of
// the server.
const healthEvery = 5

func (m Model) activityKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Up):
		m.running = m.running.move(-1)
		return m, nil
	case key.Matches(msg, m.keys.Down):
		m.running = m.running.move(1)
		return m, nil
	case key.Matches(msg, m.keys.Cancel):
		return m.confirmStop(false)
	case key.Matches(msg, m.keys.Terminate):
		return m.confirmStop(true)
	case key.Matches(msg, m.keys.Choose):
		return m.sessionPage()
	}
	return m, nil
}

// confirmStop refuses before it asks: a read only connection stops nothing, and
// nobody gets to pull the rug from under themselves.
func (m Model) confirmStop(terminate bool) (tea.Model, tea.Cmd) {
	chosen, ok := m.running.selected()
	if !ok {
		return m, nil
	}
	if !m.session.Capabilities.Sessions {
		return m, m.notify("this driver has no sessions to stop")
	}
	if chosen.Mine {
		return m, m.notify("that session is this program")
	}
	dialog := ask(m.theme, "cancel what "+chosen.ID+" is running?",
		ui.Truncate(chosen.Statement, statementLength), stopMsg{id: chosen.ID})
	if terminate {
		dialog = ask(m.theme, "close session "+chosen.ID+"?",
			"the client on the other end loses its connection mid statement",
			stopMsg{id: chosen.ID, terminate: true})
		dialog.danger = true
	}
	dialog.tag = m.theme.Muted.Render(chosen.User)
	if !m.mayChange() {
		dialog.warning("this profile is read only. Stopping a session changes the server, "+
			"which is the one thing read only says will not happen.", "do it anyway")
	}
	m.modal = dialog
	return m, nil
}

// mayChange says whether the profile expects this program to change the server.
// It is a warning rather than a wall: the person in front of it decides, once,
// per action.
func (m Model) mayChange() bool {
	return m.session.Connection.Mode == config.ReadWrite
}

func (m Model) stop(msg stopMsg) tea.Cmd {
	conn := m.session.Conn
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), catalogTimeout)
		defer cancel()

		return stoppedMsg{id: msg.id, terminate: msg.terminate, err: conn.Stop(ctx, msg.id, msg.terminate)}
	}
}

func (m Model) stopped(msg stoppedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		return m, m.notify(msg.err.Error())
	}
	verb := "cancelled"
	if msg.terminate {
		verb = "closed"
	}
	return m, tea.Batch(m.readSessions(), m.notify(verb+" session "+msg.id))
}
