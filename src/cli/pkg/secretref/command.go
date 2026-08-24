package secretref

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// CommandBackend reads a secret from the output of an external command, which is
// how opendba talks to password managers it does not need to know about.
type CommandBackend struct {
	Run func(ctx context.Context, name string, args ...string) ([]byte, error)
}

// NewCommandBackend returns a backend that runs commands with the os/exec package.
func NewCommandBackend() CommandBackend { return CommandBackend{Run: runCommand} }

func (CommandBackend) Scheme() string { return SchemeCommand }

func (b CommandBackend) Get(ctx context.Context, ref Ref) ([]byte, error) {
	fields := strings.Fields(ref.Value)
	if len(fields) == 0 {
		return nil, fmt.Errorf("secret command is empty")
	}
	run := b.Run
	if run == nil {
		run = runCommand
	}
	output, err := run(ctx, fields[0], fields[1:]...)
	if err != nil {
		return nil, fmt.Errorf("secret command %q failed: %w", fields[0], err)
	}
	secret := bytes.TrimRight(output, "\r\n")
	if len(secret) == 0 {
		return nil, fmt.Errorf("%w: secret command produced no output", ErrNotFound)
	}
	return secret, nil
}

func (b CommandBackend) Set(context.Context, Ref, []byte) error {
	return fmt.Errorf("secrets from a command are managed outside opendba")
}

func (b CommandBackend) Delete(context.Context, Ref) error {
	return fmt.Errorf("secrets from a command are managed outside opendba")
}

func runCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}
