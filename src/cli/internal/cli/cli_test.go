package cli

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/zalando/go-keyring"

	"github.com/sonquer/tui4db/src/cli/internal/config"
	"github.com/sonquer/tui4db/src/cli/pkg/secretref"
	"github.com/sonquer/tui4db/src/cli/pkg/sqldialect"

	_ "modernc.org/sqlite"
)

const fixture = `
CREATE TABLE users (id integer PRIMARY KEY, email text NOT NULL UNIQUE);
CREATE TABLE orders (id integer PRIMARY KEY, user_id integer NOT NULL REFERENCES users (id), total real);
CREATE INDEX orders_user_id_idx ON orders (user_id);
INSERT INTO users (id, email) VALUES (1, 'a@example.com'), (2, 'b@example.com');
INSERT INTO orders (id, user_id, total) VALUES (1, 1, 10.5);
`

type harness struct {
	app    App
	store  config.Store
	stdout *bytes.Buffer
	stderr *bytes.Buffer
}

func (h harness) out() string { return ansi.Strip(h.stdout.String()) }

func (h harness) err() string { return ansi.Strip(h.stderr.String()) }

func seedDatabase(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "app.db")
	database, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer database.Close()
	for _, statement := range strings.Split(fixture, ";") {
		if strings.TrimSpace(statement) == "" {
			continue
		}
		if _, err := database.Exec(statement); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	return path
}

func newHarness(t *testing.T, connections ...config.Connection) harness {
	t.Helper()
	root := t.TempDir()
	paths := config.Paths{Config: filepath.Join(root, "config"), State: filepath.Join(root, "state")}
	if err := paths.Ensure(); err != nil {
		t.Fatal(err)
	}
	store := config.NewStore(paths)
	if len(connections) > 0 {
		if err := store.SaveProfiles(config.Profiles{Connections: connections}); err != nil {
			t.Fatal(err)
		}
	}
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	return harness{
		store:  store,
		stdout: stdout,
		stderr: stderr,
		app: App{
			Store:    store,
			Registry: Registry(),
			Secrets:  Secrets(paths, func() ([]byte, error) { return []byte("passphrase"), nil }),
			Dialects: sqldialect.Default(),
			Stdout:   stdout,
			Stderr:   stderr,
			Version:  "0.1.0",
		},
	}
}

func localConnection(t *testing.T) config.Connection {
	t.Helper()
	return config.Connection{
		ID:     "01J000000000000000000001",
		Name:   "local",
		Driver: "sqlite",
		File:   seedDatabase(t),
		Mode:   config.ReadOnly,
		Color:  "green",
	}
}

func TestRegistryOffersOnlyWhatIsImplemented(t *testing.T) {
	registry := Registry()
	entries := registry.Entries()
	if len(entries) != 2 {
		t.Fatalf("entries = %+v", entries)
	}
	if entries[0].Name != "postgres" || entries[1].Name != "sqlite" {
		t.Fatalf("entries = %+v", entries)
	}
	for _, name := range []string{"mysql", "mariadb", "sqlserver"} {
		if _, err := registry.Get(name); err == nil {
			t.Errorf("%s is not implemented and must not be offered", name)
		}
	}
}

func TestSecretsCoverEveryBackend(t *testing.T) {
	paths := config.Paths{Config: t.TempDir()}
	store := Secrets(paths, func() ([]byte, error) { return []byte("x"), nil })
	schemes := strings.Join(store.Schemes(), " ")
	for _, scheme := range []string{secretref.SchemeKeyring, secretref.SchemeVault, secretref.SchemeEnv, secretref.SchemeCommand} {
		if !strings.Contains(schemes, scheme) {
			t.Errorf("missing backend %q in %q", scheme, schemes)
		}
	}
}

func TestVersionAndHelp(t *testing.T) {
	h := newHarness(t)
	if code := h.app.Run(context.Background(), []string{"version"}); code != ExitOK {
		t.Fatalf("exit = %d", code)
	}
	if strings.TrimSpace(h.out()) != "0.1.0" {
		t.Errorf("version = %q", h.out())
	}

	h = newHarness(t)
	if code := h.app.Run(context.Background(), []string{"help"}); code != ExitOK {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(h.out(), "a terminal workbench for databases") {
		t.Errorf("usage = %q", h.out())
	}
}

func TestUnknownCommand(t *testing.T) {
	h := newHarness(t)
	if code := h.app.Run(context.Background(), []string{"nonsense"}); code != ExitUsage {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(h.err(), `unknown command "nonsense"`) {
		t.Errorf("stderr = %q", h.err())
	}
}

func TestConnections(t *testing.T) {
	h := newHarness(t, localConnection(t))
	if code := h.app.Run(context.Background(), []string{"connections"}); code != ExitOK {
		t.Fatalf("exit = %d", code)
	}
	out := h.out()
	for _, want := range []string{"local", "sqlite", "READ ONLY"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q: %s", want, out)
		}
	}
}

func TestConnectionsWithoutAnyProfile(t *testing.T) {
	h := newHarness(t)
	if code := h.app.Run(context.Background(), []string{"connections"}); code != ExitOK {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(h.out(), "no connections are configured") {
		t.Errorf("output = %q", h.out())
	}
}

func TestInspect(t *testing.T) {
	h := newHarness(t, localConnection(t))
	if code := h.app.Run(context.Background(), []string{"inspect"}); code != ExitOK {
		t.Fatalf("exit = %d\n%s", code, h.err())
	}
	out := h.out()
	for _, want := range []string{"local", "integrity", "read only", "6 ok"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestInspectJSON(t *testing.T) {
	h := newHarness(t, localConnection(t))
	if code := h.app.Run(context.Background(), []string{"inspect", "--json"}); code != ExitOK {
		t.Fatalf("exit = %d\n%s", code, h.err())
	}
	var document map[string]any
	if err := json.Unmarshal(h.stdout.Bytes(), &document); err != nil {
		t.Fatalf("the report must be valid json: %v", err)
	}
	if document["schema_version"] != "1.0.0" {
		t.Errorf("schema version = %v", document["schema_version"])
	}
	if _, ok := document["findings"]; !ok {
		t.Error("the report must carry findings")
	}
}

func TestSchemaAndIndexes(t *testing.T) {
	connection := localConnection(t)
	for _, command := range []string{"schema", "indexes"} {
		t.Run(command, func(t *testing.T) {
			h := newHarness(t, connection)
			if code := h.app.Run(context.Background(), []string{command}); code != ExitOK {
				t.Fatalf("exit = %d\n%s", code, h.err())
			}
			if !strings.Contains(h.out(), "orders") {
				t.Errorf("output = %s", h.out())
			}

			h = newHarness(t, connection)
			if code := h.app.Run(context.Background(), []string{command, "--json"}); code != ExitOK {
				t.Fatalf("exit = %d\n%s", code, h.err())
			}
			var document map[string]any
			if err := json.Unmarshal(h.stdout.Bytes(), &document); err != nil {
				t.Fatalf("json: %v", err)
			}
		})
	}
}

func TestQuery(t *testing.T) {
	h := newHarness(t, localConnection(t))
	code := h.app.Run(context.Background(), []string{"query", "SELECT email FROM users ORDER BY id"})
	if code != ExitOK {
		t.Fatalf("exit = %d\n%s", code, h.err())
	}
	out := h.out()
	if !strings.Contains(out, "a@example.com") || !strings.Contains(out, "2 rows") {
		t.Errorf("output = %s", out)
	}
}

func TestQueryJSON(t *testing.T) {
	h := newHarness(t, localConnection(t))
	code := h.app.Run(context.Background(), []string{"query", "--json", "SELECT email FROM users WHERE id = 1"})
	if code != ExitOK {
		t.Fatalf("exit = %d\n%s", code, h.err())
	}
	var document map[string]any
	if err := json.Unmarshal(h.stdout.Bytes(), &document); err != nil {
		t.Fatalf("json: %v", err)
	}
	if document["row_count"].(float64) != 1 {
		t.Errorf("row count = %v", document["row_count"])
	}
	statement, _ := document["statement"].(string)
	if strings.Contains(statement, "= 1") {
		t.Errorf("the statement must be normalised: %q", statement)
	}
}

func TestQueryRefusesToWrite(t *testing.T) {
	h := newHarness(t, localConnection(t))
	code := h.app.Run(context.Background(), []string{"query", "DELETE FROM users"})
	if code != ExitBlocked {
		t.Fatalf("exit = %d, want blocked", code)
	}
	if !strings.Contains(h.err(), "blocked") {
		t.Errorf("stderr = %q", h.err())
	}
	if h.out() != "" {
		t.Errorf("nothing must be printed: %q", h.out())
	}
}

func TestQueryNeedsAStatement(t *testing.T) {
	h := newHarness(t, localConnection(t))
	if code := h.app.Run(context.Background(), []string{"query"}); code != ExitUsage {
		t.Fatalf("exit = %d", code)
	}
}

func TestQueryReportsServerErrors(t *testing.T) {
	h := newHarness(t, localConnection(t))
	if code := h.app.Run(context.Background(), []string{"query", "SELECT * FROM missing"}); code != ExitFailure {
		t.Fatalf("exit = %d", code)
	}
}

func TestQueryRespectsTheRowLimit(t *testing.T) {
	h := newHarness(t, localConnection(t))
	code := h.app.Run(context.Background(), []string{"query", "--limit", "1", "--json", "SELECT email FROM users"})
	if code != ExitOK {
		t.Fatalf("exit = %d\n%s", code, h.err())
	}
	var document map[string]any
	if err := json.Unmarshal(h.stdout.Bytes(), &document); err != nil {
		t.Fatal(err)
	}
	if document["row_count"].(float64) != 1 || document["truncated"] != true {
		t.Fatalf("document = %v", document)
	}
}

func TestConnectionSelection(t *testing.T) {
	first := localConnection(t)
	second := localConnection(t)
	second.ID = "01J000000000000000000002"
	second.Name = "second"
	h := newHarness(t, first, second)

	if code := h.app.Run(context.Background(), []string{"inspect", "--connection", "second"}); code != ExitOK {
		t.Fatalf("exit = %d\n%s", code, h.err())
	}
	if !strings.Contains(h.out(), "second") {
		t.Errorf("output = %s", h.out())
	}

	h = newHarness(t, first)
	if code := h.app.Run(context.Background(), []string{"inspect", "--connection", "missing"}); code != ExitFailure {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(h.err(), "no connection named") {
		t.Errorf("stderr = %q", h.err())
	}
}

func TestWithoutAnyConnection(t *testing.T) {
	h := newHarness(t)
	if code := h.app.Run(context.Background(), []string{"inspect"}); code != ExitFailure {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(h.err(), "no connections are configured") {
		t.Errorf("stderr = %q", h.err())
	}
}

func TestUnknownDriverIsReported(t *testing.T) {
	connection := localConnection(t)
	connection.Driver = "oracle"
	h := newHarness(t, connection)
	if code := h.app.Run(context.Background(), []string{"inspect"}); code != ExitFailure {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(h.err(), "no driver named") {
		t.Errorf("stderr = %q", h.err())
	}
}

func TestUnreadableConfigurationIsReported(t *testing.T) {
	h := newHarness(t, localConnection(t))
	if err := os.WriteFile(h.store.Paths.SettingsFile(), []byte("["), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := h.app.Run(context.Background(), []string{"inspect"}); code != ExitFailure {
		t.Fatalf("exit = %d", code)
	}
}

func TestSecretsAreResolved(t *testing.T) {
	connection := localConnection(t)
	connection.Secret = "env:TUI4DB_TEST_PASSWORD"
	t.Setenv("TUI4DB_TEST_PASSWORD", "hunter2")
	h := newHarness(t, connection)
	if code := h.app.Run(context.Background(), []string{"inspect"}); code != ExitOK {
		t.Fatalf("exit = %d\n%s", code, h.err())
	}
}

func TestMissingSecretsAreReported(t *testing.T) {
	connection := localConnection(t)
	connection.Secret = "env:TUI4DB_ABSENT_PASSWORD"
	h := newHarness(t, connection)
	if code := h.app.Run(context.Background(), []string{"inspect"}); code != ExitFailure {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(h.err(), "is not set") {
		t.Errorf("stderr = %q", h.err())
	}
}

func TestPromptSecrets(t *testing.T) {
	connection := localConnection(t)
	connection.Secret = "prompt"
	h := newHarness(t, connection)
	if code := h.app.Run(context.Background(), []string{"inspect"}); code != ExitFailure {
		t.Fatalf("without a terminal the run must fail, got %d", code)
	}

	h = newHarness(t, connection)
	asked := ""
	h.app.Prompt = func(prompt string) ([]byte, error) {
		asked = prompt
		return []byte("hunter2"), nil
	}
	if code := h.app.Run(context.Background(), []string{"inspect"}); code != ExitOK {
		t.Fatalf("exit = %d\n%s", code, h.err())
	}
	if !strings.Contains(asked, "local") {
		t.Errorf("the prompt must name the connection: %q", asked)
	}
}

func TestPgpassIsLeftToTheDriver(t *testing.T) {
	connection := localConnection(t)
	connection.Secret = "pgpass"
	h := newHarness(t, connection)
	if code := h.app.Run(context.Background(), []string{"inspect"}); code != ExitOK {
		t.Fatalf("exit = %d\n%s", code, h.err())
	}
}

func TestBrokenSecretReferenceIsReported(t *testing.T) {
	connection := localConnection(t)
	connection.Secret = "env:"
	h := newHarness(t, connection)
	if code := h.app.Run(context.Background(), []string{"inspect"}); code != ExitFailure {
		t.Fatalf("exit = %d", code)
	}
}

func TestLaunchIsUsedForTheInterface(t *testing.T) {
	h := newHarness(t, localConnection(t))
	launched := false
	h.app.Launch = func(session Session, workspace Workspace) error {
		launched = true
		if workspace == nil {
			t.Error("the interface must be able to reach the other connections")
		}
		if session.Connection.Name != "local" || session.Conn == nil {
			t.Errorf("session = %+v", session)
		}
		return nil
	}
	if code := h.app.Run(context.Background(), nil); code != ExitOK {
		t.Fatalf("exit = %d\n%s", code, h.err())
	}
	if !launched {
		t.Error("the interface must be launched")
	}
}

func TestLaunchFailures(t *testing.T) {
	h := newHarness(t, localConnection(t))
	if code := h.app.Run(context.Background(), nil); code != ExitFailure {
		t.Fatalf("a build without an interface must say so, got %d", code)
	}

	h = newHarness(t, localConnection(t))
	h.app.Launch = func(Session, Workspace) error { return errors.New("no terminal") }
	if code := h.app.Run(context.Background(), nil); code != ExitFailure {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(h.err(), "no terminal") {
		t.Errorf("stderr = %q", h.err())
	}
}

func TestFlagErrors(t *testing.T) {
	h := newHarness(t, localConnection(t))
	if code := h.app.Run(context.Background(), []string{"inspect", "--nope"}); code != ExitUsage {
		t.Fatalf("exit = %d", code)
	}
	h = newHarness(t, localConnection(t))
	if code := h.app.Run(context.Background(), []string{"inspect", "-h"}); code != ExitOK {
		t.Fatalf("exit = %d", code)
	}
}

func TestTargetDescribesEveryConnection(t *testing.T) {
	cases := map[string]config.Connection{
		"/tmp/app.db":             {File: "/tmp/app.db"},
		"db.example.com:5432/app": {Host: "db.example.com", Port: 5432, Database: "app"},
		"db.example.com":          {Host: "db.example.com"},
	}
	for want, connection := range cases {
		if got := Target(connection); got != want {
			t.Errorf("Target(%+v) = %q, want %q", connection, got, want)
		}
	}
}

func TestTimeoutsComeFromSettings(t *testing.T) {
	timeouts := Timeouts(config.SafetySettings{QueryTimeout: "5s", LockTimeout: "500ms"})
	if timeouts.Statement.String() != "5s" || timeouts.Lock.String() != "500ms" {
		t.Fatalf("timeouts = %+v", timeouts)
	}
	defaults := Timeouts(config.SafetySettings{QueryTimeout: "nonsense"})
	if defaults.Statement.String() != "15s" {
		t.Errorf("an unreadable timeout must fall back to the default: %+v", defaults)
	}
}

func TestModeMapping(t *testing.T) {
	if Mode(config.ReadWrite).Writable() != true {
		t.Error("read write must be writable")
	}
	if Mode(config.ReadOnly).Writable() || Mode("nonsense").Writable() {
		t.Error("anything but read write must be read only")
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("no space") }

func TestReportWriteFailuresAreReported(t *testing.T) {
	commands := [][]string{
		{"inspect", "--json"},
		{"schema", "--json"},
		{"indexes", "--json"},
		{"query", "--json", "SELECT 1"},
	}
	for _, args := range commands {
		t.Run(args[0], func(t *testing.T) {
			h := newHarness(t, localConnection(t))
			h.app.Stdout = failingWriter{}
			if code := h.app.Run(context.Background(), args); code != ExitFailure {
				t.Fatalf("exit = %d, want failure", code)
			}
			if h.err() == "" {
				t.Error("the failure must be reported")
			}
		})
	}
}

func TestUnhealthyDatabaseFailsTheCommand(t *testing.T) {
	connection := localConnection(t)
	corrupt := filepath.Join(t.TempDir(), "corrupt.db")
	if err := os.WriteFile(corrupt, []byte("SQLite format 3\x00 nonsense"), 0o600); err != nil {
		t.Fatal(err)
	}
	connection.File = corrupt
	h := newHarness(t, connection)
	if code := h.app.Run(context.Background(), []string{"inspect"}); code != ExitFailure {
		t.Fatalf("a database that cannot be opened must fail, got %d", code)
	}
}

func TestReadFailuresAreReported(t *testing.T) {
	connection := localConnection(t)
	commands := map[string][]string{
		"schema":  {"schema", "--schema", "nope"},
		"indexes": {"indexes", "--schema", "nope"},
		"inspect": {"inspect", "--schema", "nope"},
	}
	for name, args := range commands {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, connection)
			code := h.app.Run(context.Background(), args)
			if name == "inspect" {
				if code != ExitOK {
					t.Fatalf("the health report ignores the schema flag, got %d", code)
				}
				return
			}
			if code != ExitFailure {
				t.Fatalf("exit = %d, want failure", code)
			}
		})
	}
}

func TestUnreadableProfilesAreReported(t *testing.T) {
	h := newHarness(t, localConnection(t))
	if err := os.WriteFile(h.store.Paths.ProfilesFile(), []byte("[[connection]]\nid = \"a\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := h.app.Run(context.Background(), []string{"connections"}); code != ExitFailure {
		t.Fatalf("exit = %d", code)
	}
	h = newHarness(t, localConnection(t))
	if err := os.WriteFile(h.store.Paths.ProfilesFile(), []byte("[[connection]]\nid = \"a\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := h.app.Run(context.Background(), []string{"inspect"}); code != ExitFailure {
		t.Fatalf("exit = %d", code)
	}
}

func TestConnectionsReportsWriteFailures(t *testing.T) {
	h := newHarness(t, localConnection(t))
	h.app.Stdout = failingWriter{}
	if code := h.app.Run(context.Background(), []string{"connections"}); code != ExitOK {
		t.Fatalf("exit = %d", code)
	}
}

func TestDefaultSchemaIsUsed(t *testing.T) {
	connection := localConnection(t)
	connection.DefaultSchema = "main"
	h := newHarness(t, connection)
	if code := h.app.Run(context.Background(), []string{"schema"}); code != ExitOK {
		t.Fatalf("exit = %d\n%s", code, h.err())
	}
	if !strings.Contains(h.out(), "main.users") {
		t.Errorf("output = %s", h.out())
	}
}

func TestSetupIsOfferedWhenNothingIsConfigured(t *testing.T) {
	h := newHarness(t)
	var offered Setup
	h.app.Wizard = func(setup Setup) (SetupOutcome, error) {
		offered = setup
		return SetupOutcome{}, nil
	}
	if code := h.app.Run(context.Background(), nil); code != ExitOK {
		t.Fatalf("exit = %d\n%s", code, h.err())
	}
	if offered.Registry == nil || offered.Secrets == nil {
		t.Fatalf("the wizard needs everything it saves with: %+v", offered)
	}
	if !strings.Contains(h.out(), "nothing was saved") {
		t.Errorf("output = %q", h.out())
	}
}

func TestASavedConnectionIsOpenedStraightAway(t *testing.T) {
	h := newHarness(t)
	connection := localConnection(t)
	launched := false
	h.app.Wizard = func(setup Setup) (SetupOutcome, error) {
		profiles, err := setup.Store.LoadProfiles()
		if err != nil {
			t.Fatal(err)
		}
		if err := profiles.Upsert(connection); err != nil {
			t.Fatal(err)
		}
		if err := setup.Store.SaveProfiles(profiles); err != nil {
			t.Fatal(err)
		}
		return SetupOutcome{Connection: connection, Saved: true, Warning: "the role can write"}, nil
	}
	h.app.Launch = func(session Session, _ Workspace) error {
		launched = true
		if session.Connection.Name != connection.Name {
			t.Errorf("session = %+v", session.Connection)
		}
		if session.Warning != "the role can write" {
			t.Errorf("the wizard warning must reach the interface: %q", session.Warning)
		}
		return nil
	}
	if code := h.app.Run(context.Background(), nil); code != ExitOK {
		t.Fatalf("exit = %d\n%s", code, h.err())
	}
	if !launched {
		t.Error("the wizard must hand over to the interface")
	}
}

func TestAProfileThatCannotBeOpenedAfterSetupFails(t *testing.T) {
	h := newHarness(t)
	h.app.Wizard = func(Setup) (SetupOutcome, error) {
		return SetupOutcome{Connection: config.Connection{Name: "ghost"}, Saved: true}, nil
	}
	if code := h.app.Run(context.Background(), nil); code != ExitFailure {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(h.err(), "ghost") {
		t.Errorf("stderr = %q", h.err())
	}
}

func TestSetupFailures(t *testing.T) {
	h := newHarness(t)
	if code := h.app.Run(context.Background(), nil); code != ExitFailure {
		t.Fatalf("a build without a wizard must say so, got %d", code)
	}
	if !strings.Contains(h.err(), "cannot set one up") {
		t.Errorf("stderr = %q", h.err())
	}

	h = newHarness(t)
	h.app.Wizard = func(Setup) (SetupOutcome, error) { return SetupOutcome{}, errors.New("no terminal") }
	if code := h.app.Run(context.Background(), nil); code != ExitFailure {
		t.Fatalf("exit = %d", code)
	}

	h = newHarness(t)
	if err := os.WriteFile(h.store.Paths.ProfilesFile(), []byte("[[connection]]\nid=\"a\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := h.app.Run(context.Background(), nil); code != ExitFailure {
		t.Fatalf("unreadable profiles must fail, got %d", code)
	}
}

func newSetup(t *testing.T) (Setup, config.Store) {
	t.Helper()
	keyring.MockInit()
	root := t.TempDir()
	paths := config.Paths{Config: filepath.Join(root, "config"), State: filepath.Join(root, "state")}
	if err := paths.Ensure(); err != nil {
		t.Fatal(err)
	}
	store := config.NewStore(paths)
	return Setup{
		Store:    store,
		Registry: Registry(),
		Secrets:  Secrets(paths, func() ([]byte, error) { return []byte("passphrase"), nil }),
	}, store
}

func TestSetupTestsAndSaves(t *testing.T) {
	setup, store := newSetup(t)
	connection := config.Connection{
		ID:     setup.NewID(),
		Name:   "local",
		Driver: "sqlite",
		File:   seedDatabase(t),
		Mode:   config.ReadOnly,
		Color:  "green",
	}
	info, err := setup.Test(context.Background(), connection, nil)
	if err != nil {
		t.Fatalf("Test: %v", err)
	}
	if info.Driver != "sqlite" || info.Version == "" {
		t.Fatalf("info = %+v", info)
	}
	if err := setup.Save(connection, []byte("hunter2")); err != nil {
		t.Fatalf("Save: %v", err)
	}
	profiles, err := store.LoadProfiles()
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles.Connections) != 1 || !strings.HasPrefix(profiles.Connections[0].Secret, "keyring:") {
		t.Fatalf("profiles = %+v", profiles)
	}
}

func TestSetupGeneratesDistinctIdentifiers(t *testing.T) {
	setup, _ := newSetup(t)
	first := setup.NewID()
	second := setup.NewID()
	if first == second {
		t.Error("every connection needs its own identifier")
	}
	if first == "" {
		t.Error("an identifier must not be empty")
	}
	fixed := Setup{ID: func() string { return "fixed" }}
	if fixed.NewID() != "fixed" {
		t.Error("an explicit generator must be used")
	}
}

func TestSetupReportsFailures(t *testing.T) {
	setup, store := newSetup(t)
	ctx := context.Background()
	unknown := config.Connection{ID: "a", Name: "x", Driver: "oracle", Mode: config.ReadOnly}
	if _, err := setup.Test(ctx, unknown, nil); err == nil {
		t.Error("an unknown driver must be reported")
	}
	missing := config.Connection{ID: "a", Name: "x", Driver: "sqlite", File: filepath.Join(t.TempDir(), "missing.db"), Mode: config.ReadOnly}
	if _, err := setup.Test(ctx, missing, nil); err == nil {
		t.Error("a database that is not there must be reported")
	}
	if err := setup.Save(config.Connection{ID: "a"}, nil); err == nil {
		t.Error("an invalid connection must not be saved")
	}
	if err := os.WriteFile(store.Paths.SettingsFile(), []byte("["), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := setup.Test(ctx, missing, nil); err == nil {
		t.Error("unreadable settings must be reported")
	}
}

func TestSetupFallsBackToTheVault(t *testing.T) {
	setup, store := newSetup(t)
	keyring.MockInitWithError(errors.New("no keychain here"))
	t.Cleanup(keyring.MockInit)

	connection := config.Connection{
		ID: setup.NewID(), Name: "local", Driver: "sqlite",
		File: seedDatabase(t), Mode: config.ReadOnly, Color: "green",
	}
	if err := setup.Save(connection, []byte("hunter2")); err != nil {
		t.Fatalf("Save: %v", err)
	}
	profiles, err := store.LoadProfiles()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(profiles.Connections[0].Secret, "vault:") {
		t.Fatalf("without a keychain the vault must be used: %q", profiles.Connections[0].Secret)
	}
}

func TestSetupReportsAnUnusableSecretStore(t *testing.T) {
	setup, _ := newSetup(t)
	keyring.MockInitWithError(errors.New("no keychain here"))
	t.Cleanup(keyring.MockInit)
	setup.Secrets = Secrets(config.Paths{Config: t.TempDir()}, func() ([]byte, error) {
		return nil, errors.New("no passphrase")
	})
	connection := config.Connection{ID: "01J", Name: "local", Driver: "sqlite", File: "x.db", Mode: config.ReadOnly}
	if err := setup.Save(connection, []byte("hunter2")); err == nil {
		t.Fatal("a password that cannot be stored must stop the save")
	}
}

func TestParseDSN(t *testing.T) {
	parsed, err := ParseDSN("postgres://user@db.example.com:6432/app?sslmode=verify-full")
	if err != nil {
		t.Fatalf("ParseDSN: %v", err)
	}
	if parsed.Host != "db.example.com" || parsed.Port != 6432 || parsed.Database != "app" {
		t.Fatalf("parsed = %+v", parsed)
	}
	if parsed.User != "user" || parsed.SSLMode != "verify-full" {
		t.Fatalf("parsed = %+v", parsed)
	}

	defaults, err := ParseDSN("postgres://db.example.com/app")
	if err != nil {
		t.Fatalf("ParseDSN: %v", err)
	}
	if defaults.Port != 5432 || defaults.SSLMode != "prefer" {
		t.Fatalf("defaults = %+v", defaults)
	}

	for _, dsn := range []string{"nonsense", "://nope", "postgres://host:abc/app"} {
		if _, err := ParseDSN(dsn); err == nil {
			t.Errorf("ParseDSN(%q) must fail", dsn)
		}
	}
}

func TestSplitDSN(t *testing.T) {
	remainder, password, err := SplitDSN("postgres://user:hunter2@host/app")
	if err != nil {
		t.Fatalf("SplitDSN: %v", err)
	}
	if string(password) != "hunter2" || strings.Contains(remainder, "hunter2") {
		t.Fatalf("remainder = %q password = %q", remainder, password)
	}
}

func TestTheWorkspaceListsOpensAndRemovesConnections(t *testing.T) {
	keyring.MockInit()
	connection := localConnection(t)
	h := newHarness(t, connection)
	workspace := Workspace(h.app)

	profiles, err := workspace.Profiles()
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles.Connections) != 1 || profiles.Connections[0].Name != "local" {
		t.Fatalf("profiles = %+v", profiles)
	}
	if workspace.Setup().Registry == nil {
		t.Error("the workspace must be able to add a connection")
	}

	session, cleanup, err := workspace.Open(context.Background(), "local")
	if err != nil {
		t.Fatal(err)
	}
	if session.Connection.Name != "local" || session.Conn == nil {
		t.Fatalf("session = %+v", session.Connection)
	}
	cleanup()

	if err := workspace.Remove(context.Background(), "local"); err != nil {
		t.Fatal(err)
	}
	profiles, err = workspace.Profiles()
	if err != nil {
		t.Fatal(err)
	}
	if !profiles.IsEmpty() {
		t.Fatalf("the connection must be gone: %+v", profiles)
	}
}

func TestRemovingAConnectionTakesItsPasswordWithIt(t *testing.T) {
	keyring.MockInit()
	connection := localConnection(t)
	connection.Secret = secretref.ForKeyring(connection.ID).String()
	h := newHarness(t, connection)
	ref := secretref.ForKeyring(connection.ID)
	if err := h.app.Secrets.Set(context.Background(), ref, []byte("hunter2")); err != nil {
		t.Fatal(err)
	}

	if err := h.app.Remove(context.Background(), "local"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.app.Secrets.Get(context.Background(), ref); err == nil {
		t.Error("the password must go with the connection")
	}
}

func TestRemovingReportsWhatItCannotDo(t *testing.T) {
	keyring.MockInit()
	h := newHarness(t, localConnection(t))
	if err := h.app.Remove(context.Background(), "elsewhere"); err == nil {
		t.Error("a connection that does not exist cannot be removed")
	}

	external := localConnection(t)
	external.Name = "prompted"
	external.ID = "01J000000000000000000002"
	external.Secret = "prompt:"
	profiles, err := h.store.LoadProfiles()
	if err != nil {
		t.Fatal(err)
	}
	if err := profiles.Upsert(external); err != nil {
		t.Fatal(err)
	}
	if err := h.store.SaveProfiles(profiles); err != nil {
		t.Fatal(err)
	}
	if err := h.app.Remove(context.Background(), "prompted"); err != nil {
		t.Errorf("a password that lives outside the store is nothing to remove: %v", err)
	}

	broken := localConnection(t)
	broken.Name = "broken"
	broken.ID = "01J000000000000000000003"
	broken.Secret = "nonsense"
	profiles, err = h.store.LoadProfiles()
	if err != nil {
		t.Fatal(err)
	}
	if err := profiles.Upsert(broken); err != nil {
		t.Fatal(err)
	}
	if err := h.store.SaveProfiles(profiles); err != nil {
		t.Fatal(err)
	}
	if err := h.app.Remove(context.Background(), "broken"); err != nil {
		t.Errorf("a reference that cannot be read is nothing to remove: %v", err)
	}
}

func TestRemovingReportsAKeychainItCannotReach(t *testing.T) {
	keyring.MockInit()
	connection := localConnection(t)
	connection.Secret = secretref.ForKeyring(connection.ID).String()
	h := newHarness(t, connection)
	keyring.MockInitWithError(errors.New("no keychain here"))
	t.Cleanup(keyring.MockInit)

	err := h.app.Remove(context.Background(), "local")
	if err == nil || !strings.Contains(err.Error(), "could not be removed") {
		t.Errorf("err = %v", err)
	}
}
