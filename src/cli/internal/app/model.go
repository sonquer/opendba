package app

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/sonquer/opendba/src/cli/internal/cli"
	"github.com/sonquer/opendba/src/cli/internal/config"
	"github.com/sonquer/opendba/src/cli/internal/driver"
	"github.com/sonquer/opendba/src/cli/internal/export"
	"github.com/sonquer/opendba/src/cli/internal/ui"
	"github.com/sonquer/opendba/src/cli/pkg/sqlguard"
)

type view string

const (
	viewDashboard view = "dashboard"
	viewSchema    view = "schema"
	viewIndexes   view = "indexes"
	viewQuery     view = "query"
	viewHelp      view = "help"
	viewAsk       view = "ask"
	viewAI        view = "ai"
	viewHistory   view = "history"
	viewSettings  view = "settings"
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

	// on says which connection was read, since a read that was asked for on one
	// lands while another may be in front.
	on sessionID
}

type queriedMsg struct {
	statement string
	columns   []string
	rows      [][]any
	duration  time.Duration
	truncated bool
	err       error

	// token says which run this result belongs to.
	token int
}

type Model struct {
	// link is the connection being worked through, and links is every one that is
	// open. A tab belongs to one of them, the way it belongs to a statement.
	link
	links  []link
	linked int

	workspace cli.Workspace
	theme     *ui.Theme

	// settings is what settings.toml says, which is a fact of the file rather than
	// of any one connection: several of them open at once would otherwise hold as
	// many stale copies of it.
	settings config.Settings
	view     view
	width    int
	height   int
	quitting bool
	toaster
	switcher *switcher
	catalog  *catalog

	// configured is what profiles.toml holds, kept on the program rather than on
	// the dialog that shows it: closing the dialog must not lose the list, and a
	// connection that would not open has to be able to say so on its row.
	configured []config.Connection
	trouble    map[string]string
	wizard     *SetupModel
	palette    *palette
	modal      *modal
	page       *details
	plan       *plan
	chats      *chatList

	// preferences is the settings screen, built when it is opened rather than held,
	// because it is a form over a file that anything else may have changed since
	// the last look.
	preferences *preferences
	reading     int
	listing     int
	lists       [2]browse
	keys        keymap
	help        help.Model
	offset      int

	// worksheet is the tab being worked in, and sheets is every tab there is.
	worksheet
	sheets []worksheet
	sheet  int

	// mouse is whether the terminal has been asked to report the mouse.
	mouse bool

	// dragging is whether the line between the statement and its result is being
	// carried by the pointer, which is the one drag that is not a selection.
	dragging bool

	// runs stamps every statement sent, so a result can say which tab asked for
	// it and a result nobody is waiting for can be told from one they are.
	runs int

	// minted is how many connections have been opened since the program started,
	// which is where the next session's id comes from. It only ever rises, so an
	// id is never handed out twice.
	minted int

	recall     recall
	onSessions bool
	generation int
	beat       int
	focus      pane
	spinner    spinner.Model

	ai         aiSettings
	chooser    *chooser
	pending    string
	stopFetch  context.CancelFunc
	stopLoad   context.CancelFunc
	stopExport context.CancelFunc
	exporter   *exporter
}

// Talk builds a conversation once it has been told how to ask for permission.
type Talk func(allowed permission) (conversation, error)

// WithAssistant gives the model something to have a conversation with.
func (m Model) WithAssistant(instance string, build Talk) Model {
	m.talk = newChat(m.theme, instance)
	m.build = build
	return m.stowLink()
}

// pane is what the keys are talking to inside the editor screen.
type pane int

const (
	focusEditor pane = iota
	focusResults
	focusSidebar
)

// firstSession is the id the connection the program starts on is given. Ids
// start at one so that a zero is a usable "no session at all".
const firstSession sessionID = 1

func NewModel(session cli.Session, workspace cli.Workspace) Model {
	theme := session.Theme
	loader := spinner.New()
	loader.Spinner = spinner.Dot
	loader.Style = lipgloss.NewStyle().Foreground(theme.P.Accent)
	hints := help.New()
	hints.Styles = helpStyles(theme)
	hints.ShortSeparator = "  "
	hints.FullSeparator = "   "
	first := newWorksheet(theme, sheetQuery, "", firstSession)
	opened := newLink(firstSession, 1, session, session.Settings, session.Release)
	opened.loading = true
	return Model{
		keys:      newKeymap(),
		help:      hints,
		settings:  session.Settings,
		trouble:   map[string]string{},
		minted:    int(firstSession),
		link:      opened,
		links:     []link{opened},
		workspace: workspace,
		theme:     theme,
		view:      viewDashboard,
		width:     96,
		height:    32,
		mouse:     session.Settings.Appearance.MouseWanted(),
		worksheet: first,
		sheets:    []worksheet{first},
		spinner:   loader,
		lists:     [2]browse{newBrowse(theme, 0, false), newBrowse(theme, 2, true)},
		recall:    newRecall(theme),
		ai:        newAISettings(theme),
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

// release closes every connection this program opened. The one it started on is
// closed by the caller, which is why that one carries no closer of its own.
func (m Model) release() {
	for _, open := range m.eachLink() {
		if open.assistant != nil {
			_ = open.assistant.Close()
		}
		if open.close != nil {
			open.close()
		}
	}
}

// load reads everything: the health of the server, the catalogue it holds, and
// the statements kept beside it.
func (m Model) load() tea.Cmd {
	return tea.Batch(m.readHealth(), m.readCatalogue(), m.readFiles())
}

// readHealth is the part of the dashboard that moves. It is cheap enough to
// repeat and is the only thing the refresh reads besides the sessions.
func (m Model) readHealth() tea.Cmd {
	conn := m.session.Conn
	on := m.link.key()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
		defer cancel()

		findings, err := conn.Health(ctx)
		if err != nil {
			return loadedMsg{part: partHealth, err: err, on: on}
		}
		return loadedMsg{part: partHealth, findings: findings, on: on}
	}
}

// readCatalogue is what the server holds rather than what it is doing.
func (m Model) readCatalogue() tea.Cmd {
	conn := m.session.Conn
	connection := m.session.Connection
	on := m.link.key()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
		defer cancel()

		tables, err := conn.Tables(ctx, one(connection))
		if err != nil {
			return loadedMsg{part: partCatalogue, err: err, on: on}
		}
		indexes, err := conn.Indexes(ctx, one(connection))
		if err != nil {
			return loadedMsg{part: partCatalogue, err: err, on: on}
		}
		return loadedMsg{
			part:    partCatalogue,
			on:      on,
			tables:  scoped(tables, connection.Only, func(t driver.Table) string { return t.Schema }),
			indexes: scoped(indexes, connection.Only, func(i driver.Index) string { return i.Schema }),
		}
	}
}

const readTimeout = 30 * time.Second

// loaded applies whichever half of a read came back, to the connection it was
// read from rather than to whichever one is in front by the time it lands.
func (m Model) loaded(msg loadedMsg) (tea.Model, tea.Cmd) {
	read := m.linked4Sheet(msg.on)
	read.loading = false
	switch {
	case msg.err != nil:
		read.failure, read.failing = msg.err.Error(), msg.part
	case msg.part == partHealth:
		read.findings = msg.findings
		read.read = true
	case msg.part == partCatalogue:
		read.tables, read.indexes = msg.tables, msg.indexes
		read.sidebar = read.sidebar.withTables(read.tables, read.fields)
		read.read = true
	}
	if msg.err == nil && read.failing == msg.part {
		read.failure, read.failing = "", ""
	}
	return m.wrote4Link(msg.on, read), nil
}

// one is the schema a driver can filter on its own.
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

// run sends a statement and keeps hold of the way to stop it.
func (m Model) run(statement string) (Model, tea.Cmd) {
	conn := m.session.Conn
	ctx, stop := context.WithCancel(context.Background())
	m.stopQuery = stop
	m.began = time.Now()
	m.inflight = true
	m.runs++
	m.token = m.runs
	token := m.runs
	query := func() tea.Msg {
		result, err := conn.Query(ctx, statement)
		if err != nil {
			return queriedMsg{statement: statement, err: err, token: token}
		}
		message := queriedMsg{statement: statement, columns: result.Columns(), token: token}
		for result.Next() {
			message.rows = append(message.rows, result.Values())
		}
		message.duration = result.Duration()
		message.truncated = result.Truncated()
		message.err = driver.Finish(result)
		return message
	}
	return m, tea.Batch(query, m.spinner.Tick)
}

// haltMsg is the command list giving up on the statement this tab is waiting
// for, which is the key a terminal may swallow said in words instead.
type haltMsg struct{}

// halt gives up on the statement that is running.
func (m Model) halt() Model {
	if m.stopQuery != nil {
		m.stopQuery()
	}
	return m
}

// elapsed is how long the statement that is running has been running, drawn
// beside the spinner so that a slow query reads as slow rather than as broken.
func (m Model) elapsed() string {
	return m.spinner.View() + m.theme.Muted.Render(" running "+
		driver.Duration(time.Since(m.began))) + "  " +
		m.theme.Subtle.Render(m.keys.Halt().Help().Key+" cancel")
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
		return m.returned(msg)
	case recalledMsg:
		return m.recalled(msg)
	case toastMsg:
		m.expire(msg)
		return m, nil
	case filesMsg:
		return m.listedFiles(msg)
	case openedFileMsg:
		return m.openedFile(msg)
	case saveFileMsg:
		return m.saveSheet()
	case nameFileMsg:
		return m.saveNamed(msg)
	case wroteFileMsg:
		return m.wroteFile(msg)
	case deleteFileMsg:
		return m, m.removeFile(msg)
	case deletedFileMsg:
		return m.deletedFile(msg)
	case profilesMsg:
		return m.listed4Switch(msg), nil
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
		read := m.linked4Sheet(msg.on)
		read.running = read.running.withSessions(msg, ui.FrameWidth(m.width))
		return m.wrote4Link(msg.on, read), nil
	case tickMsg:
		return m.refreshed(msg)
	case stopMsg:
		return m, m.stop(msg)
	case stoppedMsg:
		return m.stopped(msg)
	case catalogMsg:
		if m.catalog != nil && m.catalog.on == msg.on {
			m.catalog.withCatalog(msg)
		}
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
	case runNowMsg:
		return m.attempt()
	case haltMsg:
		return m.halt(), nil
	case runMsg:
		return m.run(msg.statement)
	case switchMsg:
		return m.openSwitcher()
	case catalogNowMsg:
		return m.openCatalog()
	case disconnectMsg:
		return m.disconnected(msg)
	case newConnectionMsg:
		return m.compose()
	case preferencesMsg:
		return m.openPreferences()
	case forgetHistoryMsg:
		return m.forgetHistory()
	case forgetChatsMsg:
		return m.forgetChats()
	case clearedMsg:
		return m.cleared(msg)
	case openChatsMsg:
		return m.openChats()
	case listedChatsMsg:
		return m.listedChats(msg)
	case openedChatMsg:
		return m.openedChat(msg)
	case forgetChatMsg:
		return m.forgetChat(msg)
	case newChatMsg:
		return m.startChat()
	case keptMsg:
		return m.wasKept(msg)
	case explainMsg:
		return m.explain(msg)
	case explainedMsg:
		return m.explained(msg)
	case exportMsg:
		return m.export4Result()
	case writeExportMsg:
		if m.exporter == nil {
			return m, nil
		}
		return m.startExport()
	case exportProgressMsg:
		return m.exporting(msg)
	case exportEndedMsg:
		return m.exported(msg)
	case mouseMsg:
		return m.tookMouse()
	case newSheetMsg:
		opened, cmd := focused(m.openSheet(newWorksheet(m.theme, sheetQuery, "", m.link.key())))
		return opened.(Model).show4Tabs(cmd)
	case closeSheetMsg:
		closed, cmd := focused(m.closeSheet(m.sheet))
		return closed.(Model).show4Tabs(cmd)
	case askCloseMsg:
		return m.confirmClose()
	case walkSheetMsg:
		walked, cmd := focused(m.walkSheets(msg.step))
		return walked.(Model).show4Tabs(cmd)
	case gotoSheetMsg:
		gone, cmd := focused(m.onSheet(msg.index))
		return gone.(Model).show4Tabs(cmd)
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
	case tea.MouseClickMsg:
		return m.clicked(msg)
	case tea.MouseMotionMsg:
		return m.dragged(msg)
	case tea.MouseReleaseMsg:
		return m.dropped(msg)
	case copyMsg:
		return m.copied(msg)
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

// spinning is whether anything on screen is waiting on something.
func (m Model) spinning() bool {
	return m.loading || m.running4Tabs() > 0 || m.ai.busy != "" || m.talk.busy || m.talk.loading
}

// running4Tabs is how many tabs are waiting on a statement, which is not only
// the one in front: leaving the editor screen leaves the statement running.
func (m Model) running4Tabs() int {
	waiting := 0
	for _, sheet := range m.eachSheet() {
		if sheet.inflight {
			waiting++
		}
	}
	return waiting
}

func (m Model) statement() string { return m.editor.Value() }

// resultsHeight is what the result gets: the rest of the pane, or the whole
// body when it is zoomed.
func (m Model) resultsHeight() int {
	if m.zoomed {
		return ui.BodyHeight(m.height) - 4
	}
	return max(m.workbenchHeight()-m.editorRows()-5, minResultsRows)
}

// editorRows is half the pane unless it has been resized, which is the split
// someone writing a statement against a result actually wants.
func (m Model) editorRows() int {
	half := (m.workbenchHeight() - 4) / 2
	if m.split > 0 {
		half = m.split
	}
	return min(max(half, minEditorRows), max(m.workbenchHeight()-6, minEditorRows))
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
	if m.plan != nil {
		return m.planKey(msg)
	}
	if m.modal != nil {
		return m.modalKey(msg)
	}
	if m.switcher != nil {
		return m.switcherKey(msg)
	}
	if m.catalog != nil {
		return m.catalogKey(msg)
	}
	if m.chats != nil {
		return m.chatsKey(msg)
	}
	if m.exporter != nil {
		return m.exportKey(msg)
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
	if m.view == viewAsk {
		return m.askKey(msg)
	}
	if m.view == viewAI {
		return m.aiKey(msg)
	}
	if m.view == viewHistory {
		return m.historyKey(msg)
	}
	if m.view == viewSettings && m.preferences != nil {
		return m.preferencesKey(msg)
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
		return m.openCatalog()
	case m.keys.opensPalette(msg, false):
		return m.openPalette()
	case key.Matches(msg, m.keys.Connections):
		return m.openSwitcher()
	case key.Matches(msg, m.keys.Quit):
		return m.confirmQuit()
	case key.Matches(msg, m.keys.Back):
		return m.show(viewDashboard)
	case key.Matches(msg, m.keys.History):
		return m.show(viewHistory)
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

// walkListing moves the cursor through the tables or the indexes, whichever list
// is on screen.
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
		return warmed, tea.Batch(warmed.talk.prompt.Focus(), warming)
	}
	m.editor.Blur()
	if target == viewHistory {
		m.recall.loading = true
		return m, m.readHistory()
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

// overlaid reports whether something is drawn over the screen, which is what
// makes a click, a wheel and the terminal's own cursor belong to the overlay
// rather than to what is behind it. It is one list rather than one per caller,
// because four hand-copied lists of the same fact had already drifted apart.
func (m Model) overlaid() bool {
	return m.wizard != nil || m.page != nil || m.plan != nil || m.modal != nil ||
		m.switcher != nil || m.catalog != nil || m.chats != nil ||
		m.exporter != nil || m.chooser != nil || m.palette != nil
}

// wheeled is the mouse doing what the keys already do.
func (m Model) wheeled(msg tea.MouseWheelMsg) (tea.Model, tea.Cmd) {
	if !m.mouse {
		return m, nil
	}
	step := wheelRows
	switch msg.Button {
	case tea.MouseWheelUp:
		step = -wheelRows
	case tea.MouseWheelDown:
	default:
		return m, nil
	}
	if m.view == viewAsk && !m.overlaid() {
		return m.rolled4Ask(step), nil
	}
	if m.view == viewQuery && !m.overlaid() {
		return m.rolled4Query(msg.Mouse().X, step), nil
	}
	return m.scrolled(step), nil
}

// wheelRows is how far one notch of a wheel goes.
const wheelRows = 3

func (m Model) openPalette() (tea.Model, tea.Cmd) {
	opened := newPalette(m.theme, m.keys, m.view)
	if m.view == viewQuery {
		opened = opened.withTabs(m.commands())
	}
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

// queryKey answers the editor screen.
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
	case key.Matches(msg, m.keys.Stop4Query):
		return m.halt(), nil
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
		return m.openSwitcher()
	case key.Matches(msg, m.keys.Catalog):
		return m.openCatalog()
	case key.Matches(msg, m.keys.Run):
		return m.attempt()
	case key.Matches(msg, m.keys.Export):
		return m.export4Result()
	case key.Matches(msg, m.keys.Write):
		return m.saveSheet()
	case key.Matches(msg, m.keys.Explain):
		return m.explain(explainMsg{})
	case key.Matches(msg, m.keys.History):
		m.editor.Blur()
		return m.show(viewHistory)
	case key.Matches(msg, m.keys.Jump):
		return focused(m.onSheet(jumped(msg)))
	case key.Matches(msg, m.keys.NewTab):
		return focused(m.openSheet(newWorksheet(m.theme, sheetQuery, "", m.link.key())))
	case key.Matches(msg, m.keys.CloseTab):
		return m.confirmClose()
	case key.Matches(msg, m.keys.PrevTab):
		return focused(m.walkSheets(-1))
	case key.Matches(msg, m.keys.NextTab):
		return focused(m.walkSheets(1))
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
		case key.Matches(msg, m.keys.Copy):
			return m.copiedCell()
		case key.Matches(msg, m.keys.CopyRow):
			return m.copied(copyMsg{format: export.FormatCSV})
		case key.Matches(msg, m.keys.Up):
			m.results = m.results.move(-1)
			return m, nil
		case key.Matches(msg, m.keys.Down):
			m.results = m.results.move(1)
			return m, nil
		case msg.String() == "pgup":
			m.results = m.results.move(-m.results.visible())
			return m, nil
		case msg.String() == "pgdown":
			m.results = m.results.move(m.results.visible())
			return m, nil
		case msg.String() == "home":
			m.results = m.results.move(-len(m.results.rows))
			return m, nil
		case msg.String() == "end":
			m.results = m.results.move(len(m.results.rows))
			return m, nil
		}
		return m, nil
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

// attempt is what pressing run does with each of the three verdicts.
func (m Model) attempt() (tea.Model, tea.Cmd) {
	statement := m.script().chosen()
	verdict := m.classify(statement)
	switch {
	case verdict.Blocked():
		return m, m.notify(ui.Reason(verdict.Reason))
	case verdict.NeedsConfirmation() && m.settings.Safety.ConfirmQueries:
		m.modal = m.confirmRun(statement, verdict)
		return m, nil
	}
	return m.run(statement)
}

// confirmRun is the dialog a write raises.
func (m Model) confirmRun(statement string, verdict sqlguard.Result) *modal {
	dialog := ask(m.theme, "run this statement?", "", runMsg{statement: statement})
	dialog.tag = m.theme.Mode(cli.Mode(m.session.Connection.Mode).Label())
	dialog.danger = true
	dialog.warn = ui.Reason(verdict.Reason)
	dialog.code = statement
	return dialog
}

type runMsg struct{ statement string }

// runNowMsg is the command list asking for the statement to be run, which goes
// through the same classification a key does rather than round it.
type runNowMsg struct{}

// Verdict is what the guard makes of the statement that would run, which in a
// buffer holding a script is the one the cursor is in rather than all of them.
func (m Model) Verdict() sqlguard.Result { return m.classify(m.script().chosen()) }

// classify is what the guard makes of one statement, against the mode the
// connection this tab is worked through was opened in.
func (m Model) classify(statement string) sqlguard.Result { return m.link.classify(statement) }

// Settled returns a model that starts no work of its own in the background.
func (m Model) Settled() Model {
	m.ai.warmed = true
	return m
}

func (m Model) View() tea.View {
	var v tea.View
	v.AltScreen = true
	v.MouseMode = tea.MouseModeNone
	if m.mouse {
		v.MouseMode = tea.MouseModeCellMotion
	}
	v.BackgroundColor = m.theme.P.Bg
	v.ForegroundColor = m.theme.P.Fg
	v.WindowTitle = "opendba, " + m.session.Connection.Name
	v.Cursor = m.caret()
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
		Tabs:   m.strip(),
		Body:   body,
		Footer: m.footer(more),
	})
	if m.page != nil {
		return ui.Overlay(screen, m.page.view(m.width, m.height), m.width, m.height)
	}
	if m.plan != nil {
		return ui.Overlay(screen, m.plan.view(m.width, m.height), m.width, m.height)
	}
	if m.modal != nil {
		return ui.Overlay(screen, m.modal.view(m.width), m.width, m.height)
	}
	if m.switcher != nil {
		return ui.Overlay(screen, m.view4Switch(m.width, m.height), m.width, m.height)
	}
	if m.catalog != nil {
		return ui.Overlay(screen, m.view4Catalog(m.width, m.height), m.width, m.height)
	}
	if m.chats != nil {
		return ui.Overlay(screen, m.chats.view(m.width, m.height), m.width, m.height)
	}
	if m.exporter != nil {
		return ui.Overlay(screen, m.exporter.view(m.width, m.height), m.width, m.height)
	}
	if m.chooser != nil {
		return ui.Overlay(screen, m.chooserView(m.width, m.height), m.width, m.height)
	}
	if m.palette != nil {
		return ui.Overlay(screen, m.palette.view(m.width, m.height), m.width, m.height)
	}

	if toast := m.render(m.theme); toast != "" {
		return ui.TopRight(screen, toast, m.width, m.view == viewQuery && !m.zoomed)
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

// blank reports a screen with nothing on it yet.
func (m Model) blank() bool {
	return len(m.findings) == 0 && len(m.tables) == 0 && len(m.indexes) == 0
}

func (m Model) header() string {
	return ui.SplitLine(m.theme.IdentityLine(
		ui.EnvColor(m.session.Connection.Color),
		ui.Slashed(m.session.Connection.Name, m.database()),
		strings.ToLower(m.session.Info.Driver)+" "+m.session.Info.Version,
		sqlguard.Mode(m.session.Connection.Mode).Label(),
	), m.tally4Running(), ui.FrameWidth(m.width))
}

// tally4Running is what is still out, said on every screen rather than only on
// the one with the tabs on it. A statement left running is invisible from the
// dashboard otherwise, and invisible work is work nobody comes back for.
func (m Model) tally4Running() string {
	waiting := m.running4Tabs()
	switch {
	case waiting == 0:
		return ""
	case waiting == 1 && m.inflight:
		return m.spinner.View() + m.theme.Muted.Render(" running "+driver.Duration(time.Since(m.began)))
	default:
		return m.spinner.View() + m.theme.Muted.Render(" "+strconv.Itoa(waiting)+" running")
	}
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
	case viewHistory:
		return m.historyBody()
	case viewSettings:
		return m.preferencesBody()
	default:
		return m.dashboardBody()
	}
}

// footer is the row of keys, kept inside the frame. Off macOS a modifier is
// spelled out rather than drawn as a glyph, so the same keys are three times as
// wide and a row that fits on one machine runs off the other.
func (m Model) footer(more int) string {
	return ui.SplitLine(m.help.ShortHelpView(m.footerKeys()),
		m.theme.Subtle.Render(scrollHint(more)), ui.FrameWidth(m.width))
}

// scrollRoom is what the footer keeps free on the right for the scroll hint. It
// is a fixed allowance rather than the width of the hint that is there now,
// because a row of keys that grew and shrank while a list was scrolled would be
// a row nobody could aim at.
const scrollRoom = 13

// footerKeys is the keys the footer has room to draw, in order. Off macOS a
// modifier is spelled out rather than drawn as a glyph, so the same eight keys
// are half again as wide and a row that fits on one machine runs off the other,
// taking the width of every row of the screen with it. Drawing and clicking
// both go through here so that a key nobody can see is a key nobody can press.
func (m Model) footerKeys() []key.Binding {
	room := ui.FrameWidth(m.width) - scrollRoom
	offered := m.keys.footer(m.view, m.suggest.active(), m.zoomed, m.onSessions,
		m.lists[m.which()].typing, m.inflight)
	separator := lipgloss.Width(
		m.help.Styles.ShortSeparator.Inline(true).Render(m.help.ShortSeparator))
	kept := make([]key.Binding, 0, len(offered))
	at := 0
	for _, binding := range offered {
		if !binding.Enabled() {
			continue
		}
		width := lipgloss.Width(m.help.ShortHelpView([]key.Binding{binding}))
		if len(kept) > 0 {
			width += separator
		}
		if at+width > room {
			break
		}
		at += width
		kept = append(kept, binding)
	}
	return kept
}

func scrollHint(more int) string {
	if more <= 0 {
		return ""
	}
	return fmt.Sprintf("↓ %d more", more)
}
