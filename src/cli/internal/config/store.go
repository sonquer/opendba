package config

import (
	"errors"
	"io/fs"
)

type Store struct {
	Paths Paths
}

func NewStore(paths Paths) Store { return Store{Paths: paths} }

func Open() (Store, error) {
	paths, err := DefaultPaths()
	if err != nil {
		return Store{}, err
	}
	if err := paths.Ensure(); err != nil {
		return Store{}, err
	}
	return NewStore(paths), nil
}

func (s Store) LoadProfiles() (Profiles, error) {
	data, err := readSecureFile(s.Paths.ProfilesFile())
	if errors.Is(err, fs.ErrNotExist) {
		return Profiles{}, nil
	}
	if err != nil {
		return Profiles{}, err
	}
	var profiles Profiles
	if err := decodeTOML(data, &profiles); err != nil {
		return Profiles{}, err
	}
	if err := profiles.Validate(); err != nil {
		return Profiles{}, err
	}
	return profiles, nil
}

func (s Store) SaveProfiles(profiles Profiles) error {
	if err := profiles.Validate(); err != nil {
		return err
	}
	data, err := encodeTOML(profiles)
	if err != nil {
		return err
	}
	return writeSecureFile(s.Paths.ProfilesFile(), data)
}

func (s Store) LoadSettings() (Settings, error) {
	data, err := readSecureFile(s.Paths.SettingsFile())
	if errors.Is(err, fs.ErrNotExist) {
		return DefaultSettings(), nil
	}
	if err != nil {
		return Settings{}, err
	}
	var settings Settings
	if err := decodeTOML(data, &settings); err != nil {
		return Settings{}, err
	}
	settings = settings.withDefaults()
	if err := settings.Validate(); err != nil {
		return Settings{}, err
	}
	return settings, nil
}

func (s Store) SaveSettings(settings Settings) error {
	if err := settings.Validate(); err != nil {
		return err
	}
	data, err := encodeTOML(settings)
	if err != nil {
		return err
	}
	return writeSecureFile(s.Paths.SettingsFile(), data)
}
