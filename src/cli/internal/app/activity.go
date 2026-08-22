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

func (a activity) withSessions(msg sessionsMsg, width int) activity {
	a.failure = ""
	if msg.err != nil {
		a.failure = msg.err.Error()
		return a
	}
	a.sessions, a.updated = msg.sessions, msg.at
	if a.cursor >= len(a.sessions) {
		a.cursor = max(0, len(a.sessions)-1)
	}
	a.table = table.New(
		table.WithColumns(columnsFor(activityHeaders, a.rows(), width)),
		table.WithRows(rowsFor(a.rows())),
		table.WithHeight(min(activityRows, max(len(a.sessions), 1))),
		table.WithWidth(width),
		table.WithStyles(tableStyles(a.theme)),
	)
	a.table.SetCursor(a.cursor)
	return a
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
	tag := ""
	if !a.updated.IsZero() {
		tag = "updated " + driver.Duration(time.Since(a.updated).Round(time.Second)) + " ago"
	}
	head := a.theme.Section("running", a.theme.Muted.Render(tag), width)
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
		return a.theme.Muted.Render("  nothing has been running for long")
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

func (m Model) refreshed(msg tickMsg) (tea.Model, tea.Cmd) {
	if msg.generation != m.generation || m.view != viewDashboard {
		return m, nil
	}
	return m, tea.Batch(m.load(), m.readSessions(), m.tick())
}

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
	if !m.mayStop() {
		return m, m.notify("this connection is read only, so nothing here can be stopped")
	}
	if chosen.Mine {
		return m, m.notify("that session is this program")
	}
	if !terminate {
		m.modal = ask(m.theme, "cancel what "+chosen.ID+" is running?",
			ui.Truncate(chosen.Statement, statementLength), stopMsg{id: chosen.ID})
		return m, nil
	}
	dialog, cmd := askTyped(m.theme, "close session "+chosen.ID+"?",
		"the client loses its connection; type the pid to confirm",
		chosen.ID, stopMsg{id: chosen.ID, terminate: true})
	m.modal = dialog
	return m, cmd
}

// mayStop is the one place that decides whether this program is allowed to
// change anything on the server.
func (m Model) mayStop() bool {
	return m.session.Capabilities.Sessions && m.session.Connection.Mode == config.ReadWrite
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
