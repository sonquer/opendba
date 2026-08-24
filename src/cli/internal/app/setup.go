package app

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/sonquer/opendba/src/cli/internal/cli"
	"github.com/sonquer/opendba/src/cli/internal/config"
	"github.com/sonquer/opendba/src/cli/internal/driver"
	"github.com/sonquer/opendba/src/cli/internal/ui"
)

type stage int

const (
	stageDriver stage = iota
	stageDetails
	stageTesting
)

type SetupDone struct {
	Connection config.Connection
	Saved      bool
	Info       driver.ServerInfo
}

func (d SetupDone) Warning() string {
	if d.Saved && d.Info.CanWrite && d.Connection.Mode == config.ReadOnly {
		return "the role can write, so read only is enforced by opendba alone"
	}
	return ""
}

type testedMsg struct {
	info  driver.ServerInfo
	saved bool
	err   error
}

type answers struct {
	driver   string
	file     string
	imported string
	host     string
	port     string
	user     string
	password string
	database string
	sslmode  string
	readOnly bool
	name     string
	color    string

	// existing marks a profile being edited rather than made, which is the
	// difference between a password field that has to be filled and one that means
	// keep what is stored.
	existing bool
}

const (
	modeReadOnly  = "READ ONLY"
	modeReadWrite = "READ / WRITE"
)

type SetupModel struct {
	setup      cli.Setup
	theme      *ui.Theme
	form       form
	driver     string
	stage      stage
	connection config.Connection

	// editing is the profile this screen opened, empty when it is making a new one.
	editing config.Connection
	failure string
	toaster
	quitting bool
	width    int
	height   int
}

func NewSetupModel(setup cli.Setup) SetupModel {
	model := SetupModel{
		setup:  setup,
		theme:  ui.Default(),
		width:  96,
		height: 30,
	}
	if entries := setup.Registry.Entries(); len(entries) > 0 {
		model.driver = entries[0].Name
	}
	model.form, _ = newForm(driverFields(setup, model.theme)...)
	return model
}

// EditSetupModel opens an existing profile in the form that made it.
func EditSetupModel(setup cli.Setup, connection config.Connection) SetupModel {
	model := SetupModel{
		setup:   setup,
		theme:   ui.Default(),
		width:   96,
		height:  30,
		driver:  connection.Driver,
		stage:   stageDetails,
		editing: connection,
	}
	values := answersFrom(connection)
	model.form, _ = newForm(detailsFields(&values, model.theme)...)
	return model
}

// answersFrom reads a saved profile back into the form.
func answersFrom(connection config.Connection) answers {
	values := defaults()
	values.existing = true
	values.driver = connection.Driver
	values.name = connection.Name
	values.file = connection.File
	values.host = connection.Host
	values.user = connection.User
	values.database = connection.Database
	values.color = connection.Color
	values.readOnly = connection.Mode != config.ReadWrite
	if connection.Port > 0 {
		values.port = strconv.Itoa(connection.Port)
	}
	if connection.SSLMode != "" {
		values.sslmode = connection.SSLMode
	}
	return values
}

func defaults() answers {
	return answers{
		host:     "localhost",
		port:     "5432",
		sslmode:  "prefer",
		readOnly: true,
		color:    string(ui.EnvGreen),
	}
}

func (m SetupModel) snapshot() answers {
	values := defaults()
	values.driver = m.driver
	values.name = m.form.valueOr("name", values.name)
	values.readOnly = m.form.enabledOr("access", values.readOnly)
	values.color = m.form.valueOr("color", values.color)
	values.file = m.form.valueOr("file", values.file)
	values.host = m.form.valueOr("host", values.host)
	values.port = m.form.valueOr("port", values.port)
	values.user = m.form.valueOr("user", values.user)
	values.database = m.form.valueOr("database", values.database)
	values.sslmode = m.form.valueOr("ssl", values.sslmode)
	values.imported = m.form.valueOr("import", values.imported)
	values.existing = m.editing.ID != ""
	if m.form.has("password") {
		values.password = string(m.form.secret("password"))
	}
	return values
}

func (m SetupModel) tested(msg testedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.failure = msg.err.Error()
		m.stage = stageDetails
		return m, nil
	}
	m.failure = ""
	done := SetupDone{Connection: m.connection, Saved: msg.saved, Info: msg.info}
	return m, func() tea.Msg { return done }
}

func driverFields(setup cli.Setup, theme *ui.Theme) []field {
	entries := setup.Registry.Entries()
	titles := make([]string, 0, len(entries))
	for _, entry := range entries {
		titles = append(titles, entry.Title)
	}
	return []field{
		choiceField("driver", "database", titles, titles[0], "← → picks a driver, only what is implemented is listed"),
		actionField("next", "continue", "set up the connection"),
	}
}

func detailsFields(values *answers, theme *ui.Theme) []field {
	fields := []field{
		textField(theme, "name", "name", values.name, "shown wherever this connection appears").require(),
	}
	fields = append(fields, connectionFields(values, theme)...)
	fields = append(fields,
		toggleField("access", "access", []string{modeReadOnly, modeReadWrite}, values.readOnly,
			"read only refuses every statement that changes data, before it is sent"),
		choiceField("color", "environment", colorNames(), values.color, "← → picks the colour of the bar on top of every screen"),
		actionField("test", "test", "connect without saving anything"),
		actionField("save", "save", "test, then keep the connection"),
	)
	return fields
}

func connectionFields(values *answers, theme *ui.Theme) []field {
	if values.driver == "sqlite" {
		return []field{
			textField(theme, "file", "file", values.file, "a path, created only when the access mode allows writing").require(),
		}
	}
	return []field{
		textField(theme, "host", "host", values.host, "the server to connect to").require(),
		textField(theme, "port", "port", values.port, "5432 unless the server was moved").require().checked(port),
		textField(theme, "user", "user", values.user, "the role opendba connects as"),
		secretField(theme, "password", "password", values.password, passwordHint(values.existing)),
		textField(theme, "database", "database", values.database, "left empty the server picks its default, usually the user name"),
		choiceField("ssl", "ssl", []string{"prefer", "require", "verify-ca", "verify-full", "disable"}, values.sslmode, "← → picks how the connection is encrypted"),
		textField(theme, "import", "paste a url", "", "postgres://user:password@host:5432/app fills in everything above"),
	}
}

func colorNames() []string {
	names := make([]string, 0, len(ui.EnvColors))
	for _, color := range ui.EnvColors {
		names = append(names, string(color))
	}
	return names
}

func required(message string) func(string) error {
	return func(value string) error {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s", message)
		}
		return nil
	}
}

func port(value string) error {
	number, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || number < 1 || number > 65535 {
		return fmt.Errorf("a port is a number between 1 and 65535")
	}
	return nil
}

func importable(values *answers) func(string) error {
	return func(raw string) error {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return nil
		}
		remainder, password, err := cli.SplitDSN(raw)
		if err != nil {
			return err
		}
		parsed, err := cli.ParseDSN(remainder)
		if err != nil {
			return err
		}
		values.host = parsed.Host
		values.port = strconv.Itoa(parsed.Port)
		values.user = parsed.User
		values.database = parsed.Database
		values.sslmode = parsed.SSLMode
		values.password = string(password)
		return nil
	}
}

func (m SetupModel) Init() tea.Cmd { return m.form.blink() }

func (m SetupModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch typed := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = typed.Width, typed.Height
		return m, nil
	case testedMsg:
		return m.tested(typed)
	case tea.KeyPressMsg:
		return m.key(typed)
	case tea.PasteMsg:
		return m.pasted(typed)
	case toastMsg:
		m.expire(typed)
		return m, nil
	}
	return m, nil
}

func (m SetupModel) pasted(msg tea.PasteMsg) (tea.Model, tea.Cmd) {
	if m.stage != stageDriver && m.stage != stageDetails {
		return m, nil
	}
	target := m.form.current()
	updated, count, cmd := m.form.paste(msg)
	m.form = updated
	if count <= 0 {
		return m, tea.Batch(cmd, m.notify("nothing was pasted here"))
	}
	return m, tea.Batch(cmd, m.notify(fmt.Sprintf("pasted %s into %s", characters(count), target.label)))
}

func characters(count int) string {
	if count == 1 {
		return "1 character"
	}
	return fmt.Sprintf("%d characters", count)
}

func (m SetupModel) key(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" {
		m.quitting = true
		return m, tea.Quit
	}
	if m.stage == stageTesting {
		return m, nil
	}
	if msg.String() == "esc" && m.stage == stageDetails {
		return m.backToDriver()
	}
	updated, action, cmd := m.form.update(msg)
	m.form = updated
	if action == "" {
		return m, cmd
	}
	return m.act(action, cmd)
}

func (m SetupModel) backToDriver() (tea.Model, tea.Cmd) {
	m.stage = stageDriver
	m.failure = ""
	updated, cmd := newForm(driverFields(m.setup, m.theme)...)
	m.form = updated
	return m, cmd
}

func (m SetupModel) act(action string, cmd tea.Cmd) (tea.Model, tea.Cmd) {
	switch action {
	case "next":
		m.driver = m.driverName()
		m.stage = stageDetails
		m.failure = ""
		values := m.snapshot()
		updated, focus := newForm(detailsFields(&values, m.theme)...)
		m.form = updated
		return m, tea.Batch(cmd, focus)
	case "test", "save":
		return m.start(action == "save", cmd)
	}
	return m, cmd
}

func (m SetupModel) driverName() string {
	title := m.form.value("driver")
	for _, entry := range m.setup.Registry.Entries() {
		if entry.Title == title {
			return entry.Name
		}
	}
	return title
}

func (m SetupModel) start(save bool, cmd tea.Cmd) (tea.Model, tea.Cmd) {
	values := m.snapshot()
	if values.imported != "" {
		return m.importURL(values, cmd)
	}
	if err := m.form.validate(); err != nil {
		m.failure = err.Error()
		return m, cmd
	}
	m.failure = ""
	m.stage = stageTesting
	connection, password := connectionFrom(m.editing, values, m.setup.NewID())
	m.connection = connection
	if len(password) == 0 {
		password = m.setup.Password(context.Background(), m.editing)
	}
	return m, tea.Batch(cmd, m.test(connection, password, save))
}

func (m SetupModel) importURL(values answers, cmd tea.Cmd) (tea.Model, tea.Cmd) {
	if err := importable(&values)(values.imported); err != nil {
		m.failure = err.Error()
		return m, cmd
	}
	values.imported = ""
	m.failure = ""
	updated, focus := newForm(detailsFields(&values, m.theme)...)
	m.form = updated
	return m, tea.Batch(cmd, focus, m.notify("filled every field from the connection string"))
}

// connectionFrom writes the answers onto a connection.
func passwordHint(existing bool) string {
	if existing {
		return "left empty the stored password is kept"
	}
	return "kept in your keychain, never in the profile"
}

func connectionFrom(base config.Connection, values answers, id string) (config.Connection, []byte) {
	connection := base
	if connection.ID == "" {
		connection.ID = id
	}
	connection.Name = strings.TrimSpace(values.name)
	connection.Driver = values.driver
	connection.Mode = config.ReadOnly
	connection.Color = values.color
	if !values.readOnly {
		connection.Mode = config.ReadWrite
	}
	if connection.Driver == "sqlite" {
		connection.File = strings.TrimSpace(values.file)
		return connection, nil
	}
	connection.Host = strings.TrimSpace(values.host)
	connection.Port, _ = strconv.Atoi(strings.TrimSpace(values.port))
	connection.User = strings.TrimSpace(values.user)
	connection.Database = strings.TrimSpace(values.database)
	connection.SSLMode = values.sslmode
	return connection, []byte(values.password)
}

func (m SetupModel) test(connection config.Connection, password []byte, save bool) tea.Cmd {
	setup := m.setup
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		info, err := setup.Test(ctx, connection, password)
		if err != nil {
			return testedMsg{err: err}
		}
		if !save {
			return testedMsg{info: info}
		}
		if err := setup.Save(connection, password); err != nil {
			return testedMsg{err: err}
		}
		return testedMsg{info: info, saved: true}
	}
}

func (m SetupModel) View() tea.View {
	var v tea.View
	v.AltScreen = true
	v.BackgroundColor = m.theme.P.Bg
	v.ForegroundColor = m.theme.P.Fg
	v.WindowTitle = "opendba setup"
	v.SetContent(m.theme.Base.Render(m.content()))
	return v
}

const (
	minSetupWidth = 48
	maxSetupWidth = 78
)

func (m SetupModel) content() string {
	return m.theme.Chrome(ui.Frame{
		Width:  m.width,
		Height: m.height,
		Env:    ui.EnvColor(m.form.value("color")),
		Header: m.headline(),
		Body:   m.body(setupWidth(m.width)),
		Footer: m.statusLine(),
	})
}

func (m SetupModel) headline() string {
	title := "opendba setup"
	if m.editing.ID != "" {
		title = "configuration"
	}
	line := m.theme.Prompt.Render("→ ") + m.theme.Muted.Render("~ ") + m.theme.Title.Render(title)
	if m.editing.ID != "" {
		line += m.theme.Separator.Render(" › ") + m.theme.Muted.Render(m.editing.Name)
	}
	return line
}

func setupWidth(terminal int) int {
	width := terminal - 6
	switch {
	case width < minSetupWidth:
		return minSetupWidth
	case width > maxSetupWidth:
		return maxSetupWidth
	default:
		return width
	}
}

func (m SetupModel) statusLine() string {
	if toast := m.render(m.theme); toast != "" {
		return toast
	}
	return m.theme.Muted.Render(m.status())
}

func (m SetupModel) status() string {
	leaves := ui.Keystroke("ctrl+c") + " leaves"
	next := ui.Keystroke("enter") + " next"
	switch m.stage {
	case stageDriver:
		return ui.Dotted("← → chooses a driver", next, leaves)
	case stageTesting:
		return ui.Dotted("connecting", leaves)
	default:
		return ui.Dotted("↑↓ moves between fields", next, leaves)
	}
}

func (m SetupModel) body(width int) string {
	switch m.stage {
	case stageTesting:
		return m.theme.Muted.Render("reaching the server…")
	default:
		sections := []string{m.form.view(m.theme, width)}
		if m.failure != "" {
			sections = append(sections, m.theme.Error.Render("✗ "+m.failure))
		}
		return strings.Join(sections, "\n\n")
	}
}

func RunSetup(setup cli.Setup) (cli.SetupOutcome, error) { return runSetup(setup) }

type setupProgram struct {
	wizard  SetupModel
	outcome cli.SetupOutcome
}

func (p setupProgram) Init() tea.Cmd { return p.wizard.Init() }

func (p setupProgram) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if done, ok := msg.(SetupDone); ok {
		p.outcome = cli.SetupOutcome{Connection: done.Connection, Saved: done.Saved, Warning: done.Warning()}
		return p, tea.Quit
	}
	updated, cmd := p.wizard.Update(msg)
	if wizard, ok := updated.(SetupModel); ok {
		p.wizard = wizard
	}
	return p, cmd
}

func (p setupProgram) View() tea.View { return p.wizard.View() }

func runSetup(setup cli.Setup, options ...tea.ProgramOption) (cli.SetupOutcome, error) {
	final, err := tea.NewProgram(setupProgram{wizard: NewSetupModel(setup)}, options...).Run()
	if err != nil {
		return cli.SetupOutcome{}, fmt.Errorf("run the setup: %w", err)
	}
	if program, ok := final.(setupProgram); ok {
		return program.outcome, nil
	}
	return cli.SetupOutcome{}, nil
}
