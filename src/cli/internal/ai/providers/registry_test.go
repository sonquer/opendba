package providers

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sonquer/tui4db/src/cli/internal/ai"
)

type stubClient struct {
	caps ai.Capabilities
}

func (s stubClient) Chat(context.Context, ai.Request) (ai.Stream, error) { return nil, nil }

func (s stubClient) Capabilities() ai.Capabilities { return s.caps }

type stubProvider struct {
	kind ai.Kind
	err  error
	caps ai.Capabilities
	seen ai.Instance
}

func (s *stubProvider) Kind() ai.Kind { return s.kind }

func (s *stubProvider) Open(instance ai.Instance, _ ai.Deps) (ai.Client, error) {
	s.seen = instance
	if s.err != nil {
		return nil, s.err
	}
	return stubClient{caps: s.caps}, nil
}

func TestRegistryOpen(t *testing.T) {
	registry := NewRegistry()
	provider := &stubProvider{kind: ai.KindAnthropic, caps: ai.Capabilities{Tools: true, Context: 200000}}
	registry.Register(provider)

	client, err := registry.Open(ai.Instance{Name: "claude", Kind: ai.KindAnthropic, Model: "sonnet"}, ai.Deps{})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if !client.Capabilities().Tools {
		t.Fatal("the registry returned a client without the provider's capabilities")
	}
	if provider.seen.Model != "sonnet" {
		t.Fatalf("the provider was given %q, want sonnet", provider.seen.Model)
	}
}

func TestRegistryUnknownKind(t *testing.T) {
	registry := NewRegistry()
	registry.Register(&stubProvider{kind: ai.KindOpenAI})
	registry.Register(&stubProvider{kind: ai.KindLocal})

	_, err := registry.Open(ai.Instance{Name: "work", Kind: ai.KindGemini}, ai.Deps{})
	if err == nil {
		t.Fatal("opening an unregistered kind must fail")
	}
	for _, want := range []string{"gemini", "openai", "local"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not name %q", err, want)
		}
	}
}

func TestRegistryOpenFailure(t *testing.T) {
	broken := errors.New("no api key")
	registry := NewRegistry()
	registry.Register(&stubProvider{kind: ai.KindOpenAI, err: broken})

	_, err := registry.Open(ai.Instance{Name: "work", Kind: ai.KindOpenAI}, ai.Deps{})
	if !errors.Is(err, broken) {
		t.Fatalf("Open() error = %v, want it to wrap %v", err, broken)
	}
	if !strings.Contains(err.Error(), "work") {
		t.Fatalf("error %q does not name the instance", err)
	}
}

func TestRegistryOrder(t *testing.T) {
	registry := NewRegistry()
	registry.Register(&stubProvider{kind: ai.KindAnthropic})
	registry.Register(&stubProvider{kind: ai.KindOpenAI})
	replacement := &stubProvider{kind: ai.KindAnthropic, caps: ai.Capabilities{Grammar: true}}
	registry.Register(replacement)

	kinds := registry.Kinds()
	if len(kinds) != 2 || kinds[0] != ai.KindAnthropic || kinds[1] != ai.KindOpenAI {
		t.Fatalf("Kinds() = %v, want registration order kept and no duplicate", kinds)
	}
	kinds[0] = ai.KindLocal
	if registry.Kinds()[0] != ai.KindAnthropic {
		t.Fatal("Kinds() handed out the registry's own slice")
	}
	provider, err := registry.Get(ai.KindAnthropic)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if provider != ai.Provider(replacement) {
		t.Fatal("registering the same kind again did not replace the provider")
	}
	if names := registry.Names(); len(names) != 2 || names[0] != "anthropic" {
		t.Fatalf("Names() = %v", names)
	}
}

func TestAllHasEveryBackEnd(t *testing.T) {
	registry := All(nil)
	for _, kind := range []ai.Kind{
		ai.KindAnthropic, ai.KindOpenAI, ai.KindCompatible,
		ai.KindGemini, ai.KindOllama, ai.KindLocal,
	} {
		if _, err := registry.Get(kind); err != nil {
			t.Fatalf("the back-end %q is not registered", kind)
		}
	}
}
