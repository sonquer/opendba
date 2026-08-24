package secretref

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filippo.io/age"
	"github.com/zalando/go-keyring"
)

func TestParse(t *testing.T) {
	cases := []struct {
		name    string
		text    string
		want    Ref
		wantErr bool
	}{
		{"keyring", "keyring:opendba/01J", Ref{Scheme: "keyring", Value: "opendba/01J"}, false},
		{"env", "env:PGPASSWORD", Ref{Scheme: "env", Value: "PGPASSWORD"}, false},
		{"command with spaces", "command:op read op://vault/db/password", Ref{Scheme: "command", Value: "op read op://vault/db/password"}, false},
		{"scheme only", "prompt", Ref{Scheme: "prompt"}, false},
		{"upper case scheme", "ENV:X", Ref{Scheme: "env", Value: "X"}, false},
		{"surrounding space", "  env:X  ", Ref{Scheme: "env", Value: "X"}, false},
		{"empty", "", Ref{}, true},
		{"blank", "   ", Ref{}, true},
		{"no value", "env:", Ref{}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := Parse(c.text)
			if (err != nil) != c.wantErr {
				t.Fatalf("Parse(%q) error = %v, wantErr %v", c.text, err, c.wantErr)
			}
			if err == nil && got != c.want {
				t.Errorf("Parse(%q) = %+v, want %+v", c.text, got, c.want)
			}
		})
	}
}

func TestRefFormatting(t *testing.T) {
	if got := ForKeyring("abc").String(); got != "keyring:abc" {
		t.Errorf("String() = %q", got)
	}
	if got := ForVault("abc").String(); got != "vault:abc" {
		t.Errorf("String() = %q", got)
	}
	if got := ForEnv("X").String(); got != "env:X" {
		t.Errorf("String() = %q", got)
	}
	if got := (Ref{Scheme: "prompt"}).String(); got != "prompt" {
		t.Errorf("String() = %q", got)
	}
	if !(Ref{}).IsZero() || ForEnv("X").IsZero() {
		t.Error("IsZero must only be true for the empty reference")
	}
}

type fakeBackend struct {
	scheme  string
	secrets map[string]string
	failGet bool
}

func (f *fakeBackend) Scheme() string { return f.scheme }

func (f *fakeBackend) Get(_ context.Context, ref Ref) ([]byte, error) {
	if f.failGet {
		return nil, errors.New("boom")
	}
	secret, ok := f.secrets[ref.Value]
	if !ok {
		return nil, ErrNotFound
	}
	return []byte(secret), nil
}

func (f *fakeBackend) Set(_ context.Context, ref Ref, secret []byte) error {
	f.secrets[ref.Value] = string(secret)
	return nil
}

func (f *fakeBackend) Delete(_ context.Context, ref Ref) error {
	delete(f.secrets, ref.Value)
	return nil
}

func TestStoreRoutesByScheme(t *testing.T) {
	backend := &fakeBackend{scheme: SchemeEnv, secrets: map[string]string{}}
	store := NewStore(backend)
	ctx := context.Background()
	ref := ForEnv("X")

	if err := store.Set(ctx, ref, []byte("hunter2")); err != nil {
		t.Fatalf("Set: %v", err)
	}
	secret, err := store.Get(ctx, ref)
	if err != nil || string(secret) != "hunter2" {
		t.Fatalf("Get = %q, %v", secret, err)
	}
	if err := store.Delete(ctx, ref); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := store.Get(ctx, ref); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get after Delete = %v", err)
	}
	if names := store.Schemes(); len(names) != 1 || names[0] != SchemeEnv {
		t.Errorf("Schemes() = %v", names)
	}
}

func TestStoreRejectsUnknownAndExternalSchemes(t *testing.T) {
	store := NewStore()
	ctx := context.Background()
	for _, scheme := range []string{SchemePrompt, SchemePgpass} {
		if _, err := store.Get(ctx, Ref{Scheme: scheme}); !errors.Is(err, ErrExternal) {
			t.Errorf("%s must be resolved elsewhere, got %v", scheme, err)
		}
	}
	if _, err := store.Get(ctx, Ref{Scheme: "nope"}); !errors.Is(err, ErrScheme) {
		t.Errorf("unknown scheme error = %v", err)
	}
	if err := store.Set(ctx, Ref{Scheme: "nope"}, nil); !errors.Is(err, ErrScheme) {
		t.Errorf("Set error = %v", err)
	}
	if err := store.Delete(ctx, Ref{Scheme: "nope"}); !errors.Is(err, ErrScheme) {
		t.Errorf("Delete error = %v", err)
	}
}

func TestZeroWipesTheSecret(t *testing.T) {
	secret := []byte("hunter2")
	Zero(secret)
	for _, b := range secret {
		if b != 0 {
			t.Fatalf("secret was not wiped: %q", secret)
		}
	}
}

func TestEnvBackend(t *testing.T) {
	backend := EnvBackend{Lookup: func(name string) (string, bool) {
		if name == "PGPASSWORD" {
			return "hunter2", true
		}
		return "", false
	}}
	secret, err := backend.Get(context.Background(), ForEnv("PGPASSWORD"))
	if err != nil || string(secret) != "hunter2" {
		t.Fatalf("Get = %q, %v", secret, err)
	}
	if _, err := backend.Get(context.Background(), ForEnv("MISSING")); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing variable error = %v", err)
	}
	if backend.Scheme() != SchemeEnv {
		t.Error("wrong scheme")
	}
	if err := backend.Set(context.Background(), ForEnv("X"), nil); err == nil {
		t.Error("environment variables cannot be written")
	}
	if err := backend.Delete(context.Background(), ForEnv("X")); err == nil {
		t.Error("environment variables cannot be deleted")
	}
}

func TestEnvBackendFallsBackToTheProcessEnvironment(t *testing.T) {
	t.Setenv("OPENDBA_TEST_SECRET", "from-process")
	backend := NewEnvBackend()
	backend.Lookup = nil
	secret, err := backend.Get(context.Background(), ForEnv("OPENDBA_TEST_SECRET"))
	if err != nil || string(secret) != "from-process" {
		t.Fatalf("Get = %q, %v", secret, err)
	}
}

func TestCommandBackend(t *testing.T) {
	backend := CommandBackend{Run: func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name != "op" || len(args) != 2 {
			return nil, errors.New("unexpected command")
		}
		return []byte("hunter2\n"), nil
	}}
	secret, err := backend.Get(context.Background(), Ref{Scheme: SchemeCommand, Value: "op read op://v/db"})
	if err != nil || string(secret) != "hunter2" {
		t.Fatalf("Get = %q, %v", secret, err)
	}
	if backend.Scheme() != SchemeCommand {
		t.Error("wrong scheme")
	}
	if err := backend.Set(context.Background(), Ref{}, nil); err == nil {
		t.Error("command secrets cannot be written")
	}
	if err := backend.Delete(context.Background(), Ref{}); err == nil {
		t.Error("command secrets cannot be deleted")
	}
}

func TestCommandBackendFailures(t *testing.T) {
	empty := CommandBackend{Run: func(context.Context, string, ...string) ([]byte, error) { return []byte("\n"), nil }}
	if _, err := empty.Get(context.Background(), Ref{Scheme: SchemeCommand, Value: "true"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("empty output error = %v", err)
	}
	failing := CommandBackend{Run: func(context.Context, string, ...string) ([]byte, error) { return nil, errors.New("exit 1") }}
	if _, err := failing.Get(context.Background(), Ref{Scheme: SchemeCommand, Value: "false"}); err == nil {
		t.Error("want command error")
	}
	if _, err := (CommandBackend{}).Get(context.Background(), Ref{Scheme: SchemeCommand, Value: "   "}); err == nil {
		t.Error("an empty command must fail")
	}
}

func TestCommandBackendRunsRealCommands(t *testing.T) {
	if NewCommandBackend().Run == nil {
		t.Fatal("the default backend must be able to run commands")
	}
	var backend CommandBackend
	secret, err := backend.Get(context.Background(), Ref{Scheme: SchemeCommand, Value: "echo hunter2"})
	if err != nil || string(secret) != "hunter2" {
		t.Fatalf("Get = %q, %v", secret, err)
	}
}

type fakeKeyring struct {
	entries map[string]string
	setErr  error
	getErr  error
	delErr  error
}

func (f *fakeKeyring) Get(service, user string) (string, error) {
	if f.getErr != nil {
		return "", f.getErr
	}
	secret, ok := f.entries[service+"/"+user]
	if !ok {
		return "", keyring.ErrNotFound
	}
	return secret, nil
}

func (f *fakeKeyring) Set(service, user, password string) error {
	if f.setErr != nil {
		return f.setErr
	}
	f.entries[service+"/"+user] = password
	return nil
}

func (f *fakeKeyring) Delete(service, user string) error {
	if f.delErr != nil {
		return f.delErr
	}
	if _, ok := f.entries[service+"/"+user]; !ok {
		return keyring.ErrNotFound
	}
	delete(f.entries, service+"/"+user)
	return nil
}

func TestKeyringBackendRoundTrip(t *testing.T) {
	api := &fakeKeyring{entries: map[string]string{}}
	backend := KeyringBackend{API: api}
	ctx := context.Background()
	ref := ForKeyring("01J")

	if backend.Scheme() != SchemeKeyring {
		t.Error("wrong scheme")
	}
	if err := backend.Set(ctx, ref, []byte("hunter2")); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if api.entries[KeyringService+"/01J"] != "hunter2" {
		t.Fatalf("entries = %v", api.entries)
	}
	secret, err := backend.Get(ctx, ref)
	if err != nil || string(secret) != "hunter2" {
		t.Fatalf("Get = %q, %v", secret, err)
	}
	if err := backend.Delete(ctx, ref); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := backend.Get(ctx, ref); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get after Delete = %v", err)
	}
	if err := backend.Delete(ctx, ref); err != nil {
		t.Errorf("deleting a missing secret must succeed: %v", err)
	}
	if !backend.Available() {
		t.Error("a working keychain must be reported as available")
	}
}

func TestKeyringBackendFailures(t *testing.T) {
	ctx := context.Background()
	ref := ForKeyring("01J")

	writeFails := KeyringBackend{API: &fakeKeyring{entries: map[string]string{}, setErr: errors.New("locked")}}
	if err := writeFails.Set(ctx, ref, []byte("x")); err == nil {
		t.Error("want set error")
	}
	if writeFails.Available() {
		t.Error("a keychain that cannot be written is not available")
	}

	readFails := KeyringBackend{API: &fakeKeyring{entries: map[string]string{}, getErr: errors.New("locked")}}
	if _, err := readFails.Get(ctx, ref); err == nil {
		t.Error("want get error")
	}

	deleteFails := KeyringBackend{API: &fakeKeyring{entries: map[string]string{}, delErr: errors.New("locked")}}
	if err := deleteFails.Delete(ctx, ref); err == nil {
		t.Error("want delete error")
	}
	if deleteFails.Available() {
		t.Error("a keychain that cannot be cleaned up is not available")
	}
}

func TestKeyringBackendDefaults(t *testing.T) {
	backend := NewKeyringBackend()
	if backend.service() != KeyringService || backend.API == nil {
		t.Error("the default backend must target the opendba service")
	}
	bare := KeyringBackend{}
	if bare.service() != KeyringService {
		t.Error("an empty service must fall back to the default")
	}
	if bare.api() == nil {
		t.Error("an empty api must fall back to the system keychain")
	}
}

func vaultPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "secrets.age")
}

func TestVaultRoundTrip(t *testing.T) {
	path := vaultPath(t)
	calls := 0
	vault := NewVaultBackend(path, func() ([]byte, error) {
		calls++
		return []byte("correct horse"), nil
	})
	ctx := context.Background()
	ref := ForVault("01J")

	if vault.Scheme() != SchemeVault {
		t.Error("wrong scheme")
	}
	if err := vault.Set(ctx, ref, []byte("hunter2")); err != nil {
		t.Fatalf("Set: %v", err)
	}
	secret, err := vault.Get(ctx, ref)
	if err != nil || string(secret) != "hunter2" {
		t.Fatalf("Get = %q, %v", secret, err)
	}
	if calls != 1 {
		t.Errorf("the passphrase was requested %d times, want 1", calls)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "hunter2") {
		t.Fatal("the vault must not store the secret in clear text")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("vault mode = %04o, want 0600", info.Mode().Perm())
	}

	reopened := NewVaultBackend(path, func() ([]byte, error) { return []byte("correct horse"), nil })
	secret, err = reopened.Get(ctx, ref)
	if err != nil || string(secret) != "hunter2" {
		t.Fatalf("reopened Get = %q, %v", secret, err)
	}

	if err := vault.Delete(ctx, ref); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := vault.Get(ctx, ref); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get after Delete = %v", err)
	}
	if err := vault.Delete(ctx, ForVault("missing")); err != nil {
		t.Errorf("deleting a missing secret must succeed: %v", err)
	}
}

func TestVaultRejectsTheWrongPassphrase(t *testing.T) {
	path := vaultPath(t)
	ctx := context.Background()
	if err := NewVaultBackend(path, static("right")).Set(ctx, ForVault("a"), []byte("s")); err != nil {
		t.Fatal(err)
	}
	if _, err := NewVaultBackend(path, static("wrong")).Get(ctx, ForVault("a")); err == nil {
		t.Fatal("a wrong passphrase must not unlock the vault")
	}
}

func TestVaultPassphraseFailures(t *testing.T) {
	ctx := context.Background()
	ref := ForVault("a")

	noProvider := NewVaultBackend(vaultPath(t), nil)
	if err := noProvider.Set(ctx, ref, []byte("s")); err == nil {
		t.Error("want error without a passphrase provider")
	}
	failing := NewVaultBackend(vaultPath(t), func() ([]byte, error) { return nil, errors.New("cancelled") })
	if err := failing.Set(ctx, ref, []byte("s")); err == nil {
		t.Error("want error when the passphrase cannot be read")
	}
	empty := NewVaultBackend(vaultPath(t), func() ([]byte, error) { return nil, nil })
	if err := empty.Set(ctx, ref, []byte("s")); err == nil {
		t.Error("an empty passphrase must be rejected")
	}
}

func TestVaultLockForgetsThePassphrase(t *testing.T) {
	path := vaultPath(t)
	calls := 0
	vault := NewVaultBackend(path, func() ([]byte, error) {
		calls++
		return []byte("correct horse"), nil
	})
	ctx := context.Background()
	if err := vault.Set(ctx, ForVault("a"), []byte("s")); err != nil {
		t.Fatal(err)
	}
	vault.Lock()
	if _, err := vault.Get(ctx, ForVault("a")); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Errorf("the passphrase was requested %d times, want 2", calls)
	}
}

func TestVaultRejectsBrokenFiles(t *testing.T) {
	path := vaultPath(t)
	if err := os.WriteFile(path, []byte("not an age file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewVaultBackend(path, static("x")).Get(context.Background(), ForVault("a")); err == nil {
		t.Fatal("want decryption error")
	}
}

func TestVaultReportsUnreadablePaths(t *testing.T) {
	dir := t.TempDir()
	if _, err := NewVaultBackend(dir, static("x")).Get(context.Background(), ForVault("a")); err == nil {
		t.Fatal("want read error")
	}
	blocked := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocked, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := NewVaultBackend(filepath.Join(blocked, "sub", "secrets.age"), static("x")).Set(context.Background(), ForVault("a"), []byte("s")); err == nil {
		t.Fatal("want write error")
	}
}

func static(passphrase string) Passphrase {
	return func() ([]byte, error) { return []byte(passphrase), nil }
}

func TestSystemKeyringRoundTrip(t *testing.T) {
	keyring.MockInit()
	backend := NewKeyringBackend()
	ctx := context.Background()
	ref := ForKeyring("system-probe")

	if err := backend.Set(ctx, ref, []byte("hunter2")); err != nil {
		t.Fatalf("Set: %v", err)
	}
	secret, err := backend.Get(ctx, ref)
	if err != nil || string(secret) != "hunter2" {
		t.Fatalf("Get = %q, %v", secret, err)
	}
	if err := backend.Delete(ctx, ref); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := backend.Get(ctx, ref); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get after Delete = %v", err)
	}
}

func TestSystemKeyringReportsFailures(t *testing.T) {
	keyring.MockInitWithError(errors.New("keychain locked"))
	t.Cleanup(keyring.MockInit)
	backend := NewKeyringBackend()
	ctx := context.Background()
	ref := ForKeyring("system-probe")

	if err := backend.Set(ctx, ref, []byte("x")); err == nil {
		t.Error("want set error")
	}
	if _, err := backend.Get(ctx, ref); err == nil {
		t.Error("want get error")
	}
	if err := backend.Delete(ctx, ref); err == nil {
		t.Error("want delete error")
	}
}

func TestVaultRejectsContentsThatAreNotAVault(t *testing.T) {
	path := vaultPath(t)
	recipient, err := age.NewScryptRecipient("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	var encrypted bytes.Buffer
	writer, err := age.Encrypt(&encrypted, recipient)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte("this is not json")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, encrypted.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = NewVaultBackend(path, static("correct horse")).Get(context.Background(), ForVault("a"))
	if err == nil || !strings.Contains(err.Error(), "parse the vault") {
		t.Fatalf("err = %v", err)
	}
}

func TestVaultDeleteNeedsThePassphrase(t *testing.T) {
	path := vaultPath(t)
	if err := NewVaultBackend(path, static("right")).Set(context.Background(), ForVault("a"), []byte("s")); err != nil {
		t.Fatal(err)
	}
	if err := NewVaultBackend(path, static("wrong")).Delete(context.Background(), ForVault("a")); err == nil {
		t.Fatal("deleting from a locked vault must fail")
	}
}

func TestCommandBackendTrimsWindowsLineEndings(t *testing.T) {
	backend := CommandBackend{Run: func(context.Context, string, ...string) ([]byte, error) {
		return []byte("hunter2\r\n"), nil
	}}
	secret, err := backend.Get(context.Background(), Ref{Scheme: SchemeCommand, Value: "pass show db"})
	if err != nil || string(secret) != "hunter2" {
		t.Fatalf("Get = %q, %v", secret, err)
	}
}

func TestVaultReportsRenameFailures(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "secrets.age")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "keep"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := NewVaultBackend(target, static("correct horse")).Set(context.Background(), ForVault("a"), []byte("s"))
	if err == nil {
		t.Fatal("want an error when the vault cannot be replaced")
	}
}

func TestVaultReportsUnreadableVaultFiles(t *testing.T) {
	dir := t.TempDir()
	vault := NewVaultBackend(dir, static("correct horse"))
	if err := vault.Delete(context.Background(), ForVault("a")); err == nil {
		t.Fatal("want a read error")
	}
}

func TestVaultNeedsThePassphraseToRead(t *testing.T) {
	path := vaultPath(t)
	if err := NewVaultBackend(path, static("correct horse")).Set(context.Background(), ForVault("a"), []byte("s")); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	cases := map[string]*VaultBackend{
		"no provider":      NewVaultBackend(path, nil),
		"provider fails":   NewVaultBackend(path, func() ([]byte, error) { return nil, errors.New("cancelled") }),
		"empty passphrase": NewVaultBackend(path, func() ([]byte, error) { return nil, nil }),
	}
	for name, vault := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := vault.Get(ctx, ForVault("a")); err == nil {
				t.Error("reading must fail")
			}
			if err := vault.Delete(ctx, ForVault("a")); err == nil {
				t.Error("deleting must fail")
			}
			if err := vault.Set(ctx, ForVault("b"), []byte("x")); err == nil {
				t.Error("writing must fail")
			}
		})
	}
}

func TestVaultKeepsOtherSecretsWhenOneIsDeleted(t *testing.T) {
	path := vaultPath(t)
	vault := NewVaultBackend(path, static("correct horse"))
	ctx := context.Background()
	if err := vault.Set(ctx, ForVault("a"), []byte("first")); err != nil {
		t.Fatal(err)
	}
	if err := vault.Set(ctx, ForVault("b"), []byte("second")); err != nil {
		t.Fatal(err)
	}
	if err := vault.Delete(ctx, ForVault("a")); err != nil {
		t.Fatal(err)
	}
	secret, err := vault.Get(ctx, ForVault("b"))
	if err != nil || string(secret) != "second" {
		t.Fatalf("Get = %q, %v", secret, err)
	}
}
