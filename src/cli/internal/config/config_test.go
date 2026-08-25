package config

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func envFrom(values map[string]string) Environment {
	return func(key string) string { return values[key] }
}

func TestPathsFromXDG(t *testing.T) {
	paths, err := PathsFor(envFrom(map[string]string{
		"XDG_CONFIG_HOME": "/x/config",
		"XDG_STATE_HOME":  "/x/state",
		"XDG_DATA_HOME":   "/x/data",
	}))
	if err != nil {
		t.Fatalf("PathsFor: %v", err)
	}
	if paths.Config != filepath.Join("/x/config", "opendba") {
		t.Errorf("Config = %q", paths.Config)
	}
	if paths.State != filepath.Join("/x/state", "opendba") {
		t.Errorf("State = %q", paths.State)
	}
	if paths.Data != filepath.Join("/x/data", "opendba") {
		t.Errorf("Data = %q", paths.Data)
	}
}

func TestPathsFallBackToHome(t *testing.T) {
	paths, err := PathsFor(envFrom(map[string]string{"HOME": "/home/db"}))
	if err != nil {
		t.Fatalf("PathsFor: %v", err)
	}
	if paths.Config != filepath.Join("/home/db", ".config", "opendba") {
		t.Errorf("Config = %q", paths.Config)
	}
	if paths.State != filepath.Join("/home/db", ".local", "state", "opendba") {
		t.Errorf("State = %q", paths.State)
	}
}

func TestPathsFallBackToUserProfile(t *testing.T) {
	paths, err := PathsFor(envFrom(map[string]string{"USERPROFILE": `C:\Users\db`}))
	if err != nil {
		t.Fatalf("PathsFor: %v", err)
	}
	if !strings.Contains(paths.Config, "opendba") {
		t.Errorf("Config = %q", paths.Config)
	}
}

func TestPathsRequireAHome(t *testing.T) {
	if _, err := PathsFor(envFrom(nil)); err == nil {
		t.Fatal("want error without HOME")
	}
	if _, err := PathsFor(envFrom(map[string]string{"XDG_CONFIG_HOME": "/x"})); err == nil {
		t.Fatal("want error without a state directory")
	}
}

func TestFileNames(t *testing.T) {
	paths := Paths{Config: "/c", State: "/s", Data: "/d"}
	cases := map[string]string{
		paths.ProfilesFile(): "profiles.toml",
		paths.SettingsFile(): "settings.toml",
		paths.VaultFile():    "secrets.age",
		paths.HistoryFile():  "history.db",
		paths.SQLDir():       "sql",
	}
	for path, want := range cases {
		if filepath.Base(path) != want {
			t.Errorf("%q does not end in %q", path, want)
		}
	}
}

func TestEnsureCreatesPrivateDirectories(t *testing.T) {
	root := t.TempDir()
	paths := Paths{
		Config: filepath.Join(root, "config"),
		State:  filepath.Join(root, "state"),
		Data:   filepath.Join(root, "data"),
	}
	if err := paths.Ensure(); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	for _, dir := range []string{paths.Config, paths.State} {
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatalf("stat %s: %v", dir, err)
		}
		if runtime.GOOS != "windows" && info.Mode().Perm() != 0o700 {
			t.Errorf("%s has mode %04o, want 0700", dir, info.Mode().Perm())
		}
	}
}

func TestEnsureTightensLoosePermissions(t *testing.T) {
	root := t.TempDir()
	loose := filepath.Join(root, "config")
	if err := os.MkdirAll(loose, 0o755); err != nil {
		t.Fatal(err)
	}
	paths := Paths{Config: loose, State: filepath.Join(root, "state"), Data: filepath.Join(root, "data")}
	if err := paths.Ensure(); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	info, err := os.Stat(loose)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o700 {
		t.Errorf("mode = %04o, want 0700", info.Mode().Perm())
	}
}

func TestEnsureFailsOnFileInsteadOfDirectory(t *testing.T) {
	root := t.TempDir()
	blocked := filepath.Join(root, "config")
	if err := os.WriteFile(blocked, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	paths := Paths{Config: blocked, State: filepath.Join(root, "state")}
	if err := paths.Ensure(); err == nil {
		t.Fatal("want error")
	}
}

func newStore(t *testing.T) Store {
	t.Helper()
	root := t.TempDir()
	paths := Paths{
		Config: filepath.Join(root, "config"),
		State:  filepath.Join(root, "state"),
		Data:   filepath.Join(root, "data"),
	}
	if err := paths.Ensure(); err != nil {
		t.Fatal(err)
	}
	return NewStore(paths)
}

func sampleConnection() Connection {
	return Connection{
		ID:       "01J000000000000000000000",
		Name:     "production-eu",
		Driver:   "postgres",
		Host:     "db.example.com",
		Port:     5432,
		Database: "app",
		User:     "readonly",
		SSLMode:  "verify-full",
		Mode:     ReadOnly,
		Color:    "red",
		Secret:   "keyring:opendba/01J000000000000000000000",
	}
}

func TestProfilesRoundTrip(t *testing.T) {
	store := newStore(t)
	profiles := Profiles{Connections: []Connection{sampleConnection()}}
	if err := store.SaveProfiles(profiles); err != nil {
		t.Fatalf("SaveProfiles: %v", err)
	}
	loaded, err := store.LoadProfiles()
	if err != nil {
		t.Fatalf("LoadProfiles: %v", err)
	}
	if len(loaded.Connections) != 1 || !reflect.DeepEqual(loaded.Connections[0], profiles.Connections[0]) {
		t.Fatalf("round trip changed the profile: %+v", loaded)
	}
}

func TestProfilesFileIsPrivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permissions are not enforced on windows")
	}
	store := newStore(t)
	if err := store.SaveProfiles(Profiles{Connections: []Connection{sampleConnection()}}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(store.Paths.ProfilesFile())
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %04o, want 0600", info.Mode().Perm())
	}
}

func TestProfilesFileNeverContainsASecret(t *testing.T) {
	store := newStore(t)
	if err := store.SaveProfiles(Profiles{Connections: []Connection{sampleConnection()}}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(store.Paths.ProfilesFile())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "password") {
		t.Fatalf("profiles file mentions a password:\n%s", data)
	}
}

func TestLoadProfilesRejectsWorldReadableFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permissions are not enforced on windows")
	}
	store := newStore(t)
	if err := store.SaveProfiles(Profiles{Connections: []Connection{sampleConnection()}}); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(store.Paths.ProfilesFile(), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := store.LoadProfiles()
	if err == nil {
		t.Fatal("want error for a world readable profile file")
	}
	if !strings.Contains(err.Error(), "chmod 600") {
		t.Errorf("error should tell the user how to fix it: %v", err)
	}
}

func TestLoadProfilesOnEmptyConfiguration(t *testing.T) {
	store := newStore(t)
	profiles, err := store.LoadProfiles()
	if err != nil {
		t.Fatalf("LoadProfiles: %v", err)
	}
	if !profiles.IsEmpty() {
		t.Fatalf("want no connections, got %+v", profiles)
	}
}

func TestLoadProfilesRejectsBrokenFiles(t *testing.T) {
	store := newStore(t)
	cases := map[string]string{
		"invalid toml":  "connection = [",
		"unknown mode":  "[[connection]]\nid = \"a\"\nname = \"n\"\ndriver = \"postgres\"\nmode = \"write\"\ncolor = \"red\"\n",
		"duplicate ids": "[[connection]]\nid=\"a\"\nname=\"one\"\ndriver=\"postgres\"\nmode=\"readonly\"\ncolor=\"red\"\n[[connection]]\nid=\"a\"\nname=\"two\"\ndriver=\"postgres\"\nmode=\"readonly\"\ncolor=\"red\"\n",
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			if err := os.WriteFile(store.Paths.ProfilesFile(), []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := store.LoadProfiles(); err == nil {
				t.Fatal("want error")
			}
		})
	}
}

func TestSaveProfilesRejectsInvalidData(t *testing.T) {
	store := newStore(t)
	if err := store.SaveProfiles(Profiles{Connections: []Connection{{ID: "a"}}}); err == nil {
		t.Fatal("want validation error")
	}
}

func TestConnectionValidation(t *testing.T) {
	cases := []struct {
		name       string
		connection Connection
		wantErr    bool
	}{
		{"valid", sampleConnection(), false},
		{"no id", Connection{Name: "n", Driver: "postgres", Mode: ReadOnly}, true},
		{"no name", Connection{ID: "a", Driver: "postgres", Mode: ReadOnly}, true},
		{"no driver", Connection{ID: "a", Name: "n", Mode: ReadOnly}, true},
		{"bad mode", Connection{ID: "a", Name: "n", Driver: "postgres", Mode: "nope"}, true},
		{"password in options", Connection{ID: "a", Name: "n", Driver: "postgres", Mode: ReadOnly, Options: "password=hunter2"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.connection.Validate()
			if (err != nil) != c.wantErr {
				t.Fatalf("Validate() = %v, wantErr %v", err, c.wantErr)
			}
		})
	}
}

func TestProfilesUpsertFindAndRemove(t *testing.T) {
	var profiles Profiles
	first := sampleConnection()
	if err := profiles.Upsert(first); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	updated := first
	updated.Name = "renamed"
	if err := profiles.Upsert(updated); err != nil {
		t.Fatalf("Upsert existing: %v", err)
	}
	if len(profiles.Connections) != 1 || profiles.Connections[0].Name != "renamed" {
		t.Fatalf("connections = %+v", profiles.Connections)
	}
	if _, ok := profiles.Find(first.ID); !ok {
		t.Error("Find must locate the connection")
	}
	if _, ok := profiles.ByName("RENAMED"); !ok {
		t.Error("ByName must be case insensitive")
	}
	if _, ok := profiles.Find("missing"); ok {
		t.Error("Find must report unknown ids")
	}
	if _, ok := profiles.ByName("missing"); ok {
		t.Error("ByName must report unknown names")
	}
	if !profiles.Remove(first.ID) || !profiles.IsEmpty() {
		t.Error("Remove must delete the connection")
	}
	if profiles.Remove(first.ID) {
		t.Error("Remove must report unknown ids")
	}
}

func TestProfilesUpsertRejectsDuplicates(t *testing.T) {
	var profiles Profiles
	first := sampleConnection()
	if err := profiles.Upsert(first); err != nil {
		t.Fatal(err)
	}
	second := first
	second.ID = "01J000000000000000000001"
	if err := profiles.Upsert(second); err == nil {
		t.Fatal("want duplicate name error")
	}
	if err := profiles.Upsert(Connection{ID: "x"}); err == nil {
		t.Fatal("want validation error")
	}
}

func TestAccessModeLabels(t *testing.T) {
	if ReadOnly.Label() != "READ ONLY" || ReadWrite.Label() != "READ / WRITE" {
		t.Error("access mode labels are shown to the user")
	}
	if AccessMode("nope").Valid() {
		t.Error("unknown modes must be invalid")
	}
	if AccessMode("nope").Label() != "READ ONLY" {
		t.Error("unknown modes must fall back to the safe label")
	}
}

func TestSettingsRoundTripAndDefaults(t *testing.T) {
	store := newStore(t)
	settings, err := store.LoadSettings()
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if settings.Safety.DefaultMode != ReadOnly {
		t.Error("read only must be the default access mode")
	}
	settings.Appearance.Accent = "purple"
	if err := store.SaveSettings(settings); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
	loaded, err := store.LoadSettings()
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if !reflect.DeepEqual(loaded, settings) {
		t.Fatalf("round trip changed the settings:\n%+v\n%+v", loaded, settings)
	}
}

func TestSettingsFillInMissingValues(t *testing.T) {
	store := newStore(t)
	if err := os.WriteFile(store.Paths.SettingsFile(), []byte("[appearance]\ntheme = \"light\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	settings, err := store.LoadSettings()
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	defaults := DefaultSettings()
	if settings.Appearance.Theme != "light" {
		t.Error("explicit values must survive")
	}
	if settings.Appearance.Accent != defaults.Appearance.Accent ||
		settings.Safety.DefaultMode != defaults.Safety.DefaultMode ||
		settings.Safety.QueryTimeout != defaults.Safety.QueryTimeout ||
		settings.Safety.LockTimeout != defaults.Safety.LockTimeout ||
		settings.Safety.RowLimit != defaults.Safety.RowLimit ||
		settings.AI.Provider != defaults.AI.Provider {
		t.Fatalf("defaults were not applied: %+v", settings)
	}
}

func TestSettingsValidation(t *testing.T) {
	cases := map[string]Settings{
		"bad mode":       {Safety: SafetySettings{DefaultMode: "nope", RowLimit: 10}},
		"zero row limit": {Safety: SafetySettings{DefaultMode: ReadOnly, RowLimit: 0}},
		"negative limit": {Safety: SafetySettings{DefaultMode: ReadOnly, RowLimit: 10}, History: HistorySettings{Limit: -1}},
	}
	for name, settings := range cases {
		t.Run(name, func(t *testing.T) {
			if err := settings.Validate(); err == nil {
				t.Fatal("want validation error")
			}
		})
	}
	store := newStore(t)
	if err := store.SaveSettings(Settings{}); err == nil {
		t.Fatal("SaveSettings must validate")
	}
}

func TestLoadSettingsRejectsBrokenFile(t *testing.T) {
	store := newStore(t)
	if err := os.WriteFile(store.Paths.SettingsFile(), []byte("["), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadSettings(); err == nil {
		t.Fatal("want parse error")
	}
	if err := os.WriteFile(store.Paths.SettingsFile(), []byte("[safety]\nrow_limit = -5\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadSettings(); err == nil {
		t.Fatal("want validation error")
	}
}

func TestLoadSettingsRejectsWorldReadableFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permissions are not enforced on windows")
	}
	store := newStore(t)
	if err := store.SaveSettings(DefaultSettings()); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(store.Paths.SettingsFile(), 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadSettings(); err == nil {
		t.Fatal("want permission error")
	}
}

func TestWriteSecureFileFailsOnUnwritableDirectory(t *testing.T) {
	blocked := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocked, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeSecureFile(filepath.Join(blocked, "nested", "profiles.toml"), []byte("x")); err == nil {
		t.Fatal("want error")
	}
}

func TestOpenUsesTheEnvironment(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	store, err := Open()
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := os.Stat(store.Paths.Config); err != nil {
		t.Fatalf("configuration directory was not created: %v", err)
	}
}

func TestOpenFailsWithoutAHome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	if _, err := Open(); err == nil {
		t.Fatal("want error")
	}
}

func TestEncodeRejectsUnsupportedValues(t *testing.T) {
	if _, err := encodeTOML(func() {}); err == nil {
		t.Fatal("want encoding error")
	}
}

func TestWriteSecureFileFailsOnReadOnlyDirectory(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("directory permissions are not enforced for this user")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	if err := writeSecureFile(filepath.Join(dir, "profiles.toml"), []byte("x")); err == nil {
		t.Fatal("want write error")
	}
}

func TestWriteSecureFileFailsWhenTargetIsADirectory(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "profiles.toml")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "keep"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeSecureFile(target, []byte("x")); err == nil {
		t.Fatal("want rename error")
	}
}

func TestReadSecureFileReportsMissingFiles(t *testing.T) {
	if _, err := readSecureFile(filepath.Join(t.TempDir(), "missing.toml")); err == nil {
		t.Fatal("want error")
	}
}

func TestEnforceDirModeReportsMissingDirectory(t *testing.T) {
	if err := enforceDirMode(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("want error")
	}
}

func TestOpenFailsWhenTheDirectoryCannotBeCreated(t *testing.T) {
	blocked := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocked, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", blocked)
	t.Setenv("XDG_STATE_HOME", blocked)
	if _, err := Open(); err == nil {
		t.Fatal("want an error when the configuration directory cannot be created")
	}
}

func TestSavingIntoAReadOnlyDirectoryFails(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("directory permissions are not enforced for this user")
	}
	store := newStore(t)
	if err := os.Chmod(store.Paths.Config, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(store.Paths.Config, 0o700) })

	if err := store.SaveProfiles(Profiles{Connections: []Connection{sampleConnection()}}); err == nil {
		t.Error("saving profiles must fail")
	}
	if err := store.SaveSettings(DefaultSettings()); err == nil {
		t.Error("saving settings must fail")
	}
}

func TestReadSecureFileRejectsADirectory(t *testing.T) {
	dir := t.TempDir()
	if _, err := readSecureFile(dir); err == nil {
		t.Fatal("want an error when the path is a directory")
	}
}

func TestTheBarStyleIsASetting(t *testing.T) {
	store := newStore(t)
	settings := DefaultSettings()
	if settings.Appearance.Bar == "" {
		t.Fatal("a fresh configuration must name a bar style")
	}
	settings.Appearance.Bar = "shade"
	if err := store.SaveSettings(settings); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
	loaded, err := store.LoadSettings()
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if loaded.Appearance.Bar != "shade" {
		t.Errorf("bar = %q", loaded.Appearance.Bar)
	}
}

// The mouse is taken unless the settings say otherwise, and "off" is the only
// answer that gives it back.
func TestMouseIsWantedUnlessItIsTurnedOff(t *testing.T) {
	for _, want := range []struct {
		name    string
		setting string
		wanted  bool
	}{
		{"nothing said", "", true},
		{"on", MouseOn, true},
		{"off", MouseOff, false},
		{"something else", "yes", true},
	} {
		t.Run(want.name, func(t *testing.T) {
			appearance := AppearanceSettings{Mouse: want.setting}
			if appearance.MouseWanted() != want.wanted {
				t.Errorf("MouseWanted = %v, want %v", appearance.MouseWanted(), want.wanted)
			}
		})
	}
	if DefaultSettings().Appearance.Mouse != MouseOn {
		t.Error("the default is to take the mouse")
	}
}

// A workspace root is somewhere a file can be found again tomorrow, which a
// path from wherever the program happened to start is not.
func TestARelativeWorkspaceRootIsRefused(t *testing.T) {
	for _, want := range []struct {
		name   string
		root   string
		refuse bool
	}{
		{"empty means the data directory", "", false},
		{"a full path", fullPath("srv", "sql"), false},
		{"a relative path", filepath.Join("sql", "here"), true},
		{"a bare name", "sql", true},
	} {
		t.Run(want.name, func(t *testing.T) {
			settings := DefaultSettings()
			settings.Workspace.Root = want.root
			err := settings.Validate()
			if want.refuse != (err != nil) {
				t.Errorf("Validate = %v, want a refusal: %v", err, want.refuse)
			}
		})
	}
}

func TestTheWorkspaceRootSurvivesTheFile(t *testing.T) {
	root := t.TempDir()
	paths := Paths{Config: filepath.Join(root, "config"), State: filepath.Join(root, "state")}
	if err := paths.Ensure(); err != nil {
		t.Fatal(err)
	}
	store := NewStore(paths)
	settings := DefaultSettings()
	settings.Workspace.Root = filepath.Join(root, "statements")
	if err := store.SaveSettings(settings); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
	held, err := store.LoadSettings()
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if held.Workspace.Root != settings.Workspace.Root {
		t.Errorf("root = %q, want %q", held.Workspace.Root, settings.Workspace.Root)
	}
}

// fullPath builds a path the system running the tests calls a full one. It is
// grown from the temporary directory rather than from a separator, because a
// path beginning with a separator is a full path on Unix and is not one on
// Windows, where a full path names the volume it is on.
func fullPath(parts ...string) string {
	return filepath.Join(append([]string{os.TempDir()}, parts...)...)
}

// TestWriteSecureFileFailsWhenTheTemporaryNameIsTaken reaches the write that
// goes wrong on every system rather than only where a directory can be made
// unwritable: the file is written beside its target first, and a directory
// standing in that place is refused everywhere.
func TestWriteSecureFileFailsWhenTheTemporaryNameIsTaken(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "profiles.toml")
	if err := os.Mkdir(filepath.Join(dir, ".profiles.toml.tmp"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writeSecureFile(target, []byte("x")); err == nil {
		t.Fatal("want an error when the temporary file cannot be written")
	}
}

// TestLoadingReportsAFileItCannotRead is the other half of a configuration that
// is there but unreadable: on a system that enforces permissions it is a file
// somebody loosened, and on one that does not it is a directory standing where
// a file belongs. Both arrive here as the same refusal.
func TestLoadingReportsAFileItCannotRead(t *testing.T) {
	t.Run("profiles", func(t *testing.T) {
		store := newStore(t)
		if err := os.Mkdir(store.Paths.ProfilesFile(), 0o700); err != nil {
			t.Fatal(err)
		}
		if _, err := store.LoadProfiles(); err == nil {
			t.Fatal("want an error when the profiles cannot be read")
		}
	})
	t.Run("settings", func(t *testing.T) {
		store := newStore(t)
		if err := os.Mkdir(store.Paths.SettingsFile(), 0o700); err != nil {
			t.Fatal(err)
		}
		if _, err := store.LoadSettings(); err == nil {
			t.Fatal("want an error when the settings cannot be read")
		}
	})
}
