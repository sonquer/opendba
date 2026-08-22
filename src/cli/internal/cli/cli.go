package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/sonquer/tui4db/src/cli/internal/config"
	"github.com/sonquer/tui4db/src/cli/internal/driver"
	"github.com/sonquer/tui4db/src/cli/internal/driver/postgres"
	"github.com/sonquer/tui4db/src/cli/internal/driver/sqlite"
	"github.com/sonquer/tui4db/src/cli/internal/ui"
	"github.com/sonquer/tui4db/src/cli/pkg/secretref"
	"github.com/sonquer/tui4db/src/cli/pkg/sqldialect"
	"github.com/sonquer/tui4db/src/cli/pkg/sqlguard"
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
	Remember(name, database, schema string) error
	Remove(ctx context.Context, name string) error
	Setup() Setup
}

type App struct {
	Store    config.Store
	Registry *driver.Registry
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
	Warning      string
	Capabilities driver.Capabilities
	Connection   config.Connection
	Settings     config.Settings
	Conn         driver.Conn
	Info         driver.ServerInfo
	Guard        sqlguard.Guard
	Theme        *ui.Theme
}

func Registry() *driver.Registry {
	registry := driver.NewRegistry()
	registry.Register(postgres.New())
	registry.Register(sqlite.New())
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

// Remember writes the database and schema a session moved to back to the
// profile, so the next run opens where the last one left off.
func (a App) Remember(name, database, schema string) error {
	profiles, err := a.Store.LoadProfiles()
	if err != nil {
		return err
	}
	connection, ok := profiles.ByName(name)
	if !ok {
		return fmt.Errorf("no connection named %q", name)
	}
	if connection.Database == database && connection.DefaultSchema == schema {
		return nil
	}
	connection.Database = database
	connection.DefaultSchema = schema
	if err := profiles.Upsert(connection); err != nil {
		return err
	}
	return a.Store.SaveProfiles(profiles)
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
	session := Session{
		Capabilities: target.Capabilities(),
		Connection:   connection,
		Settings:     settings,
		Conn:         conn,
		Info:         info,
		Guard:        sqlguard.New(dialect),
		Theme:        ui.Default(),
	}
	return session, func() { _ = conn.Close() }, nil
}

func pick(profiles config.Profiles, name string) (config.Connection, error) {
	if profiles.IsEmpty() {
		return config.Connection{}, fmt.Errorf("no connections are configured yet; run tui4db to set one up")
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
	set := flag.NewFlagSet("tui4db "+command, flag.ContinueOnError)
	set.SetOutput(a.Stderr)
	set.BoolVar(&opts.json, "json", false, "print a machine readable report")
	set.StringVar(&opts.connection, "connection", "", "the connection to use")
	set.StringVar(&opts.schema, "schema", "", "limit the report to one schema")
	set.IntVar(&opts.limit, "limit", 0, "maximum number of rows to read")
	if err := set.Parse(args); err != nil {
		return options{}, "", err
	}
	statement := strings.Join(set.Args(), " ")
	if command == "query" && strings.TrimSpace(statement) == "" {
		return options{}, "", fmt.Errorf("tui4db query needs a statement")
	}
	return opts, statement, nil
}

func (a App) usage() {
	fmt.Fprintf(a.Stdout, `tui4db, a terminal workbench for databases

usage: tui4db [command] [flags] [statement]

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
