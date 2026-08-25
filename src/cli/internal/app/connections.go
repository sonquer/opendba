package app

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/sonquer/opendba/src/cli/internal/cli"
	"github.com/sonquer/opendba/src/cli/internal/config"
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

	// profile is the connection that was being opened, so a failure can be said
	// on the row it belongs to rather than over the list.
	profile string
}

type removedMsg struct {
	name string
	err  error
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

// askToRemove takes a profile away, and only a profile: a database or a schema
// under it is not a thing this screen removes.
func (m Model) askToRemove() (tea.Model, tea.Cmd) {
	chosen, ok := m.chosen4Switch()
	if !ok || !strings.HasPrefix(chosen.key, rowProfile) {
		return m, nil
	}
	if m.standing4Profile(chosen.label) > 0 {
		return m, m.notify(chosen.label + " has an open session; disconnect it first")
	}
	dialog, cmd := askTyped(m.theme, "remove "+chosen.label+"?",
		"its password goes with it; type the name to confirm",
		chosen.label, removeMsg{name: chosen.label})
	m.modal = dialog
	return m, cmd
}

func (m Model) compose() (tea.Model, tea.Cmd) {
	wizard := NewSetupModel(m.workspace.Setup())
	wizard.width, wizard.height = m.width, m.height
	m.wizard = &wizard
	return m, wizard.Init()
}

// configure opens the profile under the cursor in the form that made it, so a
// host, a port or an access mode can be changed without removing the connection
// and starting again.
func (m Model) configure() (tea.Model, tea.Cmd) {
	chosen, ok := m.chosen4Switch()
	if !ok {
		return m, nil
	}
	named, ok := m.profile4Row(chosen)
	if !ok {
		return m, nil
	}
	profiles, err := m.workspace.Profiles()
	if err != nil {
		m.switcher.failure = err.Error()
		return m, nil
	}
	connection, found := profiles.ByName(named.Name)
	if !found {
		return m, m.notify("there is no profile named " + named.Name + " any more")
	}
	wizard := EditSetupModel(m.workspace.Setup(), connection)
	wizard.width, wizard.height = m.width, m.height
	m.wizard = &wizard
	return m, wizard.Init()
}

// created applies what the wizard did.
func (m Model) created(done SetupDone) (tea.Model, tea.Cmd) {
	editing := m.wizard != nil && m.wizard.editing.ID != ""
	mine := done.Connection.ID == m.session.Connection.ID
	m.wizard = nil
	if !done.Saved {
		return m, m.profiles()
	}
	if editing && !mine {
		return m, tea.Batch(m.profiles(), m.notify(done.Connection.Name+" is saved"))
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
		return switchedMsg{session: session, cleanup: cleanup, err: err, profile: name}
	}
}

func (m Model) switched(msg switchedMsg) (tea.Model, tea.Cmd) {
	m.loading = false
	if msg.err != nil {
		return m.troubled4Switch(msg)
	}
	m.trouble = copied4Switch(m.trouble)
	delete(m.trouble, msg.profile)
	m = m.opened4Switch(msg)
	m = m.aimed4Switch(m.id)
	m.theme = m.session.Theme
	m.focus = focusEditor
	m.offset = 0
	m.loading = true
	m.failure = ""
	return m, tea.Batch(m.load(), m.spinner.Tick,
		m.remember(profile4Link(msg.session.Connection), msg.session.Connection.Database,
			msg.session.Connection.DefaultSchema, msg.session.Connection.Filter()),
		m.notify(m.place(msg.session)))
}

// added4Switch puts a connection that was opened into the list of open ones.
// Every open is its own session, even when another is already standing on the
// same profile and the same database: two windows onto one server is a thing
// people want, and the id that tells them apart is minted here.
func (m Model) added4Switch(msg switchedMsg) (Model, int) {
	m = m.stowLink()
	m.minted++
	opened := newLink(sessionID(m.minted), m.next4Seq(msg.session.Connection),
		msg.session, m.settings, msg.cleanup)
	m.links = append(append([]link{}, m.links...), opened)
	return m, len(m.links) - 1
}

// opened4Switch puts the connection that was opened in front as well.
func (m Model) opened4Switch(msg switchedMsg) Model {
	m, at := m.added4Switch(msg)
	m.linked, m.link = at, m.eachLink()[at]
	return m
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
		return m, tea.Batch(m.profiles(), m.notify(msg.err.Error()))
	}
	return m, tea.Batch(m.profiles(), m.notify(msg.name+" is gone"))
}

// troubled4Switch says why a connection would not open, on the row it belongs to
// rather than as a banner over the ones that did, and puts the list back in
// front so the row can be read.
func (m Model) troubled4Switch(msg switchedMsg) (tea.Model, tea.Cmd) {
	opened, cmd := m.openSwitcher()
	shown, ok := opened.(Model)
	if !ok {
		return opened, cmd
	}
	shown.trouble = copied4Switch(shown.trouble)
	shown.trouble[msg.profile] = msg.err.Error()
	return shown, cmd
}

// copied4Switch copies what would not open, since a map handed between two
// models is a map two models can write.
func copied4Switch(from map[string]string) map[string]string {
	to := make(map[string]string, len(from))
	for profile, text := range from {
		to[profile] = text
	}
	return to
}
