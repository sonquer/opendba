package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"

	"github.com/sonquer/opendba/src/cli/internal/chats"
	"github.com/sonquer/opendba/src/cli/internal/config"
	"github.com/sonquer/opendba/src/cli/internal/driver"
	"github.com/sonquer/opendba/src/cli/internal/driver/mssql"
	"github.com/sonquer/opendba/src/cli/internal/driver/postgres"
	"github.com/sonquer/opendba/src/cli/internal/driver/sqlite"
	"github.com/sonquer/opendba/src/cli/internal/history"
	"github.com/sonquer/opendba/src/cli/internal/ui"
	"github.com/sonquer/opendba/src/cli/pkg/secretref"
	"github.com/sonquer/opendba/src/cli/pkg/sqldialect"
	"github.com/sonquer/opendba/src/cli/pkg/sqlguard"
)

const (
	ExitOK      = 0
	ExitFailure = 1
	ExitUsage   = 2
	ExitBlocked = 3
)

type Launcher func(Session, Workspace) error

type SetupOutcome struct {
	Connection config.Connection
	Saved      bool
	Warning    string
}

type Workspace interface {
	Profiles() (config.Profiles, error)
	Open(ctx context.Context, name string) (Session, func(), error)
	OpenDatabase(ctx context.Context, name, database string) (Session, func(), error)
	Remember(profile, database, schema string, schemas []string) error
	Remove(ctx context.Context, name string) error
	Setup() Setup
}

type App struct {
	Store    config.Store
	Registry *driver.Registry

	// Kept holds what belongs to the program rather than to one connection. An
	// App without one opens a handle per session, which is what a command that
	// opens one connection and then leaves wants.
	Kept     *Keep
	Secrets  *secretref.Store
	Dialects *sqldialect.Factory
	Stdout   io.Writer
	Stderr   io.Writer
	Version  string
	Launch   Launcher
	Wizard   func(Setup) (SetupOutcome, error)
	Prompt   func(prompt string) ([]byte, error)
}

type Session struct {
	// Version is which build of this program is running, for the account of
	// itself it leaves behind when it fails.
	Version string

	AI           Assistant
	Warning      string
	Capabilities driver.Capabilities
	Connection   config.Connection
	Settings     config.Settings
	Conn         driver.Conn
	Info         driver.ServerInfo
	Guard        sqlguard.Guard
	Theme        *ui.Theme

	// History is where the statements that have been run are kept, or nil when the
	// settings say not to keep them or the file could not be opened.
	History *history.Store

	// Chats is where conversations with the assistant are kept, or nil when the
	// settings say not to keep them or the file could not be opened.
	Chats *chats.Store

	// Dialect is what parses a statement into its parts.
	Dialect sqldialect.Dialect

	// Release closes this session's connection. It is the same closer open
	// returns, and calling it twice closes nothing twice, so an interface that can
	// disconnect and a caller that always cleans up do not have to agree on which
	// of them owns it. What is kept beside the program rather than beside the
	// connection — the history and the conversations — outlives it and is closed
	// by whoever built the App.
	Release func()
}

func Registry() *driver.Registry {
	registry := driver.NewRegistry()
	registry.Register(postgres.New())
	registry.Register(sqlite.New())
	registry.Register(mssql.New())
	return registry
}

func Secrets(paths config.Paths, passphrase secretref.Passphrase) *secretref.Store {
	return secretref.NewStore(
		secretref.NewKeyringBackend(),
		secretref.NewVaultBackend(paths.VaultFile(), passphrase),
		secretref.NewEnvBackend(),
		secretref.NewCommandBackend(),
	)
}

type options struct {
	json       bool
	connection string
	database   string
	schema     string
	limit      int

	// yes answers the question a statement that changes data would otherwise raise.
	yes bool
}

func (a App) Run(ctx context.Context, args []string) int {
	command, rest := split(args)
	switch command {
	case "help":
		a.usage()
		return ExitOK
	case "version":
		fmt.Fprintln(a.Stdout, a.Version)
		return ExitOK
	case "connections":
		return a.connections()
	}
	if !known(command) {
		fmt.Fprintf(a.Stderr, "unknown command %q\n\n", command)
		a.usage()
		return ExitUsage
	}

	if command == "tui" {
		configured, err := a.configured()
		if err != nil {
			fmt.Fprintln(a.Stderr, err)
			return ExitFailure
		}
		if !configured {
			return a.setup(ctx)
		}
	}

	opts, statement, err := a.parse(command, rest)
	if err != nil {
		if err == flag.ErrHelp {
			return ExitOK
		}
		fmt.Fprintln(a.Stderr, err)
		return ExitUsage
	}

	session, cleanup, err := a.open(ctx, opts)
	if err != nil {
		fmt.Fprintln(a.Stderr, err)
		return ExitFailure
	}
	defer cleanup()

	switch command {
	case "tui":
		return a.launch(session)
	case "inspect":
		return a.inspect(ctx, session, opts)
	case "schema":
		return a.schema(ctx, session, opts)
	case "indexes":
		return a.indexes(ctx, session, opts)
	case "query":
		return a.query(ctx, session, opts, statement)
	default:
		return ExitUsage
	}
}

func (a App) configured() (bool, error) {
	profiles, err := a.Store.LoadProfiles()
	if err != nil {
		return false, err
	}
	return !profiles.IsEmpty(), nil
}

func (a App) setup(ctx context.Context) int {
	if a.Wizard == nil {
		fmt.Fprintln(a.Stderr, "no connections are configured yet, and this build cannot set one up")
		return ExitFailure
	}
	outcome, err := a.Wizard(a.Setup())
	if err != nil {
		fmt.Fprintln(a.Stderr, err)
		return ExitFailure
	}
	if !outcome.Saved {
		fmt.Fprintln(a.Stdout, "nothing was saved")
		return ExitOK
	}
	session, cleanup, err := a.Open(ctx, outcome.Connection.Name)
	if err != nil {
		fmt.Fprintf(a.Stderr, "%s was saved but could not be opened: %v\n", outcome.Connection.Name, err)
		return ExitFailure
	}
	defer cleanup()
	session.Warning = outcome.Warning
	return a.launch(session)
}

func (a App) Setup() Setup {
	return Setup{Store: a.Store, Registry: a.Registry, Secrets: a.Secrets}
}

func (a App) Profiles() (config.Profiles, error) { return a.Store.LoadProfiles() }

func (a App) Open(ctx context.Context, name string) (Session, func(), error) {
	return a.open(ctx, options{connection: name})
}

func (a App) OpenDatabase(ctx context.Context, name, database string) (Session, func(), error) {
	return a.open(ctx, options{connection: name, database: database})
}

// Remember writes the database and schemas a session moved to back to the
// profile, so the next run opens where the last one left off. The profile is
// named by its identifier rather than by its name: a name can be changed
// between the read and the write, and a rename must not write the wrong row.
func (a App) Remember(profile, database, schema string, schemas []string) error {
	profiles, err := a.Store.LoadProfiles()
	if err != nil {
		return err
	}
	connection, ok := profiles.Find(profile)
	if !ok {
		return fmt.Errorf("no connection with id %q", profile)
	}
	if connection.Database == database && connection.DefaultSchema == schema &&
		slices.Equal(connection.Schemas, schemas) {
		return nil
	}
	connection.Database = database
	connection.DefaultSchema = schema
	connection.Schemas = schemas
	if err := profiles.Upsert(connection); err != nil {
		return err
	}
	return a.Store.SaveProfiles(profiles)
}

// appearance builds the theme a session is drawn with, which is the palette
// plus the shape of a bar, the one part of it a font can ruin.
func appearance(settings config.Settings) *ui.Theme {
	theme := ui.Default()
	theme.Bars(settings.Appearance.Bar)
	return theme
}

func (a App) Remove(ctx context.Context, name string) error {
	profiles, err := a.Store.LoadProfiles()
	if err != nil {
		return err
	}
	connection, ok := profiles.ByName(name)
	if !ok {
		return fmt.Errorf("no connection named %q", name)
	}
	if !profiles.Remove(connection.ID) {
		return fmt.Errorf("no connection named %q", name)
	}
	if err := a.Store.SaveProfiles(profiles); err != nil {
		return err
	}
	return a.Setup().Forget(ctx, connection)
}

func (a App) launch(session Session) int {
	if a.Launch == nil {
		fmt.Fprintln(a.Stderr, "this build has no interactive interface")
		return ExitFailure
	}
	if err := a.Launch(session, a); err != nil {
		fmt.Fprintln(a.Stderr, err)
		return ExitFailure
	}
	return ExitOK
}

func (a App) open(ctx context.Context, opts options) (Session, func(), error) {
	settings, err := a.Store.LoadSettings()
	if err != nil {
		return Session{}, nil, err
	}
	profiles, err := a.Store.LoadProfiles()
	if err != nil {
		return Session{}, nil, err
	}
	connection, err := pick(profiles, opts.connection)
	if err != nil {
		return Session{}, nil, err
	}
	connection = opts.apply(connection)
	target, err := a.Registry.Get(connection.Driver)
	if err != nil {
		return Session{}, nil, err
	}
	dialect, err := a.Dialects.Get(connection.Driver)
	if err != nil {
		return Session{}, nil, err
	}
	config, err := a.driverConfig(ctx, connection, settings, opts)
	if err != nil {
		return Session{}, nil, err
	}
	conn, err := target.Connect(ctx, config)
	if err != nil {
		return Session{}, nil, err
	}
	info, err := conn.Info(ctx)
	if err != nil {
		_ = conn.Close()
		return Session{}, nil, err
	}
	recorder, warning := a.history(settings)
	conversations, trouble := a.chats(settings)
	warning = ui.Dotted(warning, trouble)
	var once sync.Once
	release := func() { once.Do(func() { _ = conn.Close() }) }
	session := Session{
		Version:      a.Version,
		Warning:      warning,
		AI:           NewAssistant(ctx, a.Store.Paths, settings, a.Secrets),
		Capabilities: target.Capabilities(),
		Connection:   connection,
		Settings:     settings,
		Conn:         conn,
		Info:         info,
		Guard:        sqlguard.New(dialect),
		Dialect:      dialect,
		History:      recorder,
		Chats:        conversations,
		Theme:        appearance(settings),
		Release:      release,
	}
	return session, release, nil
}

// chats opens where conversations with the assistant are kept.
func (a App) chats(settings config.Settings) (*chats.Store, string) {
	if !settings.Chats.Enabled {
		return nil, ""
	}
	return a.Kept.Chats(a.Store.Paths.ChatsFile(), settings.Chats)
}

// history opens the store the statements you have run are kept in.
func (a App) history(settings config.Settings) (*history.Store, string) {
	if !settings.History.Enabled {
		return nil, ""
	}
	return a.Kept.History(a.Store.Paths.HistoryFile(), settings.History)
}

func pick(profiles config.Profiles, name string) (config.Connection, error) {
	if profiles.IsEmpty() {
		return config.Connection{}, fmt.Errorf("no connections are configured yet; run opendba to set one up")
	}
	if name == "" {
		return profiles.Connections[0], nil
	}
	connection, ok := profiles.ByName(name)
	if !ok {
		return config.Connection{}, fmt.Errorf("no connection named %q", name)
	}
	return connection, nil
}

// apply overrides the profile for one session, which is how the interface
// moves between the databases and schemas of a server without editing anything.
func (o options) apply(connection config.Connection) config.Connection {
	if o.database != "" {
		connection.Database = o.database
	}
	if o.schema != "" {
		connection.DefaultSchema = o.schema
	}
	return connection
}

func (a App) driverConfig(ctx context.Context, connection config.Connection, settings config.Settings, opts options) (driver.Config, error) {
	password, err := a.password(ctx, connection)
	if err != nil {
		return driver.Config{}, err
	}
	limit := settings.Safety.RowLimit
	if opts.limit > 0 {
		limit = opts.limit
	}
	return driver.Config{
		Host:        connection.Host,
		Port:        connection.Port,
		Database:    connection.Database,
		User:        connection.User,
		Password:    password,
		SSLMode:     connection.SSLMode,
		File:        connection.File,
		Options:     connection.Options,
		Mode:        Mode(connection.Mode),
		Application: connection.Name,
		Timeouts:    Timeouts(settings.Safety),
		RowLimit:    limit,
	}, nil
}

func (a App) password(ctx context.Context, connection config.Connection) ([]byte, error) {
	if strings.TrimSpace(connection.Secret) == "" {
		return nil, nil
	}
	ref, err := secretref.Parse(connection.Secret)
	if err != nil {
		return nil, err
	}
	if ref.Scheme == secretref.SchemePrompt {
		if a.Prompt == nil {
			return nil, fmt.Errorf("connection %s asks for its password, which needs a terminal", connection.Name)
		}
		return a.Prompt(fmt.Sprintf("password for %s: ", connection.Name))
	}
	if ref.Scheme == secretref.SchemePgpass {
		return nil, nil
	}
	return a.Secrets.Get(ctx, ref)
}

func Mode(mode config.AccessMode) sqlguard.Mode {
	if mode == config.ReadWrite {
		return sqlguard.ModeReadWrite
	}
	return sqlguard.ModeReadOnly
}

func (a App) connections() int {
	profiles, err := a.Store.LoadProfiles()
	if err != nil {
		fmt.Fprintln(a.Stderr, err)
		return ExitFailure
	}
	if profiles.IsEmpty() {
		fmt.Fprintln(a.Stdout, "no connections are configured yet")
		return ExitOK
	}
	theme := ui.Default()
	for _, connection := range profiles.Connections {
		environment := ui.EnvColor(connection.Color)
		fmt.Fprintf(a.Stdout, "%s %s\n", theme.Env(environment), ui.Dotted(
			connection.Name, connection.Driver, connection.Mode.Label(), Target(connection)))
	}
	return ExitOK
}

// Application is what a connection tells the server it is.
func Application(connection config.Connection) string {
	return driver.Config{Application: connection.Name}.ApplicationName()
}

func Target(connection config.Connection) string {
	if connection.File != "" {
		return connection.File
	}
	host := connection.Host
	if connection.Port > 0 {
		host = fmt.Sprintf("%s:%d", host, connection.Port)
	}
	if connection.Database == "" {
		return host
	}
	return host + "/" + connection.Database
}

var commands = map[string]bool{
	"tui":         true,
	"inspect":     true,
	"schema":      true,
	"indexes":     true,
	"query":       true,
	"connections": true,
	"version":     true,
	"help":        true,
}

func known(command string) bool { return commands[command] }

func split(args []string) (string, []string) {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return "tui", args
	}
	return args[0], args[1:]
}

func (a App) parse(command string, args []string) (options, string, error) {
	opts := options{}
	set := flag.NewFlagSet("opendba "+command, flag.ContinueOnError)
	set.SetOutput(a.Stderr)
	set.BoolVar(&opts.json, "json", false, "print a machine readable report")
	set.StringVar(&opts.connection, "connection", "", "the connection to use")
	set.StringVar(&opts.schema, "schema", "", "limit the report to one schema")
	set.IntVar(&opts.limit, "limit", 0, "maximum number of rows to read")
	set.BoolVar(&opts.yes, "yes", false, "run a statement that changes data without being asked")
	if err := set.Parse(args); err != nil {
		return options{}, "", err
	}
	statement := strings.Join(set.Args(), " ")
	if command == "query" && strings.TrimSpace(statement) == "" {
		return options{}, "", fmt.Errorf("opendba query needs a statement")
	}
	return opts, statement, nil
}

func (a App) usage() {
	fmt.Fprintf(a.Stdout, `opendba, a terminal workbench for databases

usage: opendba [command] [flags] [statement]

commands:
  tui           open the interface, the default when no command is given
  inspect       report the health of the connected database
  schema        list the tables of a schema
  indexes       list the indexes of a schema
  query         run one read-only statement
  connections   list the configured connections
  version       print the version
  help          show this message

flags:
  --json                 print a machine readable report
  --connection <name>    the connection to use, the first one by default
  --schema <name>        limit the report to one schema
  --limit <rows>         maximum number of rows to read

Everything is read only unless the connection says otherwise, and a statement
that changes data is refused before it reaches the server.
`)
}
