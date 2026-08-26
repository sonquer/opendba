package secretref

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"filippo.io/age"
)

// Passphrase is asked for the vault passphrase, once per session.
type Passphrase func() ([]byte, error)

// VaultBackend keeps secrets in a single age-encrypted file, for machines
// without a usable keychain. The passphrase is asked for once and held in memory
// until Lock is called.
type VaultBackend struct {
	Path       string
	Passphrase Passphrase

	cached []byte
}

// NewVaultBackend returns a vault stored at path.
func NewVaultBackend(path string, passphrase Passphrase) *VaultBackend {
	return &VaultBackend{Path: path, Passphrase: passphrase}
}

func (*VaultBackend) Scheme() string { return SchemeVault }

func (v *VaultBackend) Get(_ context.Context, ref Ref) ([]byte, error) {
	entries, err := v.load()
	if err != nil {
		return nil, err
	}
	secret, ok := entries[ref.Value]
	if !ok {
		return nil, fmt.Errorf("%w: %s is not in the vault", ErrNotFound, ref)
	}
	return []byte(secret), nil
}

func (v *VaultBackend) Set(_ context.Context, ref Ref, secret []byte) error {
	entries, err := v.load()
	if err != nil {
		return err
	}
	entries[ref.Value] = string(secret)
	return v.save(entries)
}

func (v *VaultBackend) Delete(_ context.Context, ref Ref) error {
	entries, err := v.load()
	if err != nil {
		return err
	}
	if _, ok := entries[ref.Value]; !ok {
		return nil
	}
	delete(entries, ref.Value)
	return v.save(entries)
}

// Lock forgets the cached passphrase and wipes it from memory.
func (v *VaultBackend) Lock() {
	Zero(v.cached)
	v.cached = nil
}

func (v *VaultBackend) passphrase() ([]byte, error) {
	if len(v.cached) > 0 {
		return v.cached, nil
	}
	if v.Passphrase == nil {
		return nil, fmt.Errorf("the vault needs a passphrase")
	}
	passphrase, err := v.Passphrase()
	if err != nil {
		return nil, fmt.Errorf("read the vault passphrase: %w", err)
	}
	if len(passphrase) == 0 {
		return nil, fmt.Errorf("the vault passphrase cannot be empty")
	}
	v.cached = passphrase
	return passphrase, nil
}

func (v *VaultBackend) load() (map[string]string, error) {
	data, err := os.ReadFile(v.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read the vault: %w", err)
	}
	passphrase, err := v.passphrase()
	if err != nil {
		return nil, err
	}
	identity, err := age.NewScryptIdentity(string(passphrase))
	if err != nil {
		return nil, fmt.Errorf("prepare the vault key: %w", err)
	}
	reader, err := age.Decrypt(bytes.NewReader(data), identity)
	if err != nil {
		return nil, fmt.Errorf("unlock the vault: %w", err)
	}
	plain, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("read the vault contents: %w", err)
	}
	entries := map[string]string{}
	if err := json.Unmarshal(plain, &entries); err != nil {
		return nil, fmt.Errorf("parse the vault contents: %w", err)
	}
	return entries, nil
}

func (v *VaultBackend) save(entries map[string]string) error {
	passphrase, err := v.passphrase()
	if err != nil {
		return err
	}
	recipient, err := age.NewScryptRecipient(string(passphrase))
	if err != nil {
		return fmt.Errorf("prepare the vault key: %w", err)
	}
	recipient.SetWorkFactor(18)

	plain, err := json.Marshal(entries)
	if err != nil {
		return fmt.Errorf("encode the vault contents: %w", err)
	}
	var encrypted bytes.Buffer
	writer, err := age.Encrypt(&encrypted, recipient)
	if err != nil {
		return fmt.Errorf("lock the vault: %w", err)
	}
	if _, err := writer.Write(plain); err != nil {
		return fmt.Errorf("write the vault contents: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("seal the vault: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(v.Path), 0o700); err != nil {
		return fmt.Errorf("create the vault directory: %w", err)
	}
	temp := v.Path + ".tmp"
	if err := os.WriteFile(temp, encrypted.Bytes(), 0o600); err != nil {
		return fmt.Errorf("write the vault: %w", err)
	}
	if err := os.Rename(temp, v.Path); err != nil {
		_ = os.Remove(temp)
		return fmt.Errorf("replace the vault: %w", err)
	}
	return nil
}
