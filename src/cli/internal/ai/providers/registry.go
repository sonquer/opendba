package providers

import (
	"fmt"
	"strings"

	"github.com/sonquer/tui4db/src/cli/internal/ai"
	"github.com/sonquer/tui4db/src/cli/internal/ai/providers/anthropic"
	"github.com/sonquer/tui4db/src/cli/internal/ai/providers/gemini"
	"github.com/sonquer/tui4db/src/cli/internal/ai/providers/local"
	"github.com/sonquer/tui4db/src/cli/internal/ai/providers/ollama"
	"github.com/sonquer/tui4db/src/cli/internal/ai/providers/openai"
)

// Registry is the factory. It maps a kind to the provider that opens it, and it
// is the only thing that knows every back-end, so no screen has to.
type Registry struct {
	entries map[ai.Kind]ai.Provider
	order   []ai.Kind
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry { return &Registry{entries: map[ai.Kind]ai.Provider{}} }

// Register adds a provider, replacing one already registered for the same kind
// without moving it in the order.
func (r *Registry) Register(provider ai.Provider) {
	kind := provider.Kind()
	if _, seen := r.entries[kind]; !seen {
		r.order = append(r.order, kind)
	}
	r.entries[kind] = provider
}

// Get returns the provider for a kind.
func (r *Registry) Get(kind ai.Kind) (ai.Provider, error) {
	provider, ok := r.entries[kind]
	if !ok {
		return nil, fmt.Errorf("no ai back-end named %q, have %s", kind, strings.Join(r.Names(), ", "))
	}
	return provider, nil
}

// Open builds the client for an instance.
func (r *Registry) Open(instance ai.Instance, deps ai.Deps) (ai.Client, error) {
	provider, err := r.Get(instance.Kind)
	if err != nil {
		return nil, err
	}
	client, err := provider.Open(instance, deps)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", instance.Name, err)
	}
	return client, nil
}

// Kinds lists the registered kinds in the order they were registered.
func (r *Registry) Kinds() []ai.Kind {
	kinds := make([]ai.Kind, len(r.order))
	copy(kinds, r.order)
	return kinds
}

// Names lists the registered kinds as strings, for a message.
func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.order))
	for _, kind := range r.order {
		names = append(names, string(kind))
	}
	return names
}

// All is every back-end this program can reach, assembled in one place. It is
// here rather than higher up because a factory belongs with the things it
// makes: adding a back-end is a package beside this one and a line in here, and
// no screen above learns a new name.
func All(models local.Models) *Registry {
	registry := NewRegistry()
	registry.Register(anthropic.New())
	registry.Register(openai.New())
	registry.Register(openai.Compatible())
	registry.Register(gemini.New())
	registry.Register(ollama.New())
	registry.Register(local.New(models))
	return registry
}
