package mssql

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/sonquer/opendba/src/cli/internal/driver"
	"github.com/sonquer/opendba/src/cli/pkg/sqlguard"
)

func readOnlyConfig() driver.Config {
	return driver.Config{
		Host: "db.internal", Port: 1433, Database: "app", User: "reader",
		Password: []byte("hunter2"), SSLMode: "require", Application: "production",
		Mode: sqlguard.ModeReadOnly, Timeouts: driver.DefaultTimeouts(),
	}
}

func writableConfig() driver.Config {
	config := readOnlyConfig()
	config.Mode = sqlguard.ModeReadWrite
	return config
}

// mocked is a connection whose server is a script. Every test that ends without
// having run the whole script fails.
func mocked(t *testing.T, config driver.Config) (*connection, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	if err != nil {
		t.Fatalf("open a mock database: %v", err)
	}
	t.Cleanup(func() {
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("the server was not asked what it was told it would be: %v", err)
		}
		_ = db.Close()
	})
	return &connection{db: db, config: config}, mock
}

func TestDriverIdentity(t *testing.T) {
	d := New()
	if d.Name() != Name || d.Title() != "SQL Server" {
		t.Errorf("identity = %q, %q", d.Name(), d.Title())
	}
	caps := d.Capabilities()
	if !caps.Explain || !caps.Relations || !caps.Health || !caps.IndexStats || !caps.Cancel || !caps.Sessions {
		t.Errorf("capabilities = %+v", caps)
	}
	if caps.ReadOnlySession {
		t.Error("sql server has no read only session, and must not claim one")
	}
	if caps.FileBased || caps.DefaultPort != DefaultPort {
		t.Errorf("capabilities = %+v", caps)
	}
}

func TestDSN(t *testing.T) {
	cases := []struct {
		name   string
		config driver.Config
		want   []string
		absent []string
	}{
		{
			name:   "a server on a port",
			config: readOnlyConfig(),
			want: []string{
				"sqlserver://reader@db.internal:1433?", "database=app",
				"app+name=opendba%2Fproduction", "encrypt=true", "trustservercertificate=true",
			},
			absent: []string{"hunter2"},
		},
		{
			name:   "a named instance is found by name rather than by port",
			config: driver.Config{Host: `WIN-SQL\SQLEXPRESS`, Port: 1433, SSLMode: "prefer"},
			want:   []string{"sqlserver://WIN-SQL/SQLEXPRESS?", "encrypt=false"},
			absent: []string{":1433"},
		},
		{
			name:   "nothing filled in at all",
			config: driver.Config{},
			want:   []string{"sqlserver://localhost:1433?", "app+name=opendba"},
		},
		{
			name:   "a certificate that must be trusted by name",
			config: driver.Config{Host: "db.internal", SSLMode: "verify-full"},
			want:   []string{"hostnameincertificate=db.internal", "trustservercertificate=false"},
		},
		{
			name:   "a certificate that must be signed",
			config: driver.Config{Host: "db.internal", SSLMode: "verify-ca"},
			want:   []string{"encrypt=true", "trustservercertificate=false"},
			absent: []string{"hostnameincertificate"},
		},
		{
			name:   "no encryption at all",
			config: driver.Config{SSLMode: "disable"},
			want:   []string{"encrypt=disable"},
		},
		{
			name:   "a connection that may not take long to make",
			config: driver.Config{Timeouts: driver.Timeouts{Connect: 4 * time.Second}},
			want:   []string{"connection+timeout=4", "dial+timeout=4"},
		},
		{
			name:   "settings this program has no field for",
			config: driver.Config{Options: "applicationintent=ReadOnly&packet+size=8000"},
			want:   []string{"applicationintent=ReadOnly", "packet+size=8000"},
		},
		{
			name:   "settings that are not a query string are ignored",
			config: driver.Config{Options: "%zz"},
			want:   []string{"sqlserver://localhost:1433"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := DSN(c.config)
			for _, want := range c.want {
				if !strings.Contains(got, want) {
					t.Errorf("DSN() = %s, want it to contain %q", got, want)
				}
			}
			for _, absent := range c.absent {
				if strings.Contains(got, absent) {
					t.Errorf("DSN() = %s, want it not to contain %q", got, absent)
				}
			}
		})
	}
}

func TestThePasswordIsOnlyInWhatTheServerIsGiven(t *testing.T) {
	config := readOnlyConfig()
	if strings.Contains(DSN(config), "hunter2") {
		t.Fatal("the connection string that may be shown must not hold the password")
	}
	if !strings.Contains(credentialled(config), "hunter2") {
		t.Fatal("the connection string the server is given must hold the password")
	}
	config.Password = nil
	if credentialled(config) != DSN(config) {
		t.Error("with no password the two must be the same string")
	}
}

func TestRedact(t *testing.T) {
	cases := map[string]string{
		"sqlserver://sa:hunter2@db/app":     "xxxxx",
		"sqlserver://sa@db?password=secret": "xxxxx",
	}
	for dsn, want := range cases {
		got := Redact(dsn)
		if strings.Contains(got, "hunter2") || strings.Contains(got, "secret") {
			t.Errorf("Redact(%q) leaked the password: %s", dsn, got)
		}
		if !strings.Contains(got, want) {
			t.Errorf("Redact(%q) = %s", dsn, got)
		}
	}
	if got := Redact("sqlserver://sa@db/app"); strings.Contains(got, "xxxxx") {
		t.Errorf("Redact() must leave a string with no password alone: %s", got)
	}
	if got := Redact("://nope"); got != "sqlserver://redacted" {
		t.Errorf("Redact() = %s", got)
	}
}

func TestSessionStatements(t *testing.T) {
	statements := strings.Join(SessionStatements(readOnlyConfig()), "\n")
	for _, want := range []string{"SET NOCOUNT ON", "SET QUOTED_IDENTIFIER ON", "SET DEADLOCK_PRIORITY LOW", "SET LOCK_TIMEOUT 2000"} {
		if !strings.Contains(statements, want) {
			t.Errorf("the session must be pinned with %q:\n%s", want, statements)
		}
	}
	if strings.Contains(statements, "READ ONLY") {
		t.Error("sql server has no read only session to pin")
	}
	fallback := strings.Join(SessionStatements(driver.Config{}), "\n")
	if !strings.Contains(fallback, "SET LOCK_TIMEOUT 2000") {
		t.Errorf("a profile with no timeouts must fall back to the defaults:\n%s", fallback)
	}
}

func expectPinning(mock sqlmock.Sqlmock, config driver.Config) {
	for _, statement := range SessionStatements(config) {
		mock.ExpectExec(statement).WillReturnResult(sqlmock.NewResult(0, 0))
	}
}

func TestConnectPinsTheSession(t *testing.T) {
	config := readOnlyConfig()
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual), sqlmock.MonitorPingsOption(true))
	if err != nil {
		t.Fatalf("open a mock database: %v", err)
	}
	mock.ExpectPing()
	expectPinning(mock, config)
	conn, err := connected(context.Background(), db, config)
	if err != nil {
		t.Fatalf("connected() = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet: %v", err)
	}
	mock.ExpectClose()
	if err := conn.Close(); err != nil {
		t.Errorf("Close() = %v", err)
	}
}

func TestConnectReportsAServerThatDoesNotAnswer(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual), sqlmock.MonitorPingsOption(true))
	if err != nil {
		t.Fatalf("open a mock database: %v", err)
	}
	mock.ExpectPing().WillReturnError(errRefused)
	mock.ExpectClose()
	if _, err := connected(context.Background(), db, readOnlyConfig()); err == nil {
		t.Fatal("a server that does not answer must be an error")
	}
}

func TestConnectReportsASessionItCannotPin(t *testing.T) {
	config := readOnlyConfig()
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual), sqlmock.MonitorPingsOption(true))
	if err != nil {
		t.Fatalf("open a mock database: %v", err)
	}
	mock.ExpectPing()
	mock.ExpectExec(SessionStatements(config)[0]).WillReturnError(errRefused)
	mock.ExpectClose()
	if _, err := connected(context.Background(), db, config); err == nil {
		t.Fatal("a session that cannot be pinned must be an error")
	}
}

func TestConnectReadsATimeoutForAnIdleConnection(t *testing.T) {
	config := readOnlyConfig()
	config.Timeouts.Idle = time.Minute
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual), sqlmock.MonitorPingsOption(true))
	if err != nil {
		t.Fatalf("open a mock database: %v", err)
	}
	mock.ExpectPing()
	expectPinning(mock, config)
	if _, err := connected(context.Background(), db, config); err != nil {
		t.Fatalf("connected() = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet: %v", err)
	}
}

func TestCloseReportsAConnectionThatWouldNotClose(t *testing.T) {
	conn, mock := mocked(t, readOnlyConfig())
	mock.ExpectClose().WillReturnError(errRefused)
	if err := conn.Close(); err == nil {
		t.Fatal("a connection that will not close must be an error")
	}
}

func TestConnectReportsAConnectionStringItCannotRead(t *testing.T) {
	_, err := New().Connect(context.Background(), driver.Config{Options: "port=not+a+number"})
	if err == nil {
		t.Fatal("a connection string the driver cannot read must be an error")
	}
	if !strings.Contains(err.Error(), "connection settings") {
		t.Errorf("Connect() = %v", err)
	}
}

// errRefused is what a server says when it will not do the thing.
var errRefused = errors.New("permission was denied")

func TestACertificateCheckedByNameOnANamedInstance(t *testing.T) {
	got := DSN(driver.Config{Host: `WIN-SQL\SQLEXPRESS`, SSLMode: "verify-full"})
	if !strings.Contains(got, "hostnameincertificate=WIN-SQL") {
		t.Errorf("DSN() = %s, want the host without its instance", got)
	}
}
