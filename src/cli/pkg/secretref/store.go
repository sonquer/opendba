package secretref

import (
	"context"
	"fmt"
)

// Backend resolves references of a single scheme.
type Backend interface {
	Scheme() string
	Get(ctx context.Context, ref Ref) ([]byte, error)
	Set(ctx context.Context, ref Ref, secret []byte) error
	Delete(ctx context.Context, ref Ref) error
}

// Store routes references to the backend that owns their scheme.
type Store struct {
	backends map[string]Backend
}

// NewStore returns a store holding the given backends.
func NewStore(backends ...Backend) *Store {
	store := &Store{backends: map[string]Backend{}}
	for _, backend := range backends {
		store.Register(backend)
	}
	return store
}

// Register adds a backend, replacing any backend for the same scheme.
func (s *Store) Register(backend Backend) { s.backends[backend.Scheme()] = backend }

// Schemes returns the schemes the store can resolve.
func (s *Store) Schemes() []string {
	schemes := make([]string, 0, len(s.backends))
	for scheme := range s.backends {
		schemes = append(schemes, scheme)
	}
	return schemes
}

// Backend returns the backend for a scheme. Schemes that are resolved outside
// the store, such as prompt and pgpass, report ErrExternal.
func (s *Store) Backend(scheme string) (Backend, error) {
	if scheme == SchemePrompt || scheme == SchemePgpass {
		return nil, fmt.Errorf("%w: %s", ErrExternal, scheme)
	}
	backend, ok := s.backends[scheme]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrScheme, scheme)
	}
	return backend, nil
}

// Get resolves a reference to its secret.
func (s *Store) Get(ctx context.Context, ref Ref) ([]byte, error) {
	backend, err := s.Backend(ref.Scheme)
	if err != nil {
		return nil, err
	}
	return backend.Get(ctx, ref)
}

// Set stores a secret under a reference.
func (s *Store) Set(ctx context.Context, ref Ref, secret []byte) error {
	backend, err := s.Backend(ref.Scheme)
	if err != nil {
		return err
	}
	return backend.Set(ctx, ref, secret)
}

// Delete removes the secret a reference points at.
func (s *Store) Delete(ctx context.Context, ref Ref) error {
	backend, err := s.Backend(ref.Scheme)
	if err != nil {
		return err
	}
	return backend.Delete(ctx, ref)
}

// Zero overwrites a secret in memory once it is no longer needed.
func Zero(secret []byte) {
	for i := range secret {
		secret[i] = 0
	}
}
