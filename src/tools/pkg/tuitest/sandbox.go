package tuitest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const configMode = 0o600

// Profile is one connection the program should find in its configuration.
type Profile struct {
	ID     string
	Name   string
	Driver string
	File   string
	Mode   string
	Color  string
}

// Sandbox is a configuration and state tree of its own, so that a run cannot
// read or write what the person running it uses.
type Sandbox struct {
	Root string
}

// NewSandbox lays out the directories the program reads through the XDG
// variables.
func NewSandbox(root string) (Sandbox, error) {
	box := Sandbox{Root: root}
	for _, dir := range []string{box.config(), box.state(), box.data(), box.Databases()} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return Sandbox{}, fmt.Errorf("lay out the sandbox: %w", err)
		}
	}
	return box, nil
}

func (b Sandbox) config() string { return filepath.Join(b.Root, "config", "opendba") }
func (b Sandbox) state() string  { return filepath.Join(b.Root, "state", "opendba") }
func (b Sandbox) data() string   { return filepath.Join(b.Root, "data", "opendba") }

// Databases is where a seeded fixture is written.
func (b Sandbox) Databases() string { return filepath.Join(b.Root, "db") }

// StateDir is where the program keeps what outlives a connection, including the
// account it leaves behind when it fails.
func (b Sandbox) StateDir() string { return b.state() }

// Environment is the environment a program started in this sandbox must have.
func (b Sandbox) Environment() []string {
	return []string{
		"HOME=" + b.Root,
		"USERPROFILE=" + b.Root,
		"XDG_CONFIG_HOME=" + filepath.Join(b.Root, "config"),
		"XDG_STATE_HOME=" + filepath.Join(b.Root, "state"),
		"XDG_DATA_HOME=" + filepath.Join(b.Root, "data"),
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
		"LANG=en_US.UTF-8",
		"NO_COLOR=",
		"PATH=" + os.Getenv("PATH"),
	}
}

// WriteProfiles writes profiles.toml, which the program refuses to read unless
// only its owner can.
func (b Sandbox) WriteProfiles(profiles []Profile) error {
	var out strings.Builder
	for _, profile := range profiles {
		fmt.Fprintf(&out, "[[connection]]\nid = %q\nname = %q\ndriver = %q\n",
			profile.ID, profile.Name, profile.Driver)
		if profile.File != "" {
			fmt.Fprintf(&out, "file = %q\n", profile.File)
		}
		fmt.Fprintf(&out, "mode = %q\ncolor = %q\n\n", profile.Mode, profile.Color)
	}
	return b.write("profiles.toml", out.String())
}

// WriteSettings pins what would otherwise be read from the terminal or from
// somebody's preferences, and turns off what keeps a file of its own between
// runs, so that two runs draw the same screen.
func (b Sandbox) WriteSettings(bar string) error {
	settings := fmt.Sprintf(`[appearance]
theme = "dark"
accent = "cyan"
bar = %q
mouse = "off"

[safety]
default_mode = "readonly"
confirm_queries = true
query_timeout = "15s"
lock_timeout = "2s"
row_limit = 1000
slow_query = "30s"
stuck_query = "5m"

[history]
enabled = false
store_sql = false
limit = 500

[chats]
enabled = false
limit = 100

[ai]
enabled = false
provider = "local"
model = "gemma-4-e4b-qat"
context = 8192
`, bar)
	return b.write("settings.toml", settings)
}

func (b Sandbox) write(name, content string) error {
	path := filepath.Join(b.config(), name)
	if err := os.WriteFile(path, []byte(content), configMode); err != nil {
		return fmt.Errorf("write %s: %w", name, err)
	}
	if err := os.Chmod(path, configMode); err != nil {
		return fmt.Errorf("close the permissions on %s: %w", name, err)
	}
	return nil
}

// Crashes is the list of accounts the program left behind after failing, which
// must be empty when a scenario ends.
func (b Sandbox) Crashes() ([]string, error) {
	entries, err := os.ReadDir(b.state())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("look for crash reports: %w", err)
	}
	var found []string
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "crash-") {
			found = append(found, filepath.Join(b.state(), entry.Name()))
		}
	}
	return found, nil
}
