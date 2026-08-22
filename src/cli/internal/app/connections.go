package app

import (
	"context"
	"strings"

	"charm.land/bubbles/v2/key"

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
	picker
	failure string
}

func newConnections(theme *ui.Theme) connections {
	return connections{picker: newPicker(theme, "no connections are configured")}
}

func (c connections) withProfiles(msg profilesMsg, width int) connections {
	if msg.err != nil {
		c.failure = msg.err.Error()
		return c
	}
	rows := make([]row, 0, len(msg.profiles))
	for _, connection := range msg.profiles {
		rows = append(rows, row{
			key:   connection.Name,
			label: connection.Name,
			note: ui.Dotted(
				connection.Driver,
				strings.ToLower(connection.Mode.Label()),
				cli.Target(connection),
			),
			current: connection.Name == msg.current,
		})
	}
	c.picker = c.withRows(rows)
	return c
}

func (c connections) view(width int) string {
	if c.failure != "" {
		return c.theme.Error.Render("✗ "+c.failure) + "\n\n" + c.picker.view(width)
	}
	return c.picker.view(width)
}

func (m Model) browse() (tea.Model, tea.Cmd) {
	m.view = viewSwitch
	m.list.failure = ""
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
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m.confirmQuit()
	case key.Matches(msg, m.keys.Back), key.Matches(msg, m.keys.Connections):
		m.view = viewDashboard
	case key.Matches(msg, m.keys.Up):
		m.list.picker = m.list.move(-1)
	case key.Matches(msg, m.keys.Down):
		m.list.picker = m.list.move(1)
	case key.Matches(msg, m.keys.Choose):
		return m.chosen()
	case key.Matches(msg, m.keys.New):
		return m.compose()
	case key.Matches(msg, m.keys.Remove):
		return m.askToRemove()
	}
	return m, nil
}

func (m Model) askToRemove() (tea.Model, tea.Cmd) {
	chosen, ok := m.list.selected()
	if !ok {
		return m, nil
	}
	dialog, cmd := askTyped(m.theme, "remove "+chosen.label+"?",
		"its password goes with it; type the name to confirm",
		chosen.label, removeMsg{name: chosen.label})
	m.modal = dialog
	return m, cmd
}

func (m Model) chosen() (tea.Model, tea.Cmd) {
	connection, ok := m.list.selected()
	if !ok {
		return m, nil
	}
	m.view = viewDashboard
	if connection.label == m.session.Connection.Name {
		return m, nil
	}
	m.loading = true
	m.failure = ""
	return m, tea.Batch(m.open(connection.label), m.spinner.Tick)
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
	m.list.failure = ""
	m.release()
	m.session, m.close = msg.session, msg.cleanup
	m.theme = msg.session.Theme
	m.results = results{}
	m.focus = focusEditor
	m.offset = 0
	m.loading = true
	m.failure = ""
	return m, tea.Batch(m.load(), m.spinner.Tick,
		m.remember(msg.session.Connection.Database, msg.session.Connection.DefaultSchema),
		m.notify(m.place(msg.session)))
}

func (m Model) place(session cli.Session) string {
	if session.Info.Database != "" && session.Connection.File == "" {
		return "now on " + session.Connection.Name + " · " + session.Info.Database
	}
	return "now on " + session.Connection.Name
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
	m.list.failure = ""
	return m, tea.Batch(m.profiles(), m.notify(msg.name+" is gone"))
}
