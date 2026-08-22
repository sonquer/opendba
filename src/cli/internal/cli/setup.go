package cli

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/sonquer/tui4db/src/cli/internal/config"
	"github.com/sonquer/tui4db/src/cli/internal/driver"
	"github.com/sonquer/tui4db/src/cli/internal/driver/postgres"
	"github.com/sonquer/tui4db/src/cli/pkg/secretref"
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
