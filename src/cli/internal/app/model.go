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
)

type loadedMsg struct {
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
	toaster
	list    connections
	catalog catalog
	wizard  *SetupModel
	palette *palette
	modal   *modal
	page    *details
	reading int
	listing int
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
	focus      pane
	zoomed     bool
	split      int
	spinner    spinner.Model
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
		suggest:   completion{theme: theme},
		sidebar:   newExplorer(theme),
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

func (m Model) load() tea.Cmd {
	conn := m.session.Conn
	connection := m.session.Connection
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		findings, err := conn.Health(ctx)
		if err != nil {
			return loadedMsg{err: err}
		}
		tables, err := conn.Tables(ctx, one(connection))
		if err != nil {
			return loadedMsg{err: err}
		}
		indexes, err := conn.Indexes(ctx, one(connection))
		if err != nil {
			return loadedMsg{err: err}
		}
		return loadedMsg{
			findings: findings,
			tables:   scoped(tables, connection.Only, func(t driver.Table) string { return t.Schema }),
			indexes:  scoped(indexes, connection.Only, func(i driver.Index) string { return i.Schema }),
		}
	}
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
		m.results = m.results.resize(m.paneWidth(), m.resultsHeight())
		if m.wizard != nil {
			return m.toWizard(msg)
		}
		return m, nil
	case spinner.TickMsg:
		if !m.loading {
			return m, nil
		}
		updated, cmd := m.spinner.Update(msg)
		m.spinner = updated
		return m, cmd
	case loadedMsg:
		m.loading = false
		if msg.err != nil {
			m.failure = msg.err.Error()
			return m, nil
		}
		m.findings, m.tables, m.indexes = msg.findings, msg.tables, msg.indexes
		m.sidebar = m.sidebar.withTables(m.tables, m.fields)
		return m, nil
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
	case newConnectionMsg:
		return m.compose()
	case quitMsg:
		m.quitting = true
		return m, tea.Quit
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
		if moved, handled := m.walkListing(msg); handled {
			return moved, nil
		}
		if key.Matches(msg, m.keys.Choose) {
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
func (m Model) walkListing(msg tea.KeyPressMsg) (Model, bool) {
	total := m.listed()
	if total == 0 {
		return m, false
	}
	switch {
	case key.Matches(msg, m.keys.Up):
		m.listing = (m.listing - 1 + total) % total
	case key.Matches(msg, m.keys.Down):
		m.listing = (m.listing + 1) % total
	default:
		return m, false
	}
	return m.follow(), true
}

func (m Model) listed() int {
	if m.view == viewIndexes {
		return len(m.indexes)
	}
	return len(m.tables)
}

// follow keeps the row under the cursor on screen after it moves.
func (m Model) follow() Model {
	area := ui.BodyHeight(m.height)
	line := ui.LineOf(m.body(), "▌")
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
	m.editor.Blur()
	return m, nil
}

func (m Model) scroll(msg tea.KeyPressMsg) Model {
	step := 0
	switch {
	case key.Matches(msg, m.keys.Up):
		step = -1
	case key.Matches(msg, m.keys.Down):
		step = 1
	case msg.String() == "pgup":
		step = -ui.BodyHeight(m.height)
	case msg.String() == "pgdown":
		step = ui.BodyHeight(m.height)
	default:
		return m
	}
	m.offset += step
	if m.offset < 0 {
		m.offset = 0
	}
	if limit := ui.MaxOffset(m.body(), ui.BodyHeight(m.height)); m.offset > limit {
		m.offset = limit
	}
	return m
}

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
		return m.browseCatalog()
	case key.Matches(msg, m.keys.Run):
		if !m.Verdict().Allowed() {
			return m, nil
		}
		return m, m.run(m.statement())
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

func (m Model) Verdict() sqlguard.Result {
	return m.session.Guard.Classify(m.statement(), cli.Mode(m.session.Connection.Mode))
}

func (m Model) View() tea.View {
	var v tea.View
	v.AltScreen = true
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
	if m.loading {
		return m.spinner.View() + m.theme.Muted.Render(" reading the server")
	}
	width := ui.FrameWidth(m.width)
	switch m.view {
	case viewSchema:
		return m.schemaBody("tables", m.theme.TableList(m.tables, m.listing, width))
	case viewIndexes:
		return m.schemaBody("indexes", m.theme.IndexList(m.indexes, m.listing, width))
	case viewQuery:
		return m.workbench()
	case viewHelp:
		return m.helpBody()
	case viewSwitch:
		return m.list.view(ui.FrameWidth(m.width))
	case viewCatalog:
		return m.catalog.view(ui.FrameWidth(m.width))
	default:
		return m.dashboardBody()
	}
}

func (m Model) footer(more int) string {
	left := m.help.View(m.keys.footer(m.view, m.suggest.active(), m.zoomed, m.onSessions))
	return ui.SplitLine(left, m.theme.Subtle.Render(scrollHint(more)), ui.FrameWidth(m.width))
}

func scrollHint(more int) string {
	if more <= 0 {
		return ""
	}
	return fmt.Sprintf("↓ %d more", more)
}
