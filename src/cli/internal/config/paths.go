package config

import (
	"fmt"
	"os"
	"path/filepath"
)

const appName = "opendba"

type Paths struct {
	Config string
	State  string
	Data   string
}

func (p Paths) ProfilesFile() string { return filepath.Join(p.Config, "profiles.toml") }

func (p Paths) SettingsFile() string { return filepath.Join(p.Config, "settings.toml") }

func (p Paths) VaultFile() string { return filepath.Join(p.Config, "secrets.age") }

func (p Paths) HistoryFile() string { return filepath.Join(p.State, "history.db") }

// ChatsFile is where conversations with the assistant are kept. State rather
// than config: it is what the program has done, not how it was told to behave.
func (p Paths) ChatsFile() string { return filepath.Join(p.State, "chats.db") }

// ModelsDir is where downloaded models are kept.
func (p Paths) ModelsDir() string { return filepath.Join(p.Data, "models") }

// SQLDir is where the statements you keep are kept, unless the settings name
// somewhere else. Data rather than state: they are yours.
func (p Paths) SQLDir() string { return filepath.Join(p.Data, "sql") }

// LibDir is where the inference library is unpacked.
func (p Paths) LibDir() string { return filepath.Join(p.Data, "lib") }

// EngineLog is where the inference library's own account of what it is doing is
// written.
func (p Paths) EngineLog() string { return filepath.Join(p.State, "engine.log") }

// CrashFile is where an account of a failure that ended the program is written,
// named for when it happened so that one does not overwrite the last.
func (p Paths) CrashFile(stamp string) string {
	return filepath.Join(p.State, "crash-"+stamp+".log")
}

type Environment func(string) string

func DefaultPaths() (Paths, error) { return PathsFor(os.Getenv) }

func PathsFor(env Environment) (Paths, error) {
	home := env("HOME")
	if home == "" {
		home = env("USERPROFILE")
	}
	config := env("XDG_CONFIG_HOME")
	if config == "" {
		if home == "" {
			return Paths{}, fmt.Errorf("cannot determine the configuration directory: neither XDG_CONFIG_HOME nor HOME is set")
		}
		config = filepath.Join(home, ".config")
	}
	state := env("XDG_STATE_HOME")
	if state == "" {
		if home == "" {
			return Paths{}, fmt.Errorf("cannot determine the state directory: neither XDG_STATE_HOME nor HOME is set")
		}
		state = filepath.Join(home, ".local", "state")
	}
	data := env("XDG_DATA_HOME")
	if data == "" {
		if home == "" {
			return Paths{}, fmt.Errorf("cannot determine the data directory: neither XDG_DATA_HOME nor HOME is set")
		}
		data = filepath.Join(home, ".local", "share")
	}
	return Paths{
		Config: filepath.Join(config, appName),
		State:  filepath.Join(state, appName),
		Data:   filepath.Join(data, appName),
	}, nil
}

// Ensure makes the directories and tightens them when another user can read
// them.
func (p Paths) Ensure() error {
	for _, dir := range []string{p.Config, p.State, p.Data} {
		if dir == "" {
			continue
		}
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
		if err := enforceDirMode(dir); err != nil {
			return err
		}
	}
	return nil
}

func enforceDirMode(dir string) error {
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("inspect %s: %w", dir, err)
	}
	if info.Mode().Perm()&0o077 == 0 {
		return nil
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return fmt.Errorf("tighten permissions on %s: %w", dir, err)
	}
	return nil
}
