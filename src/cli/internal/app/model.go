package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/sonquer/tui4db/src/cli/internal/cli"
	"github.com/sonquer/tui4db/src/cli/internal/config"
	"github.com/sonquer/tui4db/src/cli/internal/driver"
	"github.com/sonquer/tui4db/src/cli/internal/ui"
	"github.com/sonquer/tui4db/src/cli/pkg/sqlguard"
)

type view string

const (
	viewDashboard view = "dashboard"
	viewSchema    view = "schema"
	viewIndexes   view = "indexes"
	viewCatalog   view = "databases"
	viewQuery     view = "query"
	viewHelp      view = "help"
	viewSwitch    view = "connections"
	viewAsk       view = "ask"
	viewAI        view = "ai"
)

// part is which of the two reads a message belongs to, so a refresh that asked
// for one does not blank the other and a failure of one is not cleared by the
// other coming back.
type part string

const (
	partHealth    part = "health"
	partCatalogue part = "catalogue"
)

type loadedMsg struct {
	part     part
	findings []driver.Finding
	tables   []driver.Table
	indexes  []driver.Index
	err      error
}

type queriedMsg struct {
	statement string
	columns   []string
	rows      [][]string
	duration  time.Duration
	truncated bool
	err       error
}

type Model struct {
	session   cli.Session
	workspace cli.Workspace
	close     func()
	theme     *ui.Theme
	view      view
	width     int
	height    int
	quitting  bool
	loading   bool
	failure   string
	failing   part
	toaster
	list    connections
	catalog catalog
	wizard  *SetupModel
	palette *palette
	modal   *modal
	page    *details
	reading int
	listing int
	lists   [2]browse
	suggest completion
	fields  map[string][]driver.Column
	keys    keymap
	help    help.Model
	offset  int

	findings []driver.Finding
	tables   []driver.Table
	indexes  []driver.Index

	editor     textarea.Model
	results    results
	sidebar    explorer
	running    activity
	onSessions bool
	generation int
	beat       int
	focus      pane
	zoomed     bool
	split      int
	spinner    spinner.Model

	assistant conversation
	build     Talk
	talk      chat
	ai        aiSettings
	chooser   *chooser
	pending   string
	stopFetch context.CancelFunc
	stopAsk   context.CancelFunc
	stopLoad  context.CancelFunc
}

// Talk builds a conversation once it has been told how to ask for permission.
// The screen owns consent because the screen is what can put the question in
// front of somebody, so the assistant is handed a way to ask rather than
// arriving with one.
type Talk func(allowed permission) (conversation, error)

// WithAssistant gives the model something to have a conversation with. What is
// kept is the way to build one rather than one already built: opening a local
// model reads gigabytes off the disk, and nobody who never presses the key
// should pay for that. A screen that can be handed a fake is also a screen that
// can be tested.
func (m Model) WithAssistant(instance string, build Talk) Model {
	m.talk = newChat(m.theme, instance)
	m.build = build
	return m
}

// pane is what the keys are talking to inside the editor screen.
type pane int

const (
	focusEditor pane = iota
	focusResults
	focusSidebar
)

func NewModel(session cli.Session, workspace cli.Workspace) Model {
	theme := session.Theme
	loader := spinner.New()
	loader.Spinner = spinner.Dot
	loader.Style = lipgloss.NewStyle().Foreground(theme.P.Accent)
	hints := help.New()
	hints.Styles = helpStyles(theme)
	hints.ShortSeparator = "  "
	hints.FullSeparator = "   "
	return Model{
		keys:      newKeymap(),
		help:      hints,
		session:   session,
		workspace: workspace,
		theme:     theme,
		view:      viewDashboard,
		width:     96,
		height:    32,
		loading:   true,
		editor:    newEditor(theme),
		spinner:   loader,
		list:      newConnections(theme),
		catalog:   newCatalog(theme),
		lists:     [2]browse{newBrowse(theme, 0, false), newBrowse(theme, 2, true)},
		suggest:   completion{theme: theme},
		sidebar:   newExplorer(theme),
		talk:      newChat(theme, ""),
		ai:        newAISettings(theme),
		running:   newActivity(theme, session.Settings.Safety),
		fields:    map[string][]driver.Column{},
	}
}

// helpStyles puts every key on a cap, because a bare letter beside a word does
// not read as something to press.
func helpStyles(theme *ui.Theme) help.Styles {
	cap := theme.KeycapStyle
	return help.Styles{
		Ellipsis:       theme.Subtle,
		ShortKey:       cap,
		ShortDesc:      theme.Muted,
		ShortSeparator: theme.Subtle,
		FullKey:        cap,
		FullDesc:       theme.Muted,
		FullSeparator:  theme.Subtle,
	}
}

func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.load(), m.spinner.Tick, m.readSessions(), m.tick()}
	if m.session.Warning != "" {
		cmds = append(cmds, m.notify(m.session.Warning))
	}
	return tea.Batch(cmds...)
}

func (m Model) release() {
	if m.close != nil {
		m.close()
	}
}

// load reads everything: the health of the server and the catalogue it holds.
// It is what a fresh connection and an explicit reload ask for.
func (m Model) load() tea.Cmd {
	return tea.Batch(m.readHealth(), m.readCatalogue())
}

// readHealth is the part of the dashboard that moves. It is cheap enough to
// repeat and is the only thing the refresh reads besides the sessions.
func (m Model) readHealth() tea.Cmd {
	conn := m.session.Conn
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
		defer cancel()

		findings, err := conn.Health(ctx)
		if err != nil {
			return loadedMsg{part: partHealth, err: err}
		}
		return loadedMsg{part: partHealth, findings: findings}
	}
}

// readCatalogue is what the server holds rather than what it is doing. It is a
// size and statistics sweep of every table and every index, so it is read when
// the shape of the database could have changed and not on a clock.
func (m Model) readCatalogue() tea.Cmd {
	conn := m.session.Conn
	connection := m.session.Connection
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
		defer cancel()

		tables, err := conn.Tables(ctx, one(connection))
		if err != nil {
			return loadedMsg{part: partCatalogue, err: err}
		}
		indexes, err := conn.Indexes(ctx, one(connection))
		if err != nil {
			return loadedMsg{part: partCatalogue, err: err}
		}
		return loadedMsg{
			part:    partCatalogue,
			tables:  scoped(tables, connection.Only, func(t driver.Table) string { return t.Schema }),
			indexes: scoped(indexes, connection.Only, func(i driver.Index) string { return i.Schema }),
		}
	}
}

const readTimeout = 30 * time.Second

// loaded applies whichever half of a read came back. A refresh that asked only
// for the health of the server leaves the catalogue alone, and the other way
// round, so neither read can blank the other's part of the screen.
func (m Model) loaded(msg loadedMsg) (tea.Model, tea.Cmd) {
	m.loading = false
	if msg.err != nil {
		m.failure, m.failing = msg.err.Error(), msg.part
		return m, nil
	}
	if m.failing == msg.part {
		m.failure, m.failing = "", ""
	}
	switch msg.part {
	case partHealth:
		m.findings = msg.findings
	case partCatalogue:
		m.tables, m.indexes = msg.tables, msg.indexes
		m.sidebar = m.sidebar.withTables(m.tables, m.fields)
	}
	return m, nil
}

// one is the schema a driver can filter on its own. A form with several schemas
// ticked reads the whole server once and keeps what it asked for, because the
// drivers take one schema and a round trip each is worse than a filter here.
func one(connection config.Connection) string {
	if filter := connection.Filter(); len(filter) == 1 {
		return filter[0]
	}
	return ""
}

// scoped drops what the schema filter does not ask for.
func scoped[T any](items []T, keep func(string) bool, of func(T) string) []T {
	kept := items[:0]
	for _, item := range items {
		if keep(of(item)) {
			kept = append(kept, item)
		}
	}
	return kept
}

func (m Model) run(statement string) tea.Cmd {
	conn := m.session.Conn
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()

		result, err := conn.Query(ctx, statement)
		if err != nil {
			return queriedMsg{statement: statement, err: err}
		}
		defer func() { _ = result.Close() }()

		message := queriedMsg{statement: statement, columns: result.Columns()}
		for result.Next() {
			message.rows = append(message.rows, ui.Strings(result.Values()))
		}
		message.err = result.Err()
		message.duration = result.Duration()
		message.truncated = result.Truncated()
		return message
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.help.SetWidth(ui.FrameWidth(m.width))
		m.running = m.running.resize(ui.FrameWidth(m.width))
		m.editor.SetWidth(m.paneWidth())
		m.editor.SetHeight(m.editorRows())
		m.talk.prompt.SetWidth(ui.TextWidth(m.width) - 4)
		m.results = m.results.resize(m.paneWidth(), m.resultsHeight())
		if m.wizard != nil {
			return m.toWizard(msg)
		}
		return m, nil
	case askEventMsg:
		return m.asked(msg)
	case askApprovalMsg:
		return m.approving(msg)
	case askAnswerMsg:
		return m.answered(msg)
	case askEndedMsg:
		return m.finished(msg)
	case fetchProgressMsg:
		return m.fetched(msg)
	case fetchEndedMsg:
		return m.doneFetching(msg)
	case ollamaMsg:
		return m.answered4Ollama(msg)
	case spinner.TickMsg:
		if !m.spinning() {
			return m, nil
		}
		updated, cmd := m.spinner.Update(msg)
		m.spinner = updated
		return m, cmd
	case loadedMsg:
		return m.loaded(msg)
	case queriedMsg:
		m.results = newResults(m.theme, msg, m.paneWidth(), m.resultsHeight())
		m.focus = focusEditor
		return m, nil
	case toastMsg:
		m.expire(msg)
		return m, nil
	case profilesMsg:
		m.list = m.list.withProfiles(msg, ui.FrameWidth(m.width))
		return m, nil
	case removeMsg:
		return m, m.remove(msg.name)
	case rememberedMsg:
		if msg.err != nil {
			return m, m.notify(msg.err.Error())
		}
		return m, nil
	case columnsMsg:
		m.fields[msg.table] = msg.columns
		m.sidebar = m.sidebar.withTables(m.tables, m.fields)
		m = m.repage(msg.table)
		return m.resuggest()
	case sessionsMsg:
		m.running = m.running.withSessions(msg, ui.FrameWidth(m.width))
		return m, nil
	case tickMsg:
		return m.refreshed(msg)
	case stopMsg:
		return m, m.stop(msg)
	case stoppedMsg:
		return m.stopped(msg)
	case catalogMsg:
		m.catalog = m.catalog.withCatalog(msg, m.session.Connection)
		return m, nil
	case switchedMsg:
		return m.switched(msg)
	case removedMsg:
		return m.removed(msg)
	case SetupDone:
		return m.created(msg)
	case tea.KeyboardEnhancementsMsg:
		m.keys = m.keys.withEnhancements(msg)
		return m, nil
	case gotoMsg:
		return m.show(msg.view)
	case reloadMsg:
		m.loading = true
		return m, tea.Batch(m.load(), m.spinner.Tick)
	case runMsg:
		return m, m.run(msg.statement)
	case newConnectionMsg:
		return m.compose()
	case warmedMsg:
		return m.warmed(msg)
	case readyMsg:
		return m.ready(msg)
	case crashedMsg:
		return m.crashed(msg)
	case releaseMsg:
		return m.released()
	case anywayMsg:
		return m.anyway(msg)
	case forgetMsg:
		return m.forgot(msg)
	case quitMsg:
		m.quitting = true
		return m, tea.Quit
	case tea.MouseWheelMsg:
		return m.wheeled(msg)
	case tea.KeyPressMsg:
		return m.key(msg)
	}
	if m.wizard != nil {
		return m.toWizard(msg)
	}
	return m, nil
}

func (m Model) toWizard(msg tea.Msg) (tea.Model, tea.Cmd) {
	updated, cmd := m.wizard.Update(msg)
	wizard, ok := updated.(SetupModel)
	if !ok {
		return m, cmd
	}
	if wizard.quitting {
		m.wizard = nil
		return m, m.profiles()
	}
	m.wizard = &wizard
	return m, cmd
}

// spinning is whether anything on screen is waiting on something. A tick is
// answered with the next one, so anything that stops answering stops the
// spinner for good: a screen that draws one and is not named here draws a still
// picture of one.
func (m Model) spinning() bool {
	return m.loading || m.ai.busy != "" || m.talk.busy || m.talk.loading
}

func (m Model) statement() string { return m.editor.Value() }

// resultsHeight is what the result gets: the rest of the pane, or the whole
// body when it is zoomed.
func (m Model) resultsHeight() int {
	if m.zoomed {
		return ui.BodyHeight(m.height) - 4
	}
	return max(ui.BodyHeight(m.height)-m.editorRows()-5, minResultsRows)
}

// editorRows is half the pane unless it has been resized, which is the split
// someone writing a statement against a result actually wants.
func (m Model) editorRows() int {
	half := (ui.BodyHeight(m.height) - 4) / 2
	if m.split > 0 {
		half = m.split
	}
	return min(max(half, minEditorRows), max(ui.BodyHeight(m.height)-6, minEditorRows))
}

func (m Model) resizeEditor(step int) (tea.Model, tea.Cmd) {
	m.split = m.editorRows() + step
	m.editor.SetHeight(m.editorRows())
	m.results = m.results.resize(m.paneWidth(), m.resultsHeight())
	return m, nil
}

func (m Model) recordPage() (tea.Model, tea.Cmd) {
	page, ok := m.results.record()
	if !ok {
		return m, nil
	}
	m.page = &page
	return m, nil
}

func (m Model) key(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.wizard != nil {
		return m.toWizard(msg)
	}
	if m.page != nil {
		return m.pageKey(msg)
	}
	if m.modal != nil {
		return m.modalKey(msg)
	}
	if m.chooser != nil {
		return m.chooserKey(msg)
	}
	if m.palette != nil {
		return m.paletteKey(msg)
	}
	if m.view == viewQuery {
		return m.queryKey(msg)
	}
	if m.view == viewSwitch {
		return m.switchKey(msg)
	}
	if m.view == viewCatalog {
		return m.catalogKey(msg)
	}
	if m.view == viewAsk {
		return m.askKey(msg)
	}
	if m.view == viewAI {
		return m.aiKey(msg)
	}
	if m.view == viewDashboard && m.onSessions && m.dashboardOwnsKey(msg) {
		return m.activityKey(msg)
	}
	if m.view == viewDashboard && !m.onSessions {
		if moved, handled := m.walkReadings(msg); handled {
			return moved.follow(), nil
		}
		if key.Matches(msg, m.keys.Choose) {
			return m.readingPage()
		}
	}
	if m.view == viewSchema || m.view == viewIndexes {
		if moved, cmd, handled := m.browseKey(msg); handled {
			return moved, cmd
		}
		switch {
		case key.Matches(msg, m.keys.Up):
			moved, _ := m.walkListing(-1)
			return moved, nil
		case key.Matches(msg, m.keys.Down):
			moved, _ := m.walkListing(1)
			return moved, nil
		case key.Matches(msg, m.keys.Choose):
			return m.listingPage()
		}
	}
	switch {
	case key.Matches(msg, m.keys.Focus):
		return m.toggleSessions()
	case key.Matches(msg, m.keys.Catalog):
		return m.browseCatalog()
	case m.keys.opensPalette(msg, false):
		return m.openPalette()
	case key.Matches(msg, m.keys.Connections):
		return m.browse()
	case key.Matches(msg, m.keys.Quit):
		return m.confirmQuit()
	case key.Matches(msg, m.keys.Back):
		return m.show(viewDashboard)
	case key.Matches(msg, m.keys.Schema):
		return m.show(viewSchema)
	case key.Matches(msg, m.keys.Indexes):
		return m.show(viewIndexes)
	case key.Matches(msg, m.keys.Help):
		return m.show(viewHelp)
	case key.Matches(msg, m.keys.Query):
		return m.show(viewQuery)
	case key.Matches(msg, m.keys.Ask):
		return m.show(viewAsk)
	case key.Matches(msg, m.keys.Reload):
		m.loading = true
		return m, tea.Batch(m.load(), m.spinner.Tick)
	}
	return m.scroll(msg), nil
}

// dashboardOwnsKey keeps the keys that only mean something to the list of
// sessions away from the rest of the dashboard.
func (m Model) dashboardOwnsKey(msg tea.KeyPressMsg) bool {
	return key.Matches(msg, m.keys.Up, m.keys.Down, m.keys.Cancel, m.keys.Terminate, m.keys.Choose)
}

// walkReadings moves the cursor through the health report, which is the list
// the dashboard is made of.
func (m Model) walkReadings(msg tea.KeyPressMsg) (Model, bool) {
	total := len(m.readings(every))
	if total == 0 {
		return m, false
	}
	switch {
	case key.Matches(msg, m.keys.Up):
		m.reading = (m.reading - 1 + total) % total
	case key.Matches(msg, m.keys.Down):
		m.reading = (m.reading + 1) % total
	default:
		return m, false
	}
	return m, true
}

// walkListing moves the cursor through the tables or the indexes, whichever
// list is on screen.
// walkListing moves the cursor by a step, wrapping, and brings the row it
// lands on into view.
func (m Model) walkListing(step int) (Model, bool) {
	total := m.listed()
	if total == 0 {
		return m, false
	}
	m.listing = (m.listing + step + total) % total
	return m.follow(), true
}

// listed is how many rows the screen is drawing, which is the filtered count
// and not what the server returned.
func (m Model) listed() int {
	if m.view == viewIndexes {
		return len(m.shownIndexes())
	}
	return len(m.shownTables())
}

// follow keeps the row under the cursor on screen after it moves.
//
// The dashboard marks its cursor with a bar in the margin and can be searched
// for it. A catalogue list paints the whole row instead, so there is no glyph
// to look for, and the row's position has to be counted rather than found: a
// search would match the first bar of a gauge and scroll to the wrong place.
func (m Model) follow() Model {
	if m.view == viewSchema || m.view == viewIndexes {
		return m.followRow(m.listing + m.rowsAbove())
	}
	return m.followRow(ui.LineOf(m.body(), "▌"))
}

// rowsAbove is what a catalogue list draws before its first row: the title and
// its rule, a blank line, the filter when it is open, and the column headings
// with the rule under them.
func (m Model) rowsAbove() int {
	above := screenTitleRows + headingRows
	if at := m.lists[m.which()]; at.typing || at.active() {
		above++
	}
	return above
}

const (
	screenTitleRows = 3
	headingRows     = 2
)

func (m Model) followRow(line int) Model {
	area := ui.BodyHeight(m.height)
	switch {
	case line < 0:
		return m
	case line < m.offset:
		m.offset = line
	case line >= m.offset+area:
		m.offset = line - area + 1
	}
	return m
}

func (m Model) toggleSessions() (tea.Model, tea.Cmd) {
	if m.view != viewDashboard || len(m.running.sessions) == 0 {
		return m, nil
	}
	m.onSessions = !m.onSessions
	m.running = m.running.focus(m.onSessions)
	return m, nil
}

func (m Model) show(target view) (tea.Model, tea.Cmd) {
	m.view = target
	m.offset = 0
	m.listing = 0
	m.onSessions = false
	m.running = m.running.focus(false)
	if target == viewDashboard {
		m.generation++
		return m, tea.Batch(m.readSessions(), m.tick())
	}
	if target == viewQuery {
		return m, m.editor.Focus()
	}
	if target == viewAsk {
		warmed, warming := m.warming()
		loading, load := warmed.load4Talk()
		return loading, tea.Batch(loading.talk.prompt.Focus(), warming, load)
	}
	m.editor.Blur()
	if target == viewCatalog {
		return m.browseCatalog()
	}
	if target == viewAI {
		warmed, warming := m.warming()
		return warmed.read4AI(), warming
	}
	m.editor.Blur()
	return m, nil
}

func (m Model) scroll(msg tea.KeyPressMsg) Model {
	switch {
	case key.Matches(msg, m.keys.Up):
		return m.scrolled(-1)
	case key.Matches(msg, m.keys.Down):
		return m.scrolled(1)
	case msg.String() == "pgup":
		return m.scrolled(-ui.BodyHeight(m.height))
	case msg.String() == "pgdown":
		return m.scrolled(ui.BodyHeight(m.height))
	}
	return m
}

// scrolled walks the body by a number of rows, however it was asked to.
func (m Model) scrolled(step int) Model {
	m.offset += step
	if m.offset < 0 {
		m.offset = 0
	}
	if limit := ui.MaxOffset(m.body(), ui.BodyHeight(m.height)); m.offset > limit {
		m.offset = limit
	}
	return m
}

// wheeled is the mouse doing what the keys already do. A terminal program that
// ignores the wheel is a terminal program people scroll with their hand off the
// keyboard and nothing happens, which reads as a program that has frozen.
func (m Model) wheeled(msg tea.MouseWheelMsg) (tea.Model, tea.Cmd) {
	step := wheelRows
	switch msg.Button {
	case tea.MouseWheelUp:
		step = -wheelRows
	case tea.MouseWheelDown:
	default:
		return m, nil
	}
	if m.view == viewAsk && m.chooser == nil && m.modal == nil && m.page == nil {
		return m.rolled4Ask(step), nil
	}
	return m.scrolled(step), nil
}

// wheelRows is how far one notch of a wheel goes. Three is what a terminal
// sends a line-scrolling program, and what every other program in a terminal
// moves by.
const wheelRows = 3

func (m Model) openPalette() (tea.Model, tea.Cmd) {
	opened := newPalette(m.theme, m.keys)
	m.palette = &opened
	return m, opened.filter.Focus()
}

func (m Model) paletteKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Back), m.keys.opensPalette(msg, true):
		m.palette = nil
		return m, nil
	case key.Matches(msg, m.keys.Choose):
		chosen, ok := m.palette.selected()
		m.palette = nil
		if !ok {
			return m, nil
		}
		return m, func() tea.Msg { return chosen.msg }
	case msg.String() == "up":
		moved := m.palette.move(-1)
		m.palette = &moved
		return m, nil
	case msg.String() == "down":
		moved := m.palette.move(1)
		m.palette = &moved
		return m, nil
	}
	edited, cmd := m.palette.edit(msg)
	m.palette = &edited
	return m, cmd
}

// queryKey answers the editor screen. A list of suggestions takes the arrows
// and nothing else: every other binding in this program answers to a letter as
// well, and a letter typed into an editor is a letter.
func (m Model) queryKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.suggest.active() {
		switch {
		case key.Matches(msg, m.keys.Accept):
			return m.accept()
		case key.Matches(msg, m.keys.Above):
			m.suggest = m.suggest.move(-1)
			return m, nil
		case key.Matches(msg, m.keys.Below):
			m.suggest = m.suggest.move(1)
			return m, nil
		case key.Matches(msg, m.keys.Back):
			m.suggest.items = nil
			return m, nil
		}
	}
	switch {
	case key.Matches(msg, m.keys.Leave):
		return m.confirmQuit()
	case key.Matches(msg, m.keys.Back):
		if m.zoomed {
			m.zoomed = false
			return m, nil
		}
		return m.show(viewDashboard)
	case m.keys.opensPalette(msg, m.focus == focusEditor):
		m.editor.Blur()
		return m.openPalette()
	case key.Matches(msg, m.keys.Connections):
		m.editor.Blur()
		return m.browse()
	case key.Matches(msg, m.keys.Catalog):
		m.editor.Blur()
		return m.browse()
	case key.Matches(msg, m.keys.Run):
		return m.attempt()
	case key.Matches(msg, m.keys.Sidebar):
		return m.toggleSidebar()
	case key.Matches(msg, m.keys.Grow):
		return m.resizeEditor(2)
	case key.Matches(msg, m.keys.Shrink):
		return m.resizeEditor(-2)
	case key.Matches(msg, m.keys.Focus):
		return m.nextPane()
	}
	switch m.focus {
	case focusSidebar:
		return m.explorerKey(msg)
	case focusResults:
		switch {
		case key.Matches(msg, m.keys.Zoom):
			m.zoomed = !m.zoomed
			m.results = m.results.resize(m.paneWidth(), m.resultsHeight())
			return m, nil
		case key.Matches(msg, m.keys.Left):
			m.results = m.results.shift(-1)
			return m, nil
		case key.Matches(msg, m.keys.Right):
			m.results = m.results.shift(1)
			return m, nil
		case key.Matches(msg, m.keys.Choose):
			return m.recordPage()
		}
		updated, cmd := m.results.update(msg)
		m.results = updated
		return m, cmd
	}
	updated, cmd := m.editor.Update(msg)
	m.editor = updated
	typed, suggesting := m.resuggest()
	return typed, tea.Batch(cmd, suggesting)
}

// nextPane walks the workbench: the schema, the statement, the result.
func (m Model) nextPane() (tea.Model, tea.Cmd) {
	for range 3 {
		m.focus = (m.focus + 1) % 3
		if m.reachable(m.focus) {
			break
		}
	}
	if m.focus == focusEditor {
		return m, m.editor.Focus()
	}
	if m.focus == focusSidebar {
		m.sidebar = m.sidebar.onTable()
	}
	m.editor.Blur()
	return m, nil
}

func (m Model) reachable(target pane) bool {
	switch target {
	case focusResults:
		return m.results.present && m.results.failure == ""
	case focusSidebar:
		return !m.sidebar.hidden && len(m.sidebar.rows) > 0
	default:
		return true
	}
}

func (m Model) toggleSidebar() (tea.Model, tea.Cmd) {
	m.sidebar.hidden = !m.sidebar.hidden
	if m.sidebar.hidden && m.focus == focusSidebar {
		m.focus = focusEditor
		return m, m.editor.Focus()
	}
	return m, nil
}

// attempt is what pressing run does with each of the three verdicts. A refusal
// says so rather than doing nothing, because a key that silently does nothing
// is a broken key. A statement that changes data is asked about, once, unless
// the profile has said not to ask.
func (m Model) attempt() (tea.Model, tea.Cmd) {
	statement := m.statement()
	verdict := m.Verdict()
	switch {
	case verdict.Blocked():
		return m, m.notify(ui.Reason(verdict.Reason))
	case verdict.NeedsConfirmation() && m.session.Settings.Safety.ConfirmQueries:
		m.modal = m.confirmRun(statement, verdict)
		return m, nil
	}
	return m, m.run(statement)
}

// confirmRun is the dialog a write raises. It shows the statement it is about
// to send, highlighted the way it is everywhere else, and why the classifier
// thinks it is worth asking about.
func (m Model) confirmRun(statement string, verdict sqlguard.Result) *modal {
	dialog := ask(m.theme, "run this statement?", "", runMsg{statement: statement})
	dialog.tag = m.theme.Mode(cli.Mode(m.session.Connection.Mode).Label())
	dialog.danger = true
	dialog.warn = ui.Reason(verdict.Reason)
	dialog.code = statement
	return dialog
}

type runMsg struct{ statement string }

func (m Model) Verdict() sqlguard.Result {
	return m.session.Guard.Classify(m.statement(), cli.Mode(m.session.Connection.Mode))
}

func (m Model) View() tea.View {
	var v tea.View
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.BackgroundColor = m.theme.P.Bg
	v.ForegroundColor = m.theme.P.Fg
	v.WindowTitle = "tui4db, " + m.session.Connection.Name
	v.SetContent(m.theme.Base.Render(m.content()))
	return v
}

func (m Model) content() string {
	if m.wizard != nil {
		return m.wizard.content()
	}
	body, more := ui.Window(m.body(), m.offset, ui.BodyHeight(m.height))
	screen := m.theme.Chrome(ui.Frame{
		Width:  m.width,
		Height: m.height,
		Env:    ui.EnvColor(m.session.Connection.Color),
		Header: m.header(),
		Body:   body,
		Footer: m.footer(more),
	})
	if m.page != nil {
		return ui.Overlay(screen, m.page.view(m.width, m.height), m.width, m.height)
	}
	if m.modal != nil {
		return ui.Overlay(screen, m.modal.view(m.width), m.width, m.height)
	}
	if m.chooser != nil {
		return ui.Overlay(screen, m.chooserView(m.width, m.height), m.width, m.height)
	}
	if m.palette != nil {
		return ui.Overlay(screen, m.palette.view(m.width, m.height), m.width, m.height)
	}
	if m.view == viewQuery && m.suggest.active() {
		return m.withSuggestions(screen)
	}
	if toast := m.render(m.theme); toast != "" {
		return ui.Corner(screen, toast, m.width, m.height)
	}
	return screen
}

// at is how the screen wants its rows drawn, which is where the cursor is and
// what the list was put in the order of.
func (m Model) at(width int) ui.List {
	list := m.lists[m.which()]
	return ui.List{
		Cursor: m.listing, Width: width, Sort: list.column, Reversed: list.reversed,
	}
}

// blank reports a screen with nothing on it yet. A first read has to say it is
// working, because there is nothing else to look at. A read that refreshes what
// is already drawn must not replace it: swapping a full screen for a spinner
// and back again five times a minute is what makes a dashboard blink.
func (m Model) blank() bool {
	return len(m.findings) == 0 && len(m.tables) == 0 && len(m.indexes) == 0
}

func (m Model) header() string {
	return m.theme.IdentityLine(
		ui.EnvColor(m.session.Connection.Color),
		ui.Slashed(m.session.Connection.Name, m.database()),
		strings.ToLower(m.session.Info.Driver)+" "+m.session.Info.Version,
		sqlguard.Mode(m.session.Connection.Mode).Label(),
	)
}

// database is what the server calls the database in use. A file backed driver
// keeps its path out of the header, since the connection already names it.
func (m Model) database() string {
	if m.session.Connection.File != "" {
		return ""
	}
	return m.session.Info.Database
}

func (m Model) body() string {
	if m.failure != "" {
		return m.theme.Error.Render("✗ " + m.failure)
	}
	if m.loading && m.blank() {
		return m.spinner.View() + m.theme.Muted.Render(" reading the server")
	}
	width := ui.FrameWidth(m.width)
	switch m.view {
	case viewSchema:
		shown := m.shownTables()
		return m.schemaBody("tables", len(shown), m.theme.TableList(shown, m.at(width)))
	case viewIndexes:
		shown := m.shownIndexes()
		list := m.at(width)
		list.Busiest = ui.Busiest(m.indexes)
		return m.schemaBody("indexes", len(shown), m.theme.IndexList(shown, list))
	case viewQuery:
		return m.workbench()
	case viewHelp:
		return m.helpBody()
	case viewAsk:
		return m.askBody()
	case viewAI:
		return m.aiBody()
	case viewSwitch:
		return m.list.view(ui.FrameWidth(m.width))
	case viewCatalog:
		return m.catalog.view(ui.FrameWidth(m.width))
	default:
		return m.dashboardBody()
	}
}

func (m Model) footer(more int) string {
	left := m.help.View(m.keys.footer(m.view, m.suggest.active(), m.zoomed, m.onSessions, m.lists[m.which()].typing))
	return ui.SplitLine(left, m.theme.Subtle.Render(scrollHint(more)), ui.FrameWidth(m.width))
}

func scrollHint(more int) string {
	if more <= 0 {
		return ""
	}
	return fmt.Sprintf("↓ %d more", more)
}
