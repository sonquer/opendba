package app

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/zalando/go-keyring"

	"github.com/sonquer/opendba/src/cli/internal/cli"
	"github.com/sonquer/opendba/src/cli/internal/config"
	"github.com/sonquer/opendba/src/cli/internal/driver"
	"github.com/sonquer/opendba/src/cli/internal/ui"
)

func newSetup(t *testing.T) (cli.Setup, config.Store) {
	t.Helper()
	keyring.MockInit()
	root := t.TempDir()
	paths := config.Paths{
		Config: filepath.Join(root, "config"),
		State:  filepath.Join(root, "state"),
		Data:   filepath.Join(root, "data"),
	}
	if err := paths.Ensure(); err != nil {
		t.Fatal(err)
	}
	store := config.NewStore(paths)
	return cli.Setup{
		Store:    store,
		Registry: cli.Registry(),
		Secrets:  cli.Secrets(paths, func() ([]byte, error) { return []byte("passphrase"), nil }),
		ID:       func() string { return "01J000000000000000000001" },
	}, store
}

func seedFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "app.db")
	opened, err := cli.Registry().Entries()[1].Driver.Connect(context.Background(),
		driver.Config{File: path, Mode: cli.Mode(config.ReadWrite)})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := opened.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func started(t *testing.T, setup cli.Setup) SetupModel {
	t.Helper()
	var model tea.Model = NewSetupModel(setup)
	cmd := model.(SetupModel).Init()
	for depth := 0; cmd != nil && depth < 20; depth++ {
		msg := cmd()
		batch, ok := msg.(tea.BatchMsg)
		if !ok {
			model, cmd = model.Update(msg)
			continue
		}
		cmd = nil
		for _, sub := range batch {
			if sub != nil {
				model, _ = model.Update(sub())
			}
		}
	}
	return model.(SetupModel)
}

func setupPress(t *testing.T, m SetupModel, key string) (SetupModel, tea.Cmd) {
	t.Helper()
	updated, cmd := m.Update(keyMsg(key))
	return updated.(SetupModel), cmd
}

func TestTheFormOffersOnlyImplementedDrivers(t *testing.T) {
	setup, _ := newSetup(t)
	m := started(t, setup)
	view := plain(m.content())
	for _, want := range []string{"opendba setup", "database", "PostgreSQL", "continue"} {
		if !strings.Contains(view, want) {
			t.Errorf("the first step must show %q:\n%s", want, view)
		}
	}
	for _, unwanted := range []string{"MySQL", "coming soon", "MariaDB", "SQL Server"} {
		if strings.Contains(view, unwanted) {
			t.Errorf("the list must not promise %q", unwanted)
		}
	}
	if m.driver != "postgres" {
		t.Errorf("driver = %q", m.driver)
	}
}

func TestTheFormStartsWithSafeDefaults(t *testing.T) {
	setup, _ := newSetup(t)
	m := NewSetupModel(setup)
	values := m.snapshot()
	if !values.readOnly {
		t.Error("read only must be preselected")
	}
	if values.host != "localhost" || values.port != "5432" || values.sslmode != "prefer" {
		t.Errorf("defaults = %+v", values)
	}
	if values.driver != "postgres" {
		t.Errorf("driver = %q", values.driver)
	}
}

func TestValidators(t *testing.T) {
	if err := required("needed")(" "); err == nil {
		t.Error("blank input must be rejected")
	}
	if err := required("needed")("value"); err != nil {
		t.Errorf("a value must pass: %v", err)
	}
	for _, bad := range []string{"", "abc", "0", "70000", "-1"} {
		if err := port(bad); err == nil {
			t.Errorf("port(%q) must be rejected", bad)
		}
	}
	if err := port(" 5432 "); err != nil {
		t.Errorf("port() = %v", err)
	}
}

func TestImportingAConnectionStringFillsTheForm(t *testing.T) {
	values := &answers{}
	fill := importable(values)
	if err := fill(""); err != nil {
		t.Fatalf("an empty import is fine: %v", err)
	}
	if err := fill("postgres://patryk:hunter2@db.example.com:6432/app?sslmode=verify-full"); err != nil {
		t.Fatalf("importable: %v", err)
	}
	if values.host != "db.example.com" || values.port != "6432" || values.user != "patryk" {
		t.Fatalf("values = %+v", values)
	}
	if values.database != "app" || values.sslmode != "verify-full" || values.password != "hunter2" {
		t.Fatalf("values = %+v", values)
	}
	for _, bad := range []string{"://nope", "nonsense"} {
		if err := fill(bad); err == nil {
			t.Errorf("importable(%q) must be rejected", bad)
		}
	}
}

func TestConnectionFromTheValues(t *testing.T) {
	values := defaults()
	values.driver = "postgres"
	values.name = " production-eu "
	values.user = "readonly"
	values.database = "app"
	values.password = "hunter2"
	values.color = string(ui.EnvRed)

	connection, password := connectionFrom(config.Connection{}, values, "01J")
	if connection.ID != "01J" || connection.Name != "production-eu" || connection.Driver != "postgres" {
		t.Fatalf("connection = %+v", connection)
	}
	if connection.Host != "localhost" || connection.Port != 5432 || connection.SSLMode != "prefer" {
		t.Fatalf("connection = %+v", connection)
	}
	if connection.Mode != config.ReadOnly || connection.Color != string(ui.EnvRed) {
		t.Fatalf("connection = %+v", connection)
	}
	if string(password) != "hunter2" {
		t.Errorf("password = %q", password)
	}

	values.readOnly = false
	values.driver = "sqlite"
	values.file = " /tmp/app.db "
	connection, password = connectionFrom(config.Connection{}, values, "01K")
	if connection.Mode != config.ReadWrite || connection.File != "/tmp/app.db" {
		t.Fatalf("connection = %+v", connection)
	}
	if password != nil {
		t.Errorf("a sqlite connection has no password: %q", password)
	}
}

func TestTheSnapshotReadsTheForm(t *testing.T) {
	setup, _ := newSetup(t)
	details := detailed(t, setup)
	filled := fill(t, details, "name", "production-eu")
	filled = fill(t, filled, "user", "readonly")

	values := filled.snapshot()
	if values.name != "production-eu" || values.user != "readonly" {
		t.Fatalf("snapshot = %+v", values)
	}
	if values.host != "localhost" || values.driver != "postgres" {
		t.Fatalf("snapshot = %+v", values)
	}

	before := NewSetupModel(setup).snapshot()
	if before.name != "" || before.host != "localhost" {
		t.Fatalf("before the details are open the defaults stand: %+v", before)
	}
}

func TestTheWizardWarnsWhenTheRoleCanWrite(t *testing.T) {
	setup, _ := newSetup(t)
	details := detailed(t, setup)
	details.connection = config.Connection{Name: "production-eu", Mode: config.ReadOnly}
	writable := driver.ServerInfo{Driver: "postgres", Version: "16.3", CanWrite: true}

	reported, cmd := details.tested(testedMsg{saved: true, info: writable})
	if reported.(SetupModel).failure != "" {
		t.Fatalf("failure = %q", reported.(SetupModel).failure)
	}
	done := outcomeOf(t, cmd)
	if done.Connection.Name != "production-eu" || !done.Saved {
		t.Fatalf("done = %+v", done)
	}
	if !strings.Contains(done.Warning(), "read only is enforced by opendba alone") {
		t.Errorf("a writable role must be called out: %q", done.Warning())
	}

	readWrite := details
	readWrite.connection.Mode = config.ReadWrite
	_, cmd = readWrite.tested(testedMsg{saved: true, info: writable})
	if warning := outcomeOf(t, cmd).Warning(); warning != "" {
		t.Errorf("a read write connection has nothing to warn about: %q", warning)
	}

	_, cmd = details.tested(testedMsg{info: writable})
	if warning := outcomeOf(t, cmd).Warning(); warning != "" {
		t.Errorf("an unsaved connection has nothing to warn about: %q", warning)
	}
}

func outcomeOf(t *testing.T, cmd tea.Cmd) SetupDone {
	t.Helper()
	if cmd == nil {
		t.Fatal("a finished wizard must report what it did")
	}
	done, ok := cmd().(SetupDone)
	if !ok {
		t.Fatalf("the wizard reported %T", cmd())
	}
	return done
}

func TestTheWizardShowsProgressWhileConnecting(t *testing.T) {
	setup, _ := newSetup(t)
	m := NewSetupModel(setup)
	m.stage = stageTesting
	view := plain(m.content())
	if !strings.Contains(view, "reaching the server") || !strings.Contains(view, "connecting") {
		t.Errorf("view = %s", view)
	}
	same, cmd := setupPress(t, m, "enter")
	if cmd != nil || same.stage != stageTesting {
		t.Error("keys must be ignored while connecting")
	}
}

func TestTheWizardQuits(t *testing.T) {
	setup, _ := newSetup(t)
	quit, cmd := setupPress(t, NewSetupModel(setup), "ctrl+c")
	if cmd == nil || !quit.quitting {
		t.Error("ctrl+c must leave")
	}

}

func TestTheWizardHandlesTheWindowAndUnknownMessages(t *testing.T) {
	setup, _ := newSetup(t)
	m := NewSetupModel(setup)
	sized, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	if sized.(SetupModel).width != 120 || sized.(SetupModel).height != 40 {
		t.Errorf("size = %dx%d", sized.(SetupModel).width, sized.(SetupModel).height)
	}
	if _, cmd := m.Update(struct{}{}); cmd != nil {
		t.Log("an unknown message reached the form, which is fine")
	}
}

func TestTheWizardRunsAsAProgram(t *testing.T) {
	setup, _ := newSetup(t)
	outcome, err := runSetup(setup, tea.WithInput(strings.NewReader("\x03")), tea.WithOutput(io.Discard))
	if err != nil {
		t.Fatalf("runSetup: %v", err)
	}
	if outcome.Saved {
		t.Errorf("a wizard that was cut short saved nothing: %+v", outcome)
	}
}

func TestPromptNeedsATerminal(t *testing.T) {
	if _, err := Prompt("password: "); err == nil {
		t.Error("reading a password without a terminal must fail")
	}
	if _, err := PassphrasePrompt(); err == nil {
		t.Error("the vault passphrase needs a terminal too")
	}
}

func TestMainReportsAnUnusableConfiguration(t *testing.T) {
	blocked := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocked, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", blocked)
	t.Setenv("XDG_STATE_HOME", blocked)
	if code := Main("test", []string{"version"}); code != cli.ExitFailure {
		t.Fatalf("exit = %d, want failure", code)
	}
}

func TestMainRunsACommand(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	if code := Main("9.9.9", []string{"version"}); code != cli.ExitOK {
		t.Fatalf("exit = %d", code)
	}
}

func TestUnknownErrorsKeepTheWizardUsable(t *testing.T) {
	setup, _ := newSetup(t)
	m := NewSetupModel(setup)
	finished, _ := m.tested(testedMsg{err: errors.New("connection refused")})
	if finished.(SetupModel).failure != "connection refused" {
		t.Errorf("failure = %q", finished.(SetupModel).failure)
	}
}

func TestRunSetupIsAThinWrapper(t *testing.T) {
	setup, _ := newSetup(t)
	if _, err := RunSetup(setup); err == nil {
		t.Log("the wizard started, which means this machine has a terminal")
	}
}

func TestTabMovesThroughEveryField(t *testing.T) {
	setup, _ := newSetup(t)
	m := NewSetupModel(setup)
	if cmd := m.Init(); cmd != nil {
		cmd()
	}
	chosen, _ := setupPress(t, m, "tab")
	details, _ := setupPress(t, chosen, "enter")
	if details.stage != stageDetails {
		t.Fatalf("stage = %v", details.stage)
	}

	seen := map[string]bool{}
	current := details
	for i := 0; i < len(current.form.fields); i++ {
		seen[current.form.current().key] = true
		current, _ = setupPress(t, current, "tab")
	}
	for _, key := range []string{"name", "host", "port", "user", "password", "database", "ssl", "import", "access", "color", "test", "save"} {
		if !seen[key] {
			t.Errorf("tab never reached %q", key)
		}
	}
	if current.form.current().key != details.form.current().key {
		t.Error("tab must wrap around to the first field")
	}
}

func TestShiftTabAndArrowsMoveTheOtherWay(t *testing.T) {
	setup, _ := newSetup(t)
	m := NewSetupModel(setup)
	if cmd := m.Init(); cmd != nil {
		cmd()
	}
	chosen, _ := setupPress(t, m, "tab")
	details, _ := setupPress(t, chosen, "enter")

	back, _ := setupPress(t, details, "shift+tab")
	if back.form.current().key != "save" {
		t.Errorf("shift+tab from the first field must wrap to the last, got %q", back.form.current().key)
	}
	down, _ := setupPress(t, details, "down")
	if down.form.current().key != "host" {
		t.Errorf("down = %q", down.form.current().key)
	}
	up, _ := setupPress(t, down, "up")
	if up.form.current().key != "name" {
		t.Errorf("up = %q", up.form.current().key)
	}
}

func TestFormEditsText(t *testing.T) {
	theme := ui.Default()
	built, cmd := newForm(
		textField(theme, "host", "host", "localhost", "the server"),
		secretField(theme, "password", "password", "", "kept in your keychain"),
		actionField("save", "save", "keep it"),
	)
	if cmd == nil {
		t.Fatal("the first field must take focus")
	}
	if built.current().key != "host" {
		t.Fatalf("focus = %q", built.current().key)
	}

	typed := built
	for _, key := range []string{"backspace", "backspace", "backspace"} {
		typed, _, _ = typed.update(keyMsg(key))
	}
	if typed.value("host") != "localh" {
		t.Fatalf("host = %q", typed.value("host"))
	}
	for _, key := range strings.Split("ost", "") {
		typed, _, _ = typed.update(keyMsg(key))
	}
	if typed.value("host") != "localhost" {
		t.Fatalf("host = %q", typed.value("host"))
	}
}

func TestFormMasksSecrets(t *testing.T) {
	theme := ui.Default()
	built, _ := newForm(
		secretField(theme, "password", "password", "", ""),
		actionField("save", "save", ""),
	)
	typed := built
	for _, key := range strings.Split("hunter2", "") {
		typed, _, _ = typed.update(keyMsg(key))
	}
	if string(typed.secret("password")) != "hunter2" {
		t.Fatalf("secret = %q", typed.secret("password"))
	}
	moved, _, _ := typed.update(keyMsg("tab"))
	view := plain(moved.view(theme, 80))
	if strings.Contains(view, "hunter2") {
		t.Fatalf("the password must never be shown:\n%s", view)
	}
	if !strings.Contains(view, "•••••••") {
		t.Errorf("the password must be masked:\n%s", view)
	}
}

func TestFormCyclesChoicesAndToggles(t *testing.T) {
	built, _ := newForm(
		choiceField("ssl", "ssl", []string{"prefer", "require", "disable"}, "prefer", ""),
		toggleField("access", "access", []string{"READ ONLY", "READ / WRITE"}, true, ""),
	)
	next, _, _ := built.update(keyMsg("right"))
	if next.value("ssl") != "require" {
		t.Fatalf("ssl = %q", next.value("ssl"))
	}
	back, _, _ := next.update(keyMsg("left"))
	if back.value("ssl") != "prefer" {
		t.Fatalf("ssl = %q", back.value("ssl"))
	}
	wrapped, _, _ := back.update(keyMsg("left"))
	if wrapped.value("ssl") != "disable" {
		t.Fatalf("choices must wrap, got %q", wrapped.value("ssl"))
	}

	onToggle, _, _ := built.update(keyMsg("tab"))
	flipped, _, _ := onToggle.update(keyMsg("space"))
	if flipped.enabled("access") {
		t.Error("space must flip the toggle")
	}
	if flipped.value("access") != "READ / WRITE" {
		t.Errorf("access = %q", flipped.value("access"))
	}
}

func TestFormReportsActions(t *testing.T) {
	theme := ui.Default()
	built, _ := newForm(
		textField(theme, "name", "name", "", ""),
		actionField("test", "test", ""),
	)
	onAction, _, _ := built.update(keyMsg("tab"))
	_, action, _ := onAction.update(keyMsg("enter"))
	if action != "test" {
		t.Fatalf("action = %q", action)
	}
	_, none, _ := built.update(keyMsg("enter"))
	if none != "" {
		t.Errorf("enter on a text field only moves on, got %q", none)
	}
}

func TestFormRendering(t *testing.T) {
	theme := ui.Default()
	built, _ := newForm(
		textField(theme, "host", "host", "localhost", "the server to connect to"),
		textField(theme, "user", "user", "", ""),
		choiceField("color", "environment", colorNames(), "green", ""),
		actionField("save", "save", ""),
	)
	view := plain(built.view(theme, 80))
	for _, want := range []string{"host", "localhost", "user", "environment", "green", "[ save ]", "the server to connect to"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q:\n%s", want, view)
		}
	}
	if !strings.Contains(view, "—") {
		t.Error("an empty field must be marked")
	}
	if !strings.Contains(view, "▌") {
		t.Error("the focused field must be marked")
	}
	if strings.Contains(view, "save          [ save ]") {
		t.Error("an action must not repeat its label")
	}
}

func TestFormShowsShortChoiceListsInFull(t *testing.T) {
	theme := ui.Default()
	built, _ := newForm(
		toggleField("access", "access", []string{"READ ONLY", "READ / WRITE"}, true, ""),
		choiceField("ssl", "ssl", []string{"prefer", "require", "verify-ca", "verify-full", "disable"}, "prefer", ""),
	)
	view := plain(built.view(theme, 80))
	if !strings.Contains(view, "READ ONLY") || !strings.Contains(view, "READ / WRITE") {
		t.Errorf("a short list must show every option:\n%s", view)
	}
	if strings.Contains(view, "verify-full") {
		t.Errorf("a long list must show only the choice:\n%s", view)
	}
}

func TestFormIgnoresKeysThatDoNotApply(t *testing.T) {
	built, _ := newForm(choiceField("ssl", "ssl", []string{"a", "b"}, "a", ""))
	same, _, _ := built.update(keyMsg("x"))
	if same.value("ssl") != "a" {
		t.Error("typing into a choice must do nothing")
	}
	empty := form{}
	moved, _ := empty.move(1)
	if len(moved.fields) != 0 {
		t.Error("an empty form has nowhere to move")
	}
	if empty.value("missing") != "" || empty.secret("missing") != nil || empty.enabled("missing") {
		t.Error("an empty form has no values")
	}
	if empty.blink() != nil {
		t.Error("an empty form has nothing to blink")
	}
	if empty.current().key != "" {
		t.Error("an empty form has no current field")
	}
}

func detailed(t *testing.T, setup cli.Setup) SetupModel {
	t.Helper()
	m := NewSetupModel(setup)
	if cmd := m.Init(); cmd != nil {
		cmd()
	}
	chosen, _ := setupPress(t, m, "tab")
	details, _ := setupPress(t, chosen, "enter")
	if details.stage != stageDetails {
		t.Fatalf("stage = %v", details.stage)
	}
	return details
}

func fill(t *testing.T, m SetupModel, key, value string) SetupModel {
	t.Helper()
	for m.form.current().key != key {
		m, _ = setupPress(t, m, "tab")
	}
	for _, letter := range strings.Split(value, "") {
		m, _ = setupPress(t, m, letter)
	}
	return m
}

func act(t *testing.T, m SetupModel, key string) (SetupModel, tea.Cmd) {
	t.Helper()
	for m.form.current().key != key {
		m, _ = setupPress(t, m, "tab")
	}
	return setupPress(t, m, "enter")
}

func TestEscapeGoesBackToTheDriverChoice(t *testing.T) {
	setup, _ := newSetup(t)
	details := detailed(t, setup)
	back, _ := setupPress(t, details, "esc")
	if back.stage != stageDriver {
		t.Fatalf("stage = %v", back.stage)
	}
	if back.form.current().key != "driver" {
		t.Errorf("focus = %q", back.form.current().key)
	}
	if back.failure != "" {
		t.Errorf("going back must clear the failure, got %q", back.failure)
	}
}

func TestSavingAConnectionEndToEnd(t *testing.T) {
	setup, store := newSetup(t)
	m := NewSetupModel(setup)
	if cmd := m.Init(); cmd != nil {
		cmd()
	}
	chosen, _ := setupPress(t, m, "right")
	if chosen.form.value("driver") != "SQLite" {
		t.Fatalf("driver = %q", chosen.form.value("driver"))
	}
	details, _ := act(t, chosen, "next")
	if details.snapshot().driver != "sqlite" {
		t.Fatalf("driver = %q", details.snapshot().driver)
	}

	details = fill(t, details, "file", seedFile(t))
	details = fill(t, details, "name", "local")
	running, cmd := act(t, details, "save")
	if cmd == nil || running.stage != stageTesting {
		t.Fatalf("stage = %v", running.stage)
	}
	_, reported := running.Update(cmd())
	done := outcomeOf(t, reported)
	if !done.Saved || done.Connection.Name != "local" {
		t.Fatalf("done = %+v", done)
	}
	profiles, err := store.LoadProfiles()
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles.Connections) != 1 || profiles.Connections[0].Name != "local" {
		t.Fatalf("profiles = %+v", profiles)
	}
}

func TestTestingWithoutSavingKeepsTheStoreEmpty(t *testing.T) {
	setup, store := newSetup(t)
	m := NewSetupModel(setup)
	if cmd := m.Init(); cmd != nil {
		cmd()
	}
	chosen, _ := setupPress(t, m, "right")
	details, _ := act(t, chosen, "next")
	details = fill(t, details, "file", seedFile(t))
	details = fill(t, details, "name", "local")

	running, cmd := act(t, details, "test")
	if cmd == nil {
		t.Fatal("test must connect")
	}
	_, reported := running.Update(cmd())
	if done := outcomeOf(t, reported); done.Saved {
		t.Fatalf("nothing was asked to be saved: %+v", done)
	}
	profiles, err := store.LoadProfiles()
	if err != nil {
		t.Fatal(err)
	}
	if !profiles.IsEmpty() {
		t.Fatalf("nothing must be saved: %+v", profiles)
	}
}

func TestTheWizardRefusesIncompleteConnections(t *testing.T) {
	setup, _ := newSetup(t)
	details := detailed(t, setup)

	blocked, cmd := act(t, details, "save")
	if cmd != nil || blocked.stage != stageDetails {
		t.Fatalf("stage = %v", blocked.stage)
	}
	if !strings.Contains(blocked.failure, "name") {
		t.Errorf("failure = %q", blocked.failure)
	}

	named := fill(t, blocked, "name", "local")
	withoutHost := named
	for withoutHost.form.current().key != "host" {
		withoutHost, _ = setupPress(t, withoutHost, "tab")
	}
	for i := 0; i < len("localhost"); i++ {
		withoutHost, _ = setupPress(t, withoutHost, "backspace")
	}
	failed, _ := act(t, withoutHost, "save")
	if !strings.Contains(failed.failure, "host") {
		t.Errorf("failure = %q", failed.failure)
	}

	badPort := fill(t, named, "port", "abc")
	failed, _ = act(t, badPort, "save")
	if !strings.Contains(failed.failure, "port") {
		t.Errorf("failure = %q", failed.failure)
	}
}

func TestPastingAUrlFillsTheFields(t *testing.T) {
	setup, _ := newSetup(t)
	details := detailed(t, setup)
	details = fill(t, details, "import", "postgres://patryk:hunter2@db.example.com:6432/app?sslmode=verify-full")
	filled, _ := act(t, details, "save")

	if filled.snapshot().host != "db.example.com" || filled.snapshot().port != "6432" {
		t.Fatalf("values = %+v", filled.snapshot())
	}
	if filled.snapshot().user != "patryk" || filled.snapshot().password != "hunter2" {
		t.Fatalf("values = %+v", filled.snapshot())
	}
	if filled.form.value("host") != "db.example.com" || filled.form.value("import") != "" {
		t.Fatalf("the form must show what was imported: %q %q", filled.form.value("host"), filled.form.value("import"))
	}
}

func TestPastingABrokenUrlIsReported(t *testing.T) {
	setup, _ := newSetup(t)
	details := detailed(t, setup)
	details = fill(t, details, "import", "nonsense")
	failed, cmd := act(t, details, "save")
	if cmd != nil {
		t.Fatal("a broken url must not connect")
	}
	if failed.failure == "" {
		t.Error("the failure must be shown")
	}
}

func TestASqliteConnectionNeedsAFile(t *testing.T) {
	setup, _ := newSetup(t)
	m := NewSetupModel(setup)
	if cmd := m.Init(); cmd != nil {
		cmd()
	}
	chosen, _ := setupPress(t, m, "right")
	details, _ := act(t, chosen, "next")
	details = fill(t, details, "name", "local")
	failed, cmd := act(t, details, "save")
	if cmd != nil || !strings.Contains(failed.failure, "file") {
		t.Fatalf("failure = %q", failed.failure)
	}
}

func TestNameComesFirstAndRequiredFieldsAreMarked(t *testing.T) {
	setup, _ := newSetup(t)
	details := detailed(t, setup)

	if details.form.fields[0].key != "name" {
		t.Fatalf("the first field is %q", details.form.fields[0].key)
	}
	required := map[string]bool{}
	for _, entry := range details.form.fields {
		if entry.required {
			required[entry.key] = true
		}
	}
	for _, key := range []string{"name", "host", "port"} {
		if !required[key] {
			t.Errorf("%q must be marked as required", key)
		}
	}
	for _, key := range []string{"database", "user", "password", "ssl", "import", "access", "color"} {
		if required[key] {
			t.Errorf("%q is not required and must not be marked", key)
		}
	}
	view := plain(details.form.view(details.theme, 80))
	if !strings.Contains(view, "name        *") {
		t.Errorf("required fields must carry a mark:\n%s", view)
	}
}

func TestSqliteRequiresItsFile(t *testing.T) {
	setup, _ := newSetup(t)
	m := NewSetupModel(setup)
	if cmd := m.Init(); cmd != nil {
		cmd()
	}
	chosen, _ := setupPress(t, m, "right")
	details, _ := act(t, chosen, "next")
	for _, entry := range details.form.fields {
		if entry.key == "file" && !entry.required {
			t.Error("a sqlite connection cannot be saved without a file")
		}
	}
}

func TestEmptyRequiredFieldsAreNamedInTheFailure(t *testing.T) {
	setup, _ := newSetup(t)
	details := detailed(t, setup)
	blocked, cmd := act(t, details, "save")
	if cmd != nil {
		t.Fatal("nothing must be sent while required fields are empty")
	}
	for _, want := range []string{"name", "cannot be empty"} {
		if !strings.Contains(blocked.failure, want) {
			t.Errorf("failure %q must mention %q", blocked.failure, want)
		}
	}
	for _, filled := range []string{"host", "port"} {
		if strings.Contains(blocked.failure, filled) {
			t.Errorf("%s has a default and must not be reported: %q", filled, blocked.failure)
		}
	}
	if strings.Contains(blocked.failure, "database") {
		t.Errorf("the server picks a database when none is given: %q", blocked.failure)
	}
}

func TestPastingIntoAFieldRaisesAToast(t *testing.T) {
	setup, _ := newSetup(t)
	details := detailed(t, setup)

	pasted, cmd := details.Update(tea.PasteMsg{Content: "production-eu"})
	model := pasted.(SetupModel)
	if model.form.value("name") != "production-eu" {
		t.Fatalf("name = %q", model.form.value("name"))
	}
	if !strings.Contains(model.text(), "13 characters went into name") {
		t.Fatalf("toast = %q", model.text())
	}
	if !strings.Contains(plain(model.content()), "13 characters went into") {
		t.Error("the toast must be shown")
	}
	if cmd == nil {
		t.Fatal("the toast must be scheduled to disappear")
	}

	expired, _ := model.Update(toastMsg{sequence: model.sequence})
	if expired.(SetupModel).text() != "" {
		t.Error("the toast must fade")
	}
	stale, _ := model.Update(toastMsg{sequence: model.sequence - 1})
	if stale.(SetupModel).text() == "" {
		t.Error("an older toast must not clear a newer one")
	}
}

func TestPastingASingleCharacterReadsWell(t *testing.T) {
	setup, _ := newSetup(t)
	details := detailed(t, setup)
	pasted, _ := details.Update(tea.PasteMsg{Content: "x"})
	if !strings.Contains(pasted.(SetupModel).text(), "1 character went into") {
		t.Errorf("toast = %q", pasted.(SetupModel).text())
	}
}

func TestPastingWhereNothingCanBeTypedSaysSo(t *testing.T) {
	setup, _ := newSetup(t)
	details := detailed(t, setup)
	onChoice := details
	for onChoice.form.current().key != "ssl" {
		onChoice, _ = setupPress(t, onChoice, "tab")
	}
	pasted, _ := onChoice.Update(tea.PasteMsg{Content: "require"})
	model := pasted.(SetupModel)
	if model.form.value("ssl") != "prefer" {
		t.Errorf("a choice must not take pasted text: %q", model.form.value("ssl"))
	}
	if !strings.Contains(model.text(), "there was nothing to paste") {
		t.Errorf("toast = %q", model.text())
	}
}

func TestPastingIsIgnoredWhileTheWizardConnects(t *testing.T) {
	setup, _ := newSetup(t)
	m := NewSetupModel(setup)
	m.stage = stageTesting
	pasted, cmd := m.Update(tea.PasteMsg{Content: "x"})
	if cmd != nil || pasted.(SetupModel).text() != "" {
		t.Error("a connecting wizard has nowhere to paste")
	}
}

func TestPastingAUrlIntoTheUrlFieldStillFillsTheForm(t *testing.T) {
	setup, _ := newSetup(t)
	details := detailed(t, setup)
	onImport := details
	for onImport.form.current().key != "import" {
		onImport, _ = setupPress(t, onImport, "tab")
	}
	pasted, _ := onImport.Update(tea.PasteMsg{Content: "postgres://patryk:hunter2@db.example.com:6432/app"})
	model := pasted.(SetupModel)
	named := fill(t, model, "name", "imported")
	filled, _ := act(t, named, "save")
	if filled.snapshot().host != "db.example.com" || filled.snapshot().password != "hunter2" {
		t.Fatalf("values = %+v", filled.snapshot())
	}
}

func TestOnlyMarkedFieldsAreEnforced(t *testing.T) {
	setup, _ := newSetup(t)
	details := detailed(t, setup)
	named := fill(t, details, "name", "local")

	optional := map[string]string{"database": "", "user": "", "password": ""}
	for key, want := range optional {
		if named.form.value(key) != want {
			t.Fatalf("%s = %q, the fixture expects it empty", key, named.form.value(key))
		}
	}

	attempted, cmd := act(t, named, "test")
	if cmd == nil {
		t.Fatalf("every marked field is filled, so the connection must be attempted, failure = %q", attempted.failure)
	}
	if attempted.failure != "" {
		t.Errorf("failure = %q", attempted.failure)
	}
	if attempted.stage != stageTesting {
		t.Errorf("stage = %v", attempted.stage)
	}
}

func TestEveryMarkedFieldIsReportedAtOnce(t *testing.T) {
	setup, _ := newSetup(t)
	details := detailed(t, setup)

	empty := details
	for _, key := range []string{"host", "port"} {
		for empty.form.current().key != key {
			empty, _ = setupPress(t, empty, "tab")
		}
		for i := 0; i < 12; i++ {
			empty, _ = setupPress(t, empty, "backspace")
		}
	}
	blocked, cmd := act(t, empty, "save")
	if cmd != nil {
		t.Fatal("nothing must be sent")
	}
	for _, want := range []string{"name", "host", "port"} {
		if !strings.Contains(blocked.failure, want) {
			t.Errorf("failure %q must name %q", blocked.failure, want)
		}
	}
}

// Editing starts from what is saved. The id, the secret reference and the
// schema filter are on the profile and on no field, so a fresh struct would
// quietly drop all three.
func TestEditingKeepsWhatIsNotOnTheForm(t *testing.T) {
	saved := config.Connection{
		ID: "kept", Name: "localhost", Driver: "postgres", Host: "db.internal", Port: 6432,
		User: "reader", Database: "app", SSLMode: "require", Mode: config.ReadWrite,
		Color: "blue", Secret: "keyring:kept", DefaultSchema: "catalog",
		Schemas: []string{"catalog", "iam"}, Options: "application_name=x",
	}
	values := answersFrom(saved)
	if values.host != "db.internal" || values.port != "6432" || values.sslmode != "require" {
		t.Fatalf("the form must be seeded from the profile: %+v", values)
	}
	if values.readOnly {
		t.Error("including the access mode")
	}
	if values.password != "" {
		t.Error("a secret is written and never read back, so the field starts empty")
	}

	edited, password := connectionFrom(saved, values, "a new id")
	if edited.ID != "kept" {
		t.Errorf("id = %q, want the one that was saved", edited.ID)
	}
	if edited.Secret != "keyring:kept" {
		t.Errorf("secret = %q, want the reference to survive", edited.Secret)
	}
	if edited.DefaultSchema != "catalog" || len(edited.Schemas) != 2 {
		t.Errorf("the schema filter must survive: %+v", edited)
	}
	if edited.Options != "application_name=x" {
		t.Errorf("options = %q", edited.Options)
	}
	if len(password) != 0 {
		t.Error("an untouched password field means keep the one that is stored")
	}

	values.password = "typed again"
	replaced, password := connectionFrom(saved, values, "a new id")
	if string(password) != "typed again" || replaced.ID != "kept" {
		t.Error("a retyped password replaces the secret under the same id")
	}
}

func TestANewConnectionGetsANewID(t *testing.T) {
	values := defaults()
	values.driver, values.name = "postgres", "fresh"
	made, _ := connectionFrom(config.Connection{}, values, "minted")
	if made.ID != "minted" {
		t.Errorf("id = %q", made.ID)
	}
}
