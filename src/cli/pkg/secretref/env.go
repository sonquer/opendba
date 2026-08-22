package secretref

import (
	"context"
	"fmt"
	"os"
)

// EnvBackend reads secrets from environment variables. It cannot write them.
type EnvBackend struct {
	Lookup func(string) (string, bool)
}

// NewEnvBackend returns a backend reading from the process environment.
func NewEnvBackend() EnvBackend { return EnvBackend{Lookup: os.LookupEnv} }

func (EnvBackend) Scheme() string { return SchemeEnv }

func (b EnvBackend) Get(_ context.Context, ref Ref) ([]byte, error) {
	lookup := b.Lookup
	if lookup == nil {
		lookup = os.LookupEnv
	}
	value, ok := lookup(ref.Value)
	if !ok {
		return nil, fmt.Errorf("%w: environment variable %s is not set", ErrNotFound, ref.Value)
	}
	return []byte(value), nil
}

func (b EnvBackend) Set(context.Context, Ref, []byte) error {
	return fmt.Errorf("environment variables are managed outside tui4db")
}

func (b EnvBackend) Delete(context.Context, Ref) error {
	return fmt.Errorf("environment variables are managed outside tui4db")
}
