package app

import (
	"context"
	"strings"

	"charm.land/bubbles/v2/key"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/sonquer/tui4db/src/cli/internal/cli"
	"github.com/sonquer/tui4db/src/cli/internal/config"
	"github.com/sonquer/tui4db/src/cli/internal/ui"
)

type profilesMsg struct {
	profiles []config.Connection
	current  string
	err      error
}

type switchedMsg struct {
	session cli.Session
	cleanup func()
	err     error
}

type removedMsg struct {
	name string
	err  error
}

type connections struct {
	theme    *ui.Theme
	profiles []config.Connection
	current  string
	cursor   int
	confirm  textinput.Model
	deleting string
	failure  string
}

func newConnections(theme *ui.Theme) connections {
	return connections{theme: theme, confirm: input(theme, "", false)}
}

func (c connections) selected() (config.Connection, bool) {
	if c.cursor < 0 || c.cursor >= len(c.profiles) {
		return config.Connection{}, false
	}
	return c.profiles[c.cursor], true
}

func (c connections) withProfiles(msg profilesMsg) connections {
	c.profiles = msg.profiles
	c.current = msg.current
	if msg.err != nil {
		c.failure = msg.err.Error()
	}
	if c.cursor >= len(c.profiles) {
		c.cursor = max(0, len(c.profiles)-1)
	}
	return c
}

func (c connections) move(step int) connections {
	if len(c.profiles) == 0 {
		return c
	}
	c.cursor = (c.cursor + step + len(c.profiles)) % len(c.profiles)
	return c
}

func (c connections) askToRemove() (connections, tea.Cmd) {
	connection, ok := c.selected()
	if !ok {
		return c, nil
	}
	c.deleting = connection.Name
	c.failure = ""
	c.confirm = input(c.theme, "", false)
	return c, c.confirm.Focus()
}

func (c connections) cancel() connections {
	c.deleting = ""
	c.confirm.Blur()
	return c
}

func (c connections) clear() connections {
	c.failure = ""
	return c
}

func (c connections) confirmed() bool {
	return c.deleting != "" && strings.TrimSpace(c.confirm.Value()) == c.deleting
}

func (c connections) editConfirmation(msg tea.KeyPressMsg) (connections, tea.Cmd) {
	updated, cmd := c.confirm.Update(msg)
	c.confirm = updated
	return c, cmd
}

func (c connections) removing() bool { return c.deleting != "" }

const connectionNameWidth = 22

func (c connections) view(width int) string {
	lines := make([]string, 0, len(c.profiles)+3)
	if len(c.profiles) == 0 {
		lines = append(lines, c.theme.Muted.Render("no connections are configured"))
	}
	for i, connection := range c.profiles {
		lines = append(lines, c.row(connection, width, i == c.cursor, connection.Name == c.current))
	}
	if c.deleting != "" {
		lines = append(lines, "",
			c.theme.Severity(ui.SevCritical).Render("remove "+c.deleting+" and its password?"),
			c.theme.Muted.Render("type the name to confirm")+"  "+c.confirm.View())
	}
	if c.failure != "" {
		lines = append(lines, "", c.theme.Error.Render("✗ "+c.failure))
	}
	return strings.Join(lines, "\n")
}

func (c connections) row(connection config.Connection, width int, active, current bool) string {
	marker := "  "
	if active {
		marker = c.theme.Accent.Render("▌ ")
	}
	label := ui.Pad(ui.Truncate(connection.Name, connectionNameWidth-2), connectionNameWidth)
	name := c.theme.Value.Render(label)
	if current {
		name = c.theme.Accent.Render(ui.Pad(ui.Truncate(connection.Name, connectionNameWidth-4)+" ·", connectionNameWidth))
	}
	details := ui.Dotted(
		connection.Driver,
		strings.ToLower(connection.Mode.Label()),
		cli.Target(connection),
	)
	return marker + c.theme.Env(ui.EnvColor(connection.Color)) + " " + name +
		c.theme.Muted.Render(ui.Truncate(details, width-connectionNameWidth-4))
}

func (m Model) browse() (tea.Model, tea.Cmd) {
	m.view = viewSwitch
	m.list = m.list.cancel().clear()
	return m, m.profiles()
}

func (m Model) profiles() tea.Cmd {
	workspace := m.workspace
	current := m.session.Connection.Name
	return func() tea.Msg {
		profiles, err := workspace.Profiles()
		message := profilesMsg{err: err}
		if err == nil {
			message.profiles = profiles.Connections
			message.current = current
		}
		return message
	}
}

func (m Model) switchKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.list.removing() {
		return m.removalKey(msg)
	}
	switch {
	case key.Matches(msg, m.keys.Quit):
		m.quitting = true
		return m, tea.Quit
	case key.Matches(msg, m.keys.Back), key.Matches(msg, m.keys.Connections):
		m.view = viewDashboard
	case key.Matches(msg, m.keys.Up):
		m.list = m.list.move(-1)
	case key.Matches(msg, m.keys.Down):
		m.list = m.list.move(1)
	case key.Matches(msg, m.keys.Choose):
		return m.chosen()
	case key.Matches(msg, m.keys.New):
		return m.compose()
	case key.Matches(msg, m.keys.Remove):
		list, cmd := m.list.askToRemove()
		m.list = list
		return m, cmd
	}
	return m, nil
}

func (m Model) removalKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Back):
		m.list = m.list.cancel()
		return m, nil
	case key.Matches(msg, m.keys.Choose):
		if !m.list.confirmed() {
			return m, nil
		}
		name := m.list.deleting
		m.list = m.list.cancel()
		return m, m.remove(name)
	}
	list, cmd := m.list.editConfirmation(msg)
	m.list = list
	return m, cmd
}

func (m Model) chosen() (tea.Model, tea.Cmd) {
	connection, ok := m.list.selected()
	if !ok {
		return m, nil
	}
	m.view = viewDashboard
	if connection.Name == m.session.Connection.Name {
		return m, nil
	}
	m.loading = true
	m.failure = ""
	return m, tea.Batch(m.open(connection.Name), m.spinner.Tick)
}

func (m Model) compose() (tea.Model, tea.Cmd) {
	wizard := NewSetupModel(m.workspace.Setup())
	wizard.width, wizard.height = m.width, m.height
	m.wizard = &wizard
	return m, wizard.Init()
}

func (m Model) created(done SetupDone) (tea.Model, tea.Cmd) {
	m.wizard = nil
	if !done.Saved {
		return m, m.profiles()
	}
	m.view = viewDashboard
	m.loading = true
	m.failure = ""
	return m, tea.Batch(m.open(done.Connection.Name), m.spinner.Tick)
}

func (m Model) open(name string) tea.Cmd {
	workspace := m.workspace
	return func() tea.Msg {
		session, cleanup, err := workspace.Open(context.Background(), name)
		return switchedMsg{session: session, cleanup: cleanup, err: err}
	}
}

func (m Model) switched(msg switchedMsg) (tea.Model, tea.Cmd) {
	m.loading = false
	if msg.err != nil {
		m.list.failure = msg.err.Error()
		m.view = viewSwitch
		return m, nil
	}
	m.release()
	m.list = m.list.clear()
	m.session, m.close = msg.session, msg.cleanup
	m.theme = msg.session.Theme
	m.results = results{}
	m.resultsFocus = false
	m.offset = 0
	m.loading = true
	m.failure = ""
	return m, tea.Batch(m.load(), m.spinner.Tick, m.notify("now on "+msg.session.Connection.Name))
}

func (m Model) remove(name string) tea.Cmd {
	workspace := m.workspace
	return func() tea.Msg {
		return removedMsg{name: name, err: workspace.Remove(context.Background(), name)}
	}
}

func (m Model) removed(msg removedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.list.failure = msg.err.Error()
		return m, m.profiles()
	}
	m.list = m.list.clear()
	return m, tea.Batch(m.profiles(), m.notify(msg.name+" is gone"))
}
