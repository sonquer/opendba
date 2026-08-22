package secretref

import (
	"context"
	"errors"
	"fmt"

	"github.com/zalando/go-keyring"
)

const KeyringService = "tui4db"

// KeyringAPI is the part of the operating system keychain this package uses.
type KeyringAPI interface {
	Get(service, user string) (string, error)
	Set(service, user, password string) error
	Delete(service, user string) error
}

type systemKeyring struct{}

func (systemKeyring) Get(service, user string) (string, error) { return keyring.Get(service, user) }

func (systemKeyring) Set(service, user, password string) error {
	return keyring.Set(service, user, password)
}

func (systemKeyring) Delete(service, user string) error { return keyring.Delete(service, user) }

// KeyringBackend stores secrets in the operating system keychain: Keychain on
// macOS, Credential Manager on Windows, Secret Service on Linux and BSD.
type KeyringBackend struct {
	Service string
	API     KeyringAPI
}

// NewKeyringBackend returns a backend using the system keychain.
func NewKeyringBackend() KeyringBackend {
	return KeyringBackend{Service: KeyringService, API: systemKeyring{}}
}

func (KeyringBackend) Scheme() string { return SchemeKeyring }

func (b KeyringBackend) api() KeyringAPI {
	if b.API == nil {
		return systemKeyring{}
	}
	return b.API
}

func (b KeyringBackend) service() string {
	if b.Service == "" {
		return KeyringService
	}
	return b.Service
}

func (b KeyringBackend) Get(_ context.Context, ref Ref) ([]byte, error) {
	secret, err := b.api().Get(b.service(), ref.Value)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil, fmt.Errorf("%w: %s is not in the keychain", ErrNotFound, ref)
	}
	if err != nil {
		return nil, fmt.Errorf("read %s from the keychain: %w", ref, err)
	}
	return []byte(secret), nil
}

func (b KeyringBackend) Set(_ context.Context, ref Ref, secret []byte) error {
	if err := b.api().Set(b.service(), ref.Value, string(secret)); err != nil {
		return fmt.Errorf("store %s in the keychain: %w", ref, err)
	}
	return nil
}

func (b KeyringBackend) Delete(_ context.Context, ref Ref) error {
	err := b.api().Delete(b.service(), ref.Value)
	if err == nil || errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return fmt.Errorf("remove %s from the keychain: %w", ref, err)
}

// Available reports whether the keychain can actually be written to and read
// back, which is how setup decides whether to offer the encrypted vault instead.
func (b KeyringBackend) Available() bool {
	probe := Ref{Scheme: SchemeKeyring, Value: "availability-probe"}
	if err := b.Set(context.Background(), probe, []byte("probe")); err != nil {
		return false
	}
	return b.Delete(context.Background(), probe) == nil
}
