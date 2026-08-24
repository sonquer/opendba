package cli

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/sonquer/opendba/src/cli/internal/config"
	"github.com/sonquer/opendba/src/cli/internal/driver"
	"github.com/sonquer/opendba/src/cli/internal/driver/postgres"
	"github.com/sonquer/opendba/src/cli/pkg/secretref"
)

type Setup struct {
	Store    config.Store
	Registry *driver.Registry
	Secrets  *secretref.Store
	ID       func() string
}

func (s Setup) NewID() string {
	if s.ID != nil {
		return s.ID()
	}
	return uuid.NewString()
}

func (s Setup) Test(ctx context.Context, connection config.Connection, password []byte) (driver.ServerInfo, error) {
	target, err := s.Registry.Get(connection.Driver)
	if err != nil {
		return driver.ServerInfo{}, err
	}
	settings, err := s.Store.LoadSettings()
	if err != nil {
		return driver.ServerInfo{}, err
	}
	conn, err := target.Connect(ctx, driver.Config{
		Host:        connection.Host,
		Port:        connection.Port,
		Database:    connection.Database,
		User:        connection.User,
		Password:    password,
		SSLMode:     connection.SSLMode,
		File:        connection.File,
		Mode:        Mode(connection.Mode),
		Application: connection.Name,
		Timeouts:    Timeouts(settings.Safety),
	})
	if err != nil {
		return driver.ServerInfo{}, err
	}
	defer func() { _ = conn.Close() }()
	return conn.Info(ctx)
}

func (s Setup) Save(connection config.Connection, password []byte) error {
	if len(password) > 0 {
		ref, err := s.storeSecret(connection.ID, password)
		if err != nil {
			return err
		}
		connection.Secret = ref.String()
	}
	profiles, err := s.Store.LoadProfiles()
	if err != nil {
		return err
	}
	if err := profiles.Upsert(connection); err != nil {
		return err
	}
	return s.Store.SaveProfiles(profiles)
}

// StoreKey puts an assistant's key in the operating system keychain and hands
// back the reference that goes in settings.toml.
func (s Setup) StoreKey(name string, key []byte) (secretref.Ref, error) {
	if s.Secrets == nil {
		return secretref.Ref{}, fmt.Errorf("there is nowhere to keep a key on this machine")
	}
	reference := secretref.ForKeyring("ai-" + name)
	if err := s.Secrets.Set(context.Background(), reference, key); err != nil {
		return secretref.Ref{}, fmt.Errorf("keep the key in the keychain: %w", err)
	}
	return reference, nil
}

func (s Setup) storeSecret(id string, password []byte) (secretref.Ref, error) {
	ctx := context.Background()
	keyring := secretref.ForKeyring(id)
	if err := s.Secrets.Set(ctx, keyring, password); err == nil {
		return keyring, nil
	}
	vault := secretref.ForVault(id)
	if err := s.Secrets.Set(ctx, vault, password); err != nil {
		return secretref.Ref{}, fmt.Errorf("store the password: %w", err)
	}
	return vault, nil
}

// Password resolves the secret a saved profile refers to, so a screen editing
// that profile can test the connection without making anyone retype it.
func (s Setup) Password(ctx context.Context, connection config.Connection) []byte {
	if strings.TrimSpace(connection.Secret) == "" {
		return nil
	}
	ref, err := secretref.Parse(connection.Secret)
	if err != nil {
		return nil
	}
	if ref.Scheme == secretref.SchemePrompt || ref.Scheme == secretref.SchemePgpass {
		return nil
	}
	secret, err := s.Secrets.Get(ctx, ref)
	if err != nil {
		return nil
	}
	return secret
}

func (s Setup) Forget(ctx context.Context, connection config.Connection) error {
	if strings.TrimSpace(connection.Secret) == "" {
		return nil
	}
	ref, err := secretref.Parse(connection.Secret)
	if err != nil {
		return nil
	}
	err = s.Secrets.Delete(ctx, ref)
	if err != nil && !errors.Is(err, secretref.ErrExternal) && !errors.Is(err, secretref.ErrScheme) {
		return fmt.Errorf("the connection is gone, but its password could not be removed: %w", err)
	}
	return nil
}

type DSN struct {
	Host     string
	Port     int
	Database string
	User     string
	SSLMode  string
}

func SplitDSN(dsn string) (string, []byte, error) { return postgres.Split(dsn) }

func ParseDSN(dsn string) (DSN, error) {
	parsed, err := url.Parse(strings.TrimSpace(dsn))
	if err != nil {
		return DSN{}, fmt.Errorf("read the connection string: %w", err)
	}
	if parsed.Host == "" {
		return DSN{}, fmt.Errorf("the connection string needs a host")
	}
	result := DSN{
		Host:     parsed.Hostname(),
		Database: strings.TrimPrefix(parsed.Path, "/"),
		SSLMode:  parsed.Query().Get("sslmode"),
	}
	if parsed.User != nil {
		result.User = parsed.User.Username()
	}
	if port := parsed.Port(); port != "" {
		number, err := strconv.Atoi(port)
		if err != nil {
			return DSN{}, fmt.Errorf("the port in the connection string is not a number: %w", err)
		}
		result.Port = number
	}
	if result.Port == 0 {
		result.Port = postgres.DefaultPort
	}
	if result.SSLMode == "" {
		result.SSLMode = "prefer"
	}
	return result, nil
}
