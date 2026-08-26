// Package mssql connects opendba to Microsoft SQL Server.
//
// Two things about this server differ from PostgreSQL and are visible to a
// person using it. There is no session setting that makes a connection read
// only, and there is no statement timeout the server enforces, so a read only
// profile is kept by refusing statements before they are sent and by throwing
// away the transaction they would have run in, and a statement deadline is the
// client's alone.
package mssql

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	_ "github.com/microsoft/go-mssqldb"

	"github.com/sonquer/opendba/src/cli/internal/driver"
)

const Name = "mssql"

const DefaultPort = 1433

// Scheme is what a SQL Server connection string starts with, and what tells a
// pasted one apart from a PostgreSQL one.
const Scheme = "sqlserver"

type Driver struct{}

func New() Driver { return Driver{} }

func (Driver) Name() string { return Name }

func (Driver) Title() string { return "SQL Server" }

// Capabilities reports no read only session because SQL Server has none: the
// driver rejects a read only transaction, and no session setting refuses a
// write. Saying otherwise would be a comfortable lie.
func (Driver) Capabilities() driver.Capabilities {
	return driver.Capabilities{
		Explain:     true,
		Relations:   true,
		Health:      true,
		IndexStats:  true,
		Cancel:      true,
		Sessions:    true,
		DefaultPort: DefaultPort,
	}
}

func (d Driver) Connect(ctx context.Context, config driver.Config) (driver.Conn, error) {
	database, err := sql.Open(Scheme, credentialled(config))
	if err != nil {
		return nil, fmt.Errorf("read the connection settings: %w", err)
	}
	return connected(ctx, database, config)
}

// connected settles an open database into a session opendba can use: sized,
// answering, and pinned to what this profile agreed to.
func connected(ctx context.Context, database *sql.DB, config driver.Config) (driver.Conn, error) {
	database.SetMaxOpenConns(maxConns)
	if idle := config.Timeouts.Idle; idle > 0 {
		database.SetConnMaxIdleTime(idle)
	}
	if err := database.PingContext(ctx); err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("connect to %s: %w", config.Target(), err)
	}
	conn := &connection{db: database, config: config}
	if err := conn.pin(ctx); err != nil {
		_ = database.Close()
		return nil, err
	}
	return conn, nil
}

// maxConns is how many connections one session opens. One of them is held by
// the screen that is drawing, and the others answer the screens that are not.
const maxConns = 3

// DSN is the connection string without the password, which is what may be shown
// and what may be written down.
func DSN(config driver.Config) string {
	host, instance := hostInstance(config)
	target := &url.URL{Scheme: Scheme, Host: host}
	if instance != "" {
		target.Path = "/" + instance
	}
	if config.User != "" {
		target.User = url.User(config.User)
	}
	query := url.Values{}
	if config.Database != "" {
		query.Set("database", config.Database)
	}
	query.Set("app name", config.ApplicationName())
	for name, value := range encryption(config) {
		query.Set(name, value)
	}
	if timeout := config.Timeouts.Connect; timeout > 0 {
		seconds := strconv.Itoa(int(timeout.Round(time.Second).Seconds()))
		query.Set("connection timeout", seconds)
		query.Set("dial timeout", seconds)
	}
	for name, values := range options(config) {
		query[name] = values
	}
	target.RawQuery = query.Encode()
	return target.String()
}

// credentialled is the connection string as the server is given it, which is the
// only place the password is written.
func credentialled(config driver.Config) string {
	if len(config.Password) == 0 {
		return DSN(config)
	}
	target, err := url.Parse(DSN(config))
	if err != nil {
		return DSN(config)
	}
	target.User = url.UserPassword(config.User, string(config.Password))
	return target.String()
}

// hostInstance separates a named instance from the host it runs on, which is
// how SQL Server addresses more than one server on one machine. A named
// instance is found through the browser service rather than on a port, so a
// port is only sent when no instance was named.
func hostInstance(config driver.Config) (string, string) {
	host := config.Host
	if host == "" {
		host = "localhost"
	}
	instance := ""
	if slash := strings.IndexAny(host, `\/`); slash >= 0 {
		host, instance = host[:slash], host[slash+1:]
	}
	if instance != "" {
		return host, instance
	}
	port := config.Port
	if port == 0 {
		port = DefaultPort
	}
	return host + ":" + strconv.Itoa(port), ""
}

// encryption turns the way PostgreSQL words a TLS setting, which is what the
// profile stores, into the way SQL Server words the same one.
func encryption(config driver.Config) map[string]string {
	switch config.SSLMode {
	case "disable":
		return map[string]string{"encrypt": "disable"}
	case "require":
		return map[string]string{"encrypt": "true", "trustservercertificate": "true"}
	case "verify-ca":
		return map[string]string{"encrypt": "true", "trustservercertificate": "false"}
	case "verify-full":
		host, _ := hostInstance(config)
		name, _, found := strings.Cut(host, ":")
		if !found {
			name = host
		}
		return map[string]string{
			"encrypt":                "true",
			"trustservercertificate": "false",
			"hostnameincertificate":  name,
		}
	default:
		return map[string]string{"encrypt": "false"}
	}
}

// options are the extra connection settings a profile carries verbatim, which
// is how anything this program has no field for is reached.
func options(config driver.Config) url.Values {
	parsed, err := url.ParseQuery(strings.TrimSpace(config.Options))
	if err != nil {
		return nil
	}
	return parsed
}

// Redact writes a connection string with nothing secret left in it.
func Redact(dsn string) string {
	parsed, err := url.Parse(dsn)
	if err != nil {
		return Scheme + "://redacted"
	}
	if parsed.User != nil {
		if _, hasPassword := parsed.User.Password(); hasPassword {
			parsed.User = url.UserPassword(parsed.User.Username(), "xxxxx")
		}
	}
	query := parsed.Query()
	if query.Has("password") {
		query.Set("password", "xxxxx")
	}
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

// SessionStatements pin what this connection agrees to wait for. SQL Server has
// no statement timeout of its own, so only the lock wait is set here and the
// deadline on a statement is kept by the client.
func SessionStatements(config driver.Config) []string {
	timeouts := config.Timeouts
	if timeouts.Lock == 0 {
		timeouts = driver.DefaultTimeouts()
	}
	return []string{
		"SET NOCOUNT ON",
		"SET QUOTED_IDENTIFIER ON",
		"SET ANSI_NULLS ON",
		"SET DEADLOCK_PRIORITY LOW",
		fmt.Sprintf("SET LOCK_TIMEOUT %d", timeouts.Lock.Milliseconds()),
	}
}

type connection struct {
	db     *sql.DB
	config driver.Config
}

func (c *connection) pin(ctx context.Context) error {
	for _, statement := range SessionStatements(c.config) {
		if _, err := c.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("pin the session: %w", err)
		}
	}
	return nil
}

func (c *connection) Close() error {
	if err := c.db.Close(); err != nil {
		return fmt.Errorf("close the connection: %w", err)
	}
	return nil
}
