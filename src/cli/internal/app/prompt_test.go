package app

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

// console4Test is a terminal that says what it was told to say.
func console4Test(terminal bool, secret string, err error) (console, *bytes.Buffer) {
	written := &bytes.Buffer{}
	return console{
		descriptor: 7,
		isTerminal: func(int) bool { return terminal },
		read:       func(int) ([]byte, error) { return []byte(secret), err },
		out:        written,
	}, written
}

func TestAPasswordIsOnlyReadFromATerminal(t *testing.T) {
	for _, want := range []struct {
		name     string
		terminal bool
		secret   string
		err      error
		refuse   string
	}{
		{"a terminal", true, "hunter2", nil, ""},
		{"anything else", false, "", nil, "needs a terminal"},
		{"a terminal that would not say", true, "", errors.New("closed"), "read the password"},
	} {
		t.Run(want.name, func(t *testing.T) {
			c, written := console4Test(want.terminal, want.secret, want.err)
			got, err := asked(c, "password: ")
			if want.refuse != "" {
				if err == nil || !strings.Contains(err.Error(), want.refuse) {
					t.Fatalf("asked = %q, %v, want a refusal saying %q", got, err, want.refuse)
				}
				return
			}
			if err != nil {
				t.Fatalf("asked: %v", err)
			}
			if string(got) != want.secret {
				t.Errorf("asked = %q, want %q", got, want.secret)
			}
			if !strings.Contains(written.String(), "password: ") {
				t.Errorf("the prompt must be written where the answer is not, got %q", written)
			}
		})
	}
}

// A vault that will open on an empty passphrase is a vault, and the mistake is
// worth naming rather than letting the decryption fail confusingly later.
func TestAnEmptyVaultPassphraseIsRefused(t *testing.T) {
	for _, want := range []struct {
		name   string
		typed  string
		refuse bool
	}{
		{"a passphrase", "correct horse", false},
		{"nothing", "", true},
		{"nothing but space", "   \t", true},
	} {
		t.Run(want.name, func(t *testing.T) {
			c, _ := console4Test(true, want.typed, nil)
			got, err := passphrase(c)
			if want.refuse {
				if err == nil || !strings.Contains(err.Error(), "cannot be empty") {
					t.Fatalf("passphrase = %q, %v, want a refusal", got, err)
				}
				return
			}
			if err != nil || string(got) != want.typed {
				t.Errorf("passphrase = %q, %v", got, err)
			}
		})
	}
}

// Both entry points reach for the real terminal, which a test does not have.
func TestThePromptsReachForTheRealTerminal(t *testing.T) {
	if _, err := Prompt("password: "); err == nil {
		t.Error("a test has no terminal, so this must refuse")
	}
	if _, err := PassphrasePrompt(); err == nil {
		t.Error("a test has no terminal, so this must refuse")
	}
}
