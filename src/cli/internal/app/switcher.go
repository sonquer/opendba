package app

import (
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/sonquer/opendba/src/cli/internal/cli"
	"github.com/sonquer/opendba/src/cli/internal/config"
	"github.com/sonquer/opendba/src/cli/internal/ui"
)

const (
	switcherWidth   = 72
	minSwitcherRows = 3
)

// switchMsg opens the switcher from the command list.
type switchMsg struct{}

// switcher is where you are working and where you could be: the sessions that
// are open, then the connections that are configured. It is drawn over whatever
// screen you were on rather than being a screen of its own, because choosing
// where to work is a question you answer and leave, not a place you go.
type switcher struct {
	theme   *ui.Theme
	cursor  int
	filter  textinput.Model
	typing  bool
	failure string
}

func newSwitcher(theme *ui.Theme) switcher {
	return switcher{theme: theme, filter: input(theme, "", false)}
}

// section headings, and the row keys underneath them.
const (
	sectionOpen       = "open"
	sectionConfigured = "configured"
	rowSession        = "session:"
	rowProfile        = "profile:"
)

func key4Session(id sessionID) string { return rowSession + strconv.Itoa(int(id)) }

// cap4Session is the key that reaches one session from here. It is a bare digit
// rather than the tab strip's ctrl+digit because this dialog owns the keyboard
// while it is open, and one keystroke is the point of it.
func cap4Session(at int) string {
	if at >= maxJumpTabs {
		return ""
	}
	return ui.Keystroke(strconv.Itoa(at + 1))
}

// digit4Switch is the session a bare digit reaches, or minus one when the key
// was not a digit that names one.
func digit4Switch(msg tea.KeyPressMsg) int {
	typed := msg.String()
	if len(typed) != 1 || typed[0] < '1' || typed[0] > '9' {
		return -1
	}
	return int(typed[0] - '1')
}

func key4Profile(profile string) string { return rowProfile + profile }

// openSwitcher puts the switcher in front and reads the connections that are
// configured, which is the half of the list this program does not already hold.
func (m Model) openSwitcher() (tea.Model, tea.Cmd) {
	built := newSwitcher(m.theme)
	if m.switcher != nil {
		built.failure = m.switcher.failure
	}
	m.switcher = &built
	m.editor.Blur()
	return m, m.profiles()
}

// listed4Switch takes the connections that are configured, whether or not the
// dialog that shows them is open.
func (m Model) listed4Switch(msg profilesMsg) Model {
	if msg.err != nil {
		if m.switcher != nil {
			m.switcher.failure = msg.err.Error()
		}
		return m
	}
	m.configured = msg.profiles
	if m.switcher != nil {
		m.switcher.failure = ""
	}
	return m
}

// rows4Switch is every place you could be working. The rows are read from the
// open connections and the profiles each time they are drawn, so a cached copy
// can never disagree with what is actually open.
func (m Model) rows4Switch() []row {
	rows := make([]row, 0, len(m.links)+len(m.configured))
	for i, open := range m.eachLink() {
		rows = append(rows, row{
			key:     key4Session(open.id),
			label:   m.label4Link(open),
			note:    m.told4Link(open),
			cap:     cap4Session(i),
			current: open.id == m.id,
			section: sectionOpen,
			badge:   m.theme.Mode(open.session.Connection.Mode.Label()),
		})
	}
	for _, connection := range m.configured {
		rows = append(rows, row{
			key:     key4Profile(profile4Link(connection)),
			label:   connection.Name,
			note:    m.told4Profile(connection),
			section: sectionConfigured,
			badge:   m.theme.Mode(connection.Mode.Label()),
		})
	}
	return m.matching4Switch(rows)
}

// told4Link is what one open session is: what speaks it, what it will let you
// do, how many tabs are on it and what it is running. Where it is, is left to
// the label, which already says the server and the database.
func (m Model) told4Link(open link) string {
	tabs, _ := m.holding4Link(open.id)
	return ui.Spaced(open.session.Connection.Driver,
		ui.Plural(tabs, "tab", "tabs"), m.busy4Links()[open.id])
}

// told4Profile is the same for a connection nothing is standing on, with how
// many sessions are standing on it when any are.
func (m Model) told4Profile(connection config.Connection) string {
	if trouble, ok := m.trouble[connection.Name]; ok {
		return trouble
	}
	standing := 0
	for _, open := range m.eachLink() {
		if profile4Link(open.session.Connection) == profile4Link(connection) {
			standing++
		}
	}
	held := ""
	if standing > 0 {
		held = ui.Plural(standing, "session", "sessions")
	}
	return ui.Spaced(connection.Driver, cli.Target(connection), held)
}

// matching4Switch drops the rows the filter does not name. A section with
// nothing left in it goes with them.
func (m Model) matching4Switch(rows []row) []row {
	term := strings.ToLower(strings.TrimSpace(m.switcher.filter.Value()))
	if term == "" {
		return rows
	}
	kept := make([]row, 0, len(rows))
	for _, item := range rows {
		if strings.Contains(strings.ToLower(item.label+" "+item.note), term) {
			kept = append(kept, item)
		}
	}
	return kept
}

// busy4Links is what each open connection is doing, counted per connection
// rather than once for whichever is in front.
func (m Model) busy4Links() map[sessionID]string {
	busy := map[sessionID]string{}
	for _, open := range m.eachLink() {
		if count := len(open.running.sessions); count > 0 {
			busy[open.id] = strconv.Itoa(count) + " running"
		}
	}
	return busy
}

// chosen4Switch is the row the cursor is on.
func (m Model) chosen4Switch() (row, bool) {
	rows := m.rows4Switch()
	if m.switcher.cursor < 0 || m.switcher.cursor >= len(rows) {
		return row{}, false
	}
	return rows[m.switcher.cursor], true
}

// session4Row is the open connection a row names, when it names one.
func (m Model) session4Row(item row) (link, bool) {
	if !strings.HasPrefix(item.key, rowSession) {
		return link{}, false
	}
	id, err := strconv.Atoi(strings.TrimPrefix(item.key, rowSession))
	if err != nil {
		return link{}, false
	}
	at, ok := m.linkOf(sessionID(id))
	if !ok {
		return link{}, false
	}
	return m.eachLink()[at], true
}

// profile4Row is the configured connection a row names, whichever section the
// row is in: a session row names the profile it stands on.
func (m Model) profile4Row(item row) (config.Connection, bool) {
	wanted := strings.TrimPrefix(item.key, rowProfile)
	if open, ok := m.session4Row(item); ok {
		wanted = profile4Link(open.session.Connection)
	}
	for _, connection := range m.configured {
		if profile4Link(connection) == wanted {
			return connection, true
		}
	}
	return config.Connection{}, false
}

func (m Model) switcherKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.switcher.typing {
		return m.typed4Switch(msg)
	}
	switch {
	case key.Matches(msg, m.keys.Leave):
		return m.confirmQuit()
	case key.Matches(msg, m.keys.Back), key.Matches(msg, m.keys.Connections):
		m.switcher = nil
		return m, nil
	case key.Matches(msg, m.keys.Up):
		return m.moved4Switch(-1)
	case key.Matches(msg, m.keys.Down):
		return m.moved4Switch(1)
	case key.Matches(msg, m.keys.Find):
		m.switcher.typing = true
		return m, m.switcher.filter.Focus()
	case digit4Switch(msg) >= 0:
		return m.reached4Switch(digit4Switch(msg))
	case key.Matches(msg, m.keys.Choose):
		return m.answered4Switch()
	case key.Matches(msg, m.keys.Disconnect):
		return m.askToDisconnect()
	case key.Matches(msg, m.keys.New):
		return m.compose()
	case key.Matches(msg, m.keys.Edit):
		return m.configure()
	case key.Matches(msg, m.keys.Remove):
		return m.askToRemove()
	}
	return m, nil
}

// typed4Switch is the filter having the keyboard. Esc gives it back rather than
// closing the switcher, so a search that found nothing is one key from a list
// that shows everything.
func (m Model) typed4Switch(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if key.Matches(msg, m.keys.Back) {
		m.switcher.typing = false
		m.switcher.filter.SetValue("")
		m.switcher.filter.Blur()
		return m, nil
	}
	if key.Matches(msg, m.keys.Choose) {
		m.switcher.typing = false
		m.switcher.filter.Blur()
		return m.answered4Switch()
	}
	switch {
	case key.Matches(msg, m.keys.Above):
		return m.moved4Switch(-1)
	case key.Matches(msg, m.keys.Below):
		return m.moved4Switch(1)
	}
	updated, cmd := m.switcher.filter.Update(msg)
	m.switcher.filter = updated
	m.switcher.cursor = 0
	return m, cmd
}

func (m Model) moved4Switch(step int) (tea.Model, tea.Cmd) {
	rows := m.rows4Switch()
	if len(rows) == 0 {
		return m, nil
	}
	m.switcher.cursor = (m.switcher.cursor + step + len(rows)) % len(rows)
	return m, nil
}

// reached4Switch is a digit going straight to the session drawn beside it,
// which is what makes an open session as cheap to reach as a tab.
func (m Model) reached4Switch(at int) (tea.Model, tea.Cmd) {
	links := m.eachLink()
	if at < 0 || at >= len(links) {
		return m, nil
	}
	return m.went4Switch(links[at])
}

// answered4Switch is enter, and what it means is decided by which section the
// cursor is in: a session is somewhere to switch to, a configured connection is
// something to open. That is why one key can mean both without being a trap.
func (m Model) answered4Switch() (tea.Model, tea.Cmd) {
	chosen, ok := m.chosen4Switch()
	if !ok {
		return m, nil
	}
	if open, held := m.session4Row(chosen); held {
		return m.went4Switch(open)
	}
	connection, found := m.profile4Row(chosen)
	if !found {
		return m, nil
	}
	m.switcher = nil
	m.view = viewDashboard
	m.loading = true
	m.failure = ""
	return m, tea.Batch(m.open(connection.Name), m.spinner.Tick)
}

// went4Switch puts an open session in front.
func (m Model) went4Switch(open link) (tea.Model, tea.Cmd) {
	m.switcher = nil
	m.view = viewDashboard
	if open.id == m.id && m.on == open.id {
		return m, nil
	}
	m = m.aimed4Switch(open.id)
	return m, m.notify(m.place(m.session))
}

// aimed4Switch aims a tab at a connection: the one in front when it is empty and
// idle, and a new one otherwise. Silently re-aiming a tab that holds a statement
// and its result at another server is the dangerous answer, and clearing what it
// holds is the lossy one — so a tab holding something is never the one that
// moves, and asking for that with a key of its own would be asking twice.
func (m Model) aimed4Switch(on sessionID) Model {
	if m.results.present || m.inflight || strings.TrimSpace(m.statement()) != "" {
		return m.openSheet(newWorksheet(m.theme, sheetQuery, "", on))
	}
	m.on = on
	return m.through(on)
}

func (m Model) view4Switch(width, height int) string {
	inner := min(ui.TextWidth(width)-6, switcherWidth)
	rows := m.rows4Switch()
	list := newPicker(m.theme, "no connections are configured")
	list = list.withRows(window4Switch(rows, m.switcher.cursor, room4Switch(height)))
	list.cursor = min(m.switcher.cursor, max(len(list.rows)-1, 0))

	lines := []string{ui.SplitLine(m.theme.Title.Render("connections"),
		m.theme.Subtle.Render(ui.Plural(len(m.links), "open", "open")), inner)}
	if m.switcher.typing {
		lines = append(lines, m.theme.Prompt.Render("› ")+m.switcher.filter.View())
	}
	lines = append(lines, m.theme.Rule(inner))
	if m.switcher.failure != "" {
		lines = append(lines, m.theme.Error.Render("✗ "+m.switcher.failure))
	}
	lines = append(lines, list.view(inner), "", m.hints4Switch())
	return m.theme.Panel.Render(square(strings.Join(lines, "\n"), inner))
}

// room4Switch is how many rows the list gets once the frame, the title and the
// hints have had theirs.
func room4Switch(height int) int { return max(ui.BodyHeight(height)-10, minSwitcherRows) }

// window4Switch keeps the cursor in view without moving it, which is what a list
// longer than the panel needs.
func window4Switch(rows []row, cursor, room int) []row {
	if len(rows) <= room {
		return rows
	}
	start := min(max(cursor-room/2, 0), len(rows)-room)
	return rows[start : start+room]
}

// hints4Switch says what the keys do here, and changes with the row so a key
// that does nothing on this row is not offered on it.
func (m Model) hints4Switch() string {
	if m.switcher.typing {
		return m.theme.Hints(ui.Hint{Key: "enter", Does: "open"}, ui.Hint{Key: "esc", Does: "back"})
	}
	hints := []ui.Hint{{Key: "enter", Does: "switch"}}
	chosen, ok := m.chosen4Switch()
	if _, held := m.session4Row(chosen); ok && !held {
		hints = []ui.Hint{{Key: "enter", Does: "open another"}}
	} else if ok && held {
		hints = append(hints, ui.Hint{Key: m.keys.Disconnect.Help().Key, Does: "disconnect"})
	}
	return m.theme.Hints(append(hints,
		ui.Hint{Key: m.keys.New.Help().Key, Does: m.keys.New.Help().Desc},
		ui.Hint{Key: m.keys.Find.Help().Key, Does: "find"},
		ui.Hint{Key: "esc", Does: "close"})...)
}

// standing4Profile is how many sessions are open on one configured connection.
func (m Model) standing4Profile(name string) int {
	standing := 0
	for _, open := range m.eachLink() {
		if open.session.Connection.Name == name {
			standing++
		}
	}
	return standing
}
