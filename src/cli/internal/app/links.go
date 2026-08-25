package app

import (
	"context"
	"strconv"

	tea "charm.land/bubbletea/v2"

	"github.com/sonquer/opendba/src/cli/internal/cli"
	"github.com/sonquer/opendba/src/cli/internal/config"
	"github.com/sonquer/opendba/src/cli/internal/driver"
	"github.com/sonquer/opendba/src/cli/internal/ui"
	"github.com/sonquer/opendba/src/cli/pkg/sqlguard"
)

// sessionID tells two open connections apart, including two on the same profile
// and the same database. It is minted rather than derived: what a session stands
// on can change under it — another database, another schema filter — and a tab
// pinned to it must not follow the change to somewhere else.
type sessionID int

// profile4Link is what a session was opened from, which is the profile's own
// identifier when it has one and its name when it does not.
func profile4Link(connection config.Connection) string {
	if connection.ID != "" {
		return connection.ID
	}
	return connection.Name
}

// link is one open connection: the session, the way to close it, and everything
// that has been read out of the server behind it. What a screen reads through
// the model is read through the link in front, the way a tab is.
type link struct {
	// id tells this connection apart from every other one that is open, and seq
	// is which one it is among the sessions standing on the same profile and the
	// same database.
	id  sessionID
	seq int

	session cli.Session

	// close is what the workspace handed back for this connection. Closing it twice
	// closes nothing twice, so the caller that always cleans up and an interface
	// that can disconnect do not have to agree on which of them owns it.
	close func()

	findings []driver.Finding
	tables   []driver.Table
	indexes  []driver.Index
	fields   map[string][]driver.Column
	sidebar  explorer
	running  activity
	loading  bool
	failure  string
	failing  part

	// read says whether the server behind this connection has been read, so that
	// coming back to a tab does not read it all again.
	read bool

	// assistant, build and talk are the conversation about this connection. They
	// belong to it rather than to the program: an agent is built over one
	// connection's guard, access mode and row limit, and asking it about another
	// server would be asking the wrong one. stopAsk gives up on its turn.
	assistant conversation
	build     Talk
	talk      chat
	stopAsk   context.CancelFunc
}

// newLink is a connection that has just been opened, with nothing read out of
// it yet.
func newLink(id sessionID, seq int, session cli.Session, settings config.Settings, close func()) link {
	opened := link{
		id:      id,
		seq:     seq,
		session: session,
		close:   close,
		sidebar: newExplorer(session.Theme),
		running: newActivity(session.Theme, settings),
		fields:  map[string][]driver.Column{},
		talk:    newChat(session.Theme, ""),
	}
	if session.AI.Enabled {
		opened.talk = newChat(session.Theme, session.AI.Instance.Name)
		opened.build = assistantFor(session)
	}
	return opened
}

// key is what this link is known by.
func (l link) key() sessionID { return l.id }

// classify is what the guard of this connection makes of one statement, in the
// access mode this connection was opened in. Two tabs on two connections are
// two modes, and each statement answers to its own.
func (l link) classify(statement string) sqlguard.Result {
	return l.session.Guard.Classify(statement, cli.Mode(l.session.Connection.Mode))
}

// keeping reports whether there is anywhere to keep what this connection runs.
func (l link) keeping() bool { return l.session.History != nil }

// linked4Sheet is the connection a tab belongs to, or the one in front when the
// tab names one this program no longer holds.
func (m Model) linked4Sheet(on sessionID) link {
	if at, ok := m.linkOf(on); ok {
		return m.eachLink()[at]
	}
	return m.link
}

// eachLink is every open connection, with the one being worked through as it is
// now rather than as it was last stowed.
func (m Model) eachLink() []link {
	links := make([]link, len(m.links))
	copy(links, m.links)
	if m.linked >= 0 && m.linked < len(links) {
		links[m.linked] = m.link
	}
	return links
}

// linkOf finds an open connection by its key.
func (m Model) linkOf(key sessionID) (int, bool) {
	for i, open := range m.eachLink() {
		if open.key() == key {
			return i, true
		}
	}
	return 0, false
}

// stowLink writes the connection being worked through back into the list of
// open connections, which is what makes a switch between them lose nothing.
func (m Model) stowLink() Model {
	if m.linked < 0 || m.linked >= len(m.links) {
		return m
	}
	links := make([]link, len(m.links))
	copy(links, m.links)
	links[m.linked] = m.link
	m.links = links
	return m
}

// wrote4Link puts one connection back where it came from, which is the front
// one when that is the one that was read.
func (m Model) wrote4Link(key sessionID, written link) Model {
	at, ok := m.linkOf(key)
	if !ok {
		return m
	}
	if at == m.linked {
		m.link = written
		return m
	}
	links := make([]link, len(m.links))
	copy(links, m.links)
	links[at] = written
	m.links = links
	return m
}

// through puts the connection a tab belongs to in front, which is what makes a
// tab about one server and the tab beside it about another. A key nothing holds
// any more leaves the connection in front alone rather than ending the program.
func (m Model) through(key sessionID) Model {
	if key == m.link.key() {
		return m
	}
	at, ok := m.linkOf(key)
	if !ok {
		m.on = m.link.key()
		return m
	}
	m = m.stowLink()
	m.linked, m.link = at, m.eachLink()[at]
	m.theme = m.session.Theme
	return m
}

// disconnectMsg closes one open connection, once it has been asked about.
type disconnectMsg struct{ on sessionID }

// askToDisconnect closes an open connection, asking first when closing it costs
// something and simply doing it when it does not. A connection is closed because
// it was asked to be, never because the last tab on it happened to be closed: a
// connection with no tab is still a place to stand, and the dashboard, the
// tables and the assistant all stand on one.
func (m Model) askToDisconnect() (tea.Model, tea.Cmd) {
	chosen, ok := m.chosen4Switch()
	if !ok {
		return m, nil
	}
	closing, held := m.session4Row(chosen)
	name := closing.session.Connection.Name
	at, _ := m.linkOf(closing.id)
	switch {
	case !held:
		return m, nil
	case len(m.links) == 1:
		return m, m.notify("this is the only connection open")
	case m.eachLink()[at].close == nil:
		return m, m.notify(name + " was not opened by this program, and is not closed by it")
	}
	on := m.eachLink()[at].id
	if tabs, running := m.holding4Link(on); tabs == 0 && running == 0 {
		return m.disconnected(disconnectMsg{on: on})
	}
	dialog := ask(m.theme, "disconnect "+name+"?", m.cost4Disconnect(at), disconnectMsg{on: on})
	dialog.danger = true
	m.modal = dialog
	return m, nil
}

// holding4Link is how many tabs one connection is worked through, and how many
// of them are waiting on a statement.
func (m Model) holding4Link(on sessionID) (int, int) {
	tabs, running := 0, 0
	for _, sheet := range m.eachSheet() {
		if sheet.on != on {
			continue
		}
		tabs++
		if sheet.inflight {
			running++
		}
	}
	return tabs, running
}

// cost4Disconnect is what closing one connection takes with it. A connection
// nothing is standing on costs nothing, and is not asked about at all.
func (m Model) cost4Disconnect(at int) string {
	tabs, running := m.holding4Link(m.eachLink()[at].id)
	if running > 0 {
		return ui.Plural(tabs, "tab", "tabs") + " close with it, and " +
			ui.Plural(running, "statement", "statements") + " will not arrive"
	}
	if tabs == 0 {
		return "no tab is on it, and nothing is lost"
	}
	return ui.Plural(tabs, "tab", "tabs") + " close with it, and what is in them is not kept anywhere"
}

// disconnected closes one connection, and the tabs that were worked through it.
func (m Model) disconnected(msg disconnectMsg) (tea.Model, tea.Cmd) {
	at, held := m.linkOf(msg.on)
	if !held || len(m.links) == 1 {
		return m, nil
	}
	name := m.eachLink()[at].session.Connection.Name
	m = m.stow()
	closing := m.links[at]
	if closing.close != nil {
		closing.close()
	}
	m.links = append(append([]link{}, m.links[:at]...), m.links[at+1:]...)
	switch {
	case at < m.linked:
		m.linked--
	case at == m.linked:
		m.linked = min(at, len(m.links)-1)
	}
	m.linked = min(max(m.linked, 0), len(m.links)-1)
	m.link = m.links[m.linked]
	m.theme = m.session.Theme
	said := m.notify(name + " is disconnected")
	return m.closed4Link(closing.key()), said
}

// closed4Link drops the tabs that belonged to a connection that has gone. The
// last tab is replaced rather than removed, the way closing the last tab is.
func (m Model) closed4Link(on sessionID) Model {
	kept := make([]worksheet, 0, len(m.sheets))
	front := 0
	for i, sheet := range m.eachSheet() {
		if sheet.on == on {
			_ = sheet.finished()
			continue
		}
		if i <= m.sheet {
			front = len(kept)
		}
		kept = append(kept, sheet)
	}
	if len(kept) == 0 {
		kept = append(kept, newWorksheet(m.theme, sheetQuery, "", m.link.key()))
	}
	m.sheets = kept
	m.sheet = min(max(front, 0), len(kept)-1)
	m.worksheet = kept[m.sheet]
	m = m.through(m.on)
	m.editor.SetWidth(m.paneWidth())
	m.editor.SetHeight(m.editorRows())
	m.results = m.results.resize(m.paneWidth(), m.resultsHeight())
	return m
}

// label4Link is what one open session is called: the profile it stands on, the
// database it landed in, and a number when it is not the first session standing
// on both.
func (m Model) label4Link(open link) string {
	named := ui.Slashed(open.session.Connection.Name, database4Link(open))
	if open.seq > 1 {
		return named + " #" + strconv.Itoa(open.seq)
	}
	return named
}

// database4Link is the database a session landed in, left out for a file backed
// driver, which the connection already names.
func database4Link(open link) string {
	if open.session.Connection.File != "" {
		return ""
	}
	return open.session.Info.Database
}

// next4Seq is the smallest number no session standing on the same profile and
// the same database has taken. It is minted with the session and kept, so
// closing one does not renumber the others: the label of a row you are not
// looking at must not change because of what you did to another.
func (m Model) next4Seq(connection config.Connection) int {
	taken := map[int]bool{}
	for _, open := range m.eachLink() {
		if profile4Link(open.session.Connection) == profile4Link(connection) &&
			open.session.Connection.Database == connection.Database {
			taken[open.seq] = true
		}
	}
	for seq := 1; ; seq++ {
		if !taken[seq] {
			return seq
		}
	}
}

// answering4Ask runs what one turn does against the connection the turn belongs
// to, and leaves whatever is in front in front. An answer that arrives while you
// are working somewhere else belongs to the conversation that asked for it.
func (m Model) answering4Ask(on sessionID, answering func(Model) (Model, tea.Cmd)) (Model, tea.Cmd) {
	at, ok := m.linkOf(on)
	if !ok {
		return m, nil
	}
	if at == m.linked {
		return answering(m)
	}
	front := m.linked
	m = m.stowLink()
	m.linked, m.link = at, m.links[at]
	m, cmd := answering(m)
	m = m.stowLink()
	m.linked, m.link = front, m.links[front]
	return m, cmd
}
