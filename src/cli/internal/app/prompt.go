package app

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"
)

func Prompt(prompt string) ([]byte, error) {
	descriptor := int(os.Stdin.Fd())
	if !term.IsTerminal(descriptor) {
		return nil, fmt.Errorf("reading a password needs a terminal")
	}
	fmt.Fprint(os.Stderr, prompt)
	secret, err := term.ReadPassword(descriptor)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return nil, fmt.Errorf("read the password: %w", err)
	}
	return secret, nil
}

func PassphrasePrompt() ([]byte, error) {
	secret, err := Prompt("vault passphrase: ")
	if err != nil {
		return nil, err
	}
	if len(strings.TrimSpace(string(secret))) == 0 {
		return nil, fmt.Errorf("the vault passphrase cannot be empty")
	}
	return secret, nil
}
