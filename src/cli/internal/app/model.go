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
	"github.com/sonquer/tui4db/src/cli/internal/driver"
	"github.com/sonquer/tui4db/src/cli/internal/ui"
	"github.com/sonquer/tui4db/src/cli/pkg/sqlguard"
)

type view string

const (
	viewDashboard view = "dashboard"
	viewInspect   view = "health"
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
	suggest completion
	fields  map[string][]driver.Column
	keys    keymap
	help    help.Model
	offset  int

	findings []driver.Finding
	tables   []driver.Table
	indexes  []driver.Index

	editor       textarea.Model
	results      results
	resultsFocus bool
	spinner      spinner.Model
}

func NewModel(session cli.Session, workspace cli.Workspace) Model {
	theme := session.Theme
	loader := spinner.New()
	loader.Spinner = spinner.Dot
	loader.Style = lipgloss.NewStyle().Foreground(theme.P.Accent)
	hints := help.New()
	hints.Styles = helpStyles(theme)
	hints.ShortSeparator = " · "
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
		fields:    map[string][]driver.Column{},
	}
}

func helpStyles(theme *ui.Theme) help.Styles {
	return help.Styles{
		Ellipsis:       theme.Subtle,
		ShortKey:       theme.KeyCap,
		ShortDesc:      theme.Muted,
		ShortSeparator: theme.Subtle,
		FullKey:        theme.KeyCap,
		FullDesc:       theme.Muted,
		FullSeparator:  theme.Subtle,
	}
}

func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.load(), m.spinner.Tick}
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
	schema := m.session.Connection.Schema()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		findings, err := conn.Health(ctx)
		if err != nil {
			return loadedMsg{err: err}
		}
		tables, err := conn.Tables(ctx, schema)
		if err != nil {
			return loadedMsg{err: err}
		}
		indexes, err := conn.Indexes(ctx, schema)
		if err != nil {
			return loadedMsg{err: err}
		}
		return loadedMsg{findings: findings, tables: tables, indexes: indexes}
	}
}

func (m Model) run(statement string) tea.Cmd {
	conn := m.session.Conn
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()

		result, err := conn.Query(ctx, statement)
		if err != nil {
			return queriedMsg{err: err}
		}
		defer func() { _ = result.Close() }()

		message := queriedMsg{columns: result.Columns()}
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
		m.editor.SetWidth(ui.TextWidth(m.width))
		m.editor.SetHeight(editorRows(m.height))
		m.results = m.results.resize(ui.TextWidth(m.width), resultsRows(m.height))
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
		return m, nil
	case queriedMsg:
		m.results = newResults(m.theme, msg, ui.TextWidth(m.width), resultsRows(m.height))
		m.resultsFocus = false
		return m, nil
	case toastMsg:
		m.expire(msg)
		return m, nil
	case profilesMsg:
		m.list = m.list.withProfiles(msg)
		return m, nil
	case columnsMsg:
		m.fields[msg.table] = msg.columns
		return m.resuggest()
	case catalogMsg:
		m.catalog = m.catalog.withCatalog(msg, m.session.Connection.Schema())
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

func (m Model) key(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.wizard != nil {
		return m.toWizard(msg)
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
	switch {
	case key.Matches(msg, m.keys.Catalog):
		return m.browseCatalog()
	case key.Matches(msg, m.keys.Palette):
		return m.openPalette()
	case key.Matches(msg, m.keys.Connections):
		return m.browse()
	case key.Matches(msg, m.keys.Quit):
		m.quitting = true
		return m, tea.Quit
	case key.Matches(msg, m.keys.Back):
		return m.show(viewDashboard)
	case key.Matches(msg, m.keys.Health):
		return m.show(viewInspect)
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

func (m Model) show(target view) (tea.Model, tea.Cmd) {
	m.view = target
	m.offset = 0
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
	case key.Matches(msg, m.keys.Back), key.Matches(msg, m.keys.Palette):
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

func (m Model) queryKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.suggest.active() {
		switch {
		case key.Matches(msg, m.keys.Accept):
			return m.accept()
		case key.Matches(msg, m.keys.Up):
			m.suggest = m.suggest.move(-1)
			return m, nil
		case key.Matches(msg, m.keys.Down):
			m.suggest = m.suggest.move(1)
			return m, nil
		case key.Matches(msg, m.keys.Back):
			m.suggest.items = nil
			return m, nil
		}
	}
	switch {
	case key.Matches(msg, m.keys.Leave):
		m.quitting = true
		return m, tea.Quit
	case key.Matches(msg, m.keys.Back):
		return m.show(viewDashboard)
	case key.Matches(msg, m.keys.Palette):
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
	case key.Matches(msg, m.keys.Focus):
		return m.toggleFocus()
	}
	if m.resultsFocus {
		updated, cmd := m.results.update(msg)
		m.results = updated
		return m, cmd
	}
	updated, cmd := m.editor.Update(msg)
	m.editor = updated
	typed, suggesting := m.resuggest()
	return typed, tea.Batch(cmd, suggesting)
}

func (m Model) toggleFocus() (tea.Model, tea.Cmd) {
	if !m.results.present || m.results.failure != "" {
		return m, nil
	}
	m.resultsFocus = !m.resultsFocus
	if m.resultsFocus {
		m.editor.Blur()
		return m, nil
	}
	return m, m.editor.Focus()
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
	screen := m.theme.Chrome(m.width, m.height, m.header(), body, m.footer(more))
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
		ui.Dotted(m.session.Connection.Name, m.database()),
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
	switch m.view {
	case viewInspect:
		return m.theme.FindingTable(m.findings)
	case viewSchema:
		return m.schemaBody("tables", m.theme.TableList(m.tables))
	case viewIndexes:
		return m.schemaBody("indexes", m.theme.IndexList(m.indexes))
	case viewQuery:
		return m.queryBody()
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
	left := m.help.View(m.keys.footer(m.view, m.suggest.active()))
	if m.view == viewSwitch && m.list.removing() {
		left = m.theme.Muted.Render(ui.Dotted(
			ui.Keystroke("enter")+" removes it", ui.Keystroke("esc")+" cancels"))
	}
	return ui.SplitLine(left, m.theme.Subtle.Render(scrollHint(more)), ui.FrameWidth(m.width))
}

func scrollHint(more int) string {
	if more <= 0 {
		return ""
	}
	return fmt.Sprintf("↓ %d more", more)
}
