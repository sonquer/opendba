package app

import (
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

// console is the three things reading a password needs from a terminal, taken
// as arguments so that the reading can be tested without one.
type console struct {
	descriptor int
	isTerminal func(int) bool
	read       func(int) ([]byte, error)
	out        io.Writer
}

func stdin() console {
	return console{
		descriptor: int(os.Stdin.Fd()),
		isTerminal: term.IsTerminal,
		read:       term.ReadPassword,
		out:        os.Stderr,
	}
}

func Prompt(prompt string) ([]byte, error) { return asked(stdin(), prompt) }

// asked writes the prompt where the answer will not be piped into anything, and
// reads the answer without echoing it.
func asked(c console, prompt string) ([]byte, error) {
	if !c.isTerminal(c.descriptor) {
		return nil, fmt.Errorf("reading a password needs a terminal")
	}
	fmt.Fprint(c.out, prompt)
	secret, err := c.read(c.descriptor)
	fmt.Fprintln(c.out)
	if err != nil {
		return nil, fmt.Errorf("read the password: %w", err)
	}
	return secret, nil
}

func PassphrasePrompt() ([]byte, error) { return passphrase(stdin()) }

func passphrase(c console) ([]byte, error) {
	secret, err := asked(c, "vault passphrase: ")
	if err != nil {
		return nil, err
	}
	if len(strings.TrimSpace(string(secret))) == 0 {
		return nil, fmt.Errorf("the vault passphrase cannot be empty")
	}
	return secret, nil
}
