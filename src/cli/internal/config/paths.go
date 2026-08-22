package config

import (
	"fmt"
	"os"
	"path/filepath"
)

const appName = "tui4db"

type Paths struct {
	Config string
	State  string
}

func (p Paths) ProfilesFile() string { return filepath.Join(p.Config, "profiles.toml") }

func (p Paths) SettingsFile() string { return filepath.Join(p.Config, "settings.toml") }

func (p Paths) VaultFile() string { return filepath.Join(p.Config, "secrets.age") }

func (p Paths) HistoryFile() string { return filepath.Join(p.State, "history.db") }

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
	return Paths{
		Config: filepath.Join(config, appName),
		State:  filepath.Join(state, appName),
	}, nil
}

func (p Paths) Ensure() error {
	for _, dir := range []string{p.Config, p.State} {
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
