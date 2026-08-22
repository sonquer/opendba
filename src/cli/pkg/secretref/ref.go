// Package secretref keeps database passwords out of configuration files.
//
// A connection profile stores a reference such as keyring:01J or env:PGPASSWORD,
// never a password. Resolving that reference is the job of a backend: the
// operating system keychain, an age-encrypted vault for machines without one, an
// environment variable, or an external command such as a password manager CLI.
//
// Two schemes are deliberately resolved elsewhere. A pgpass reference is left to
// the database driver, and a prompt reference means the user is asked and
// nothing is ever stored.
package secretref

import (
	"errors"
	"fmt"
	"strings"
)

var (
	ErrNotFound = errors.New("secret not found")
	ErrExternal = errors.New("secret is resolved outside the secret store")
	ErrScheme   = errors.New("unknown secret backend")
)

// Ref points at a secret held by one of the backends. It is what a connection
// profile stores; the secret itself never appears in configuration.
type Ref struct {
	Scheme string
	Value  string
}

// Parse reads a reference written as scheme:value, or as a bare scheme for the
// backends that need no value.
func Parse(text string) (Ref, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return Ref{}, fmt.Errorf("empty secret reference")
	}
	scheme, value, found := strings.Cut(trimmed, ":")
	scheme = strings.ToLower(strings.TrimSpace(scheme))
	if !found {
		return Ref{Scheme: scheme}, nil
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return Ref{}, fmt.Errorf("secret reference %q has no value", text)
	}
	return Ref{Scheme: scheme, Value: value}, nil
}

// String renders the reference in the form Parse accepts.
func (r Ref) String() string {
	if r.Value == "" {
		return r.Scheme
	}
	return r.Scheme + ":" + r.Value
}

// IsZero reports whether the reference is empty.
func (r Ref) IsZero() bool { return r.Scheme == "" }

const (
	SchemeKeyring = "keyring"
	SchemeVault   = "vault"
	SchemeEnv     = "env"
	SchemeCommand = "command"
	SchemePgpass  = "pgpass"
	SchemePrompt  = "prompt"
)

// ForKeyring returns a reference to a secret in the operating system keychain.
func ForKeyring(id string) Ref { return Ref{Scheme: SchemeKeyring, Value: id} }

// ForVault returns a reference to a secret in the encrypted vault file.
func ForVault(id string) Ref { return Ref{Scheme: SchemeVault, Value: id} }

// ForEnv returns a reference to a secret held in an environment variable.
func ForEnv(name string) Ref { return Ref{Scheme: SchemeEnv, Value: name} }
