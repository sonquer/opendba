package postgres

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgconn/ctxwatch"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sonquer/opendba/src/cli/internal/driver"
)

const Name = "postgres"

const DefaultPort = 5432

type pool interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Begin(ctx context.Context) (pgx.Tx, error)
	BeginTx(ctx context.Context, options pgx.TxOptions) (pgx.Tx, error)
	Ping(ctx context.Context) error
	Close()
}

type Driver struct{}

func New() Driver { return Driver{} }

func (Driver) Name() string { return Name }

func (Driver) Title() string { return "PostgreSQL" }

func (Driver) Capabilities() driver.Capabilities {
	return driver.Capabilities{
		Explain:         true,
		Relations:       true,
		Health:          true,
		IndexStats:      true,
		ReadOnlySession: true,
		Cancel:          true,
		Sessions:        true,
	}
}

func (d Driver) Connect(ctx context.Context, config driver.Config) (driver.Conn, error) {
	settings, err := poolConfig(config)
	if err != nil {
		return nil, err
	}
	pool, err := pgxpool.NewWithConfig(ctx, settings)
	if err != nil {
		return nil, fmt.Errorf("connect to %s: %w", config.Target(), err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("connect to %s: %w", config.Target(), err)
	}
	return &connection{db: pool, config: config}, nil
}

func poolConfig(config driver.Config) (*pgxpool.Config, error) {
	settings, err := pgxpool.ParseConfig(DSN(config))
	if err != nil {
		return nil, fmt.Errorf("read the connection settings: %w", err)
	}
	settings.ConnConfig.Password = string(config.Password)
	settings.MaxConns = maxConns
	settings.ConnConfig.BuildContextWatcherHandler = cancelRequests
	if timeout := config.Timeouts.Connect; timeout > 0 {
		settings.ConnConfig.ConnectTimeout = timeout
	}
	settings.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		for _, statement := range SessionStatements(config) {
			if _, err := conn.Exec(ctx, statement); err != nil {
				return fmt.Errorf("pin the session: %w", err)
			}
		}
		return nil
	}
	return settings, nil
}

// maxConns is how many connections one session opens.
const maxConns = 3

// cancelRequests makes a cancelled context reach the server.
func cancelRequests(conn *pgconn.PgConn) ctxwatch.Handler {
	return &pgconn.CancelRequestContextWatcherHandler{
		Conn:          conn,
		DeadlineDelay: cancelDeadline,
	}
}

// cancelDeadline is how long the socket is given to answer a cancel request
// before it is torn down anyway.
const cancelDeadline = 5 * time.Second

func DSN(config driver.Config) string {
	target := &url.URL{
		Scheme: "postgres",
		Host:   hostPort(config),
	}
	if config.Database != "" {
		target.Path = "/" + config.Database
	}
	if config.User != "" {
		target.User = url.User(config.User)
	}
	query := url.Values{}
	if config.SSLMode != "" {
		query.Set("sslmode", config.SSLMode)
	}
	query.Set("application_name", application(config))
	if config.Options != "" {
		query.Set("options", config.Options)
	}
	target.RawQuery = query.Encode()
	return target.String()
}

func application(config driver.Config) string { return config.ApplicationName() }

func hostPort(config driver.Config) string {
	host := config.Host
	if host == "" {
		host = "localhost"
	}
	port := config.Port
	if port == 0 {
		port = DefaultPort
	}
	return host + ":" + strconv.Itoa(port)
}

func SessionStatements(config driver.Config) []string {
	timeouts := config.Timeouts
	if timeouts.Statement == 0 {
		timeouts = driver.DefaultTimeouts()
	}
	statements := []string{
		setStatement("statement_timeout", timeouts.Statement),
		setStatement("lock_timeout", timeouts.Lock),
		setStatement("idle_in_transaction_session_timeout", timeouts.Idle),
	}
	if config.ReadOnly() {
		statements = append(statements, "SET default_transaction_read_only = on")
	}
	return statements
}

func setStatement(name string, timeout time.Duration) string {
	return fmt.Sprintf("SET %s = %d", name, timeout.Milliseconds())
}

func Redact(dsn string) string {
	parsed, err := url.Parse(dsn)
	if err != nil {
		return "postgres://redacted"
	}
	if parsed.User != nil {
		if _, hasPassword := parsed.User.Password(); hasPassword {
			parsed.User = url.UserPassword(parsed.User.Username(), "xxxxx")
		}
	}
	query := parsed.Query()
	for _, name := range []string{"password", "passfile"} {
		if query.Has(name) {
			query.Set(name, "xxxxx")
		}
	}
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func Split(dsn string) (string, []byte, error) {
	parsed, err := url.Parse(strings.TrimSpace(dsn))
	if err != nil {
		return "", nil, fmt.Errorf("read the connection string: %w", err)
	}
	if parsed.User == nil {
		return parsed.String(), nil, nil
	}
	password, ok := parsed.User.Password()
	if !ok {
		return parsed.String(), nil, nil
	}
	parsed.User = url.User(parsed.User.Username())
	return parsed.String(), []byte(password), nil
}

type connection struct {
	db     pool
	config driver.Config
}

func (c *connection) Close() error {
	c.db.Close()
	return nil
}
