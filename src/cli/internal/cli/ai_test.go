package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/sonquer/tui4db/src/cli/internal/ai"
	"github.com/sonquer/tui4db/src/cli/internal/config"
	"github.com/sonquer/tui4db/src/cli/pkg/secretref"
)

func aiSettings() config.Settings {
	settings := config.DefaultSettings()
	settings.AI = config.AISettings{
		Enabled: true,
		Active:  "claude",
		Context: 16384,
		Instances: []config.AIInstance{
			{Name: "claude", Kind: "anthropic", Model: "claude-sonnet-5", Key: "env:TUI4DB_TEST_AI_KEY"},
			{Name: "here", Kind: "local", Model: "gemma-4-e4b-qat", Context: 4096, Thinking: true},
			{Name: "keyless", Kind: "ollama", Model: "qwen3.5:9b"},
		},
	}
	return settings
}

func secrets(t *testing.T) *secretref.Store {
	t.Helper()
	return secretref.NewStore(secretref.NewEnvBackend())
}

func TestAIInstance(t *testing.T) {
	t.Setenv("TUI4DB_TEST_AI_KEY", "sk-ant-from-the-environment")
	settings := aiSettings()

	chosen, err := AIInstance(context.Background(), settings, secrets(t), "")
	if err != nil {
		t.Fatalf("AIInstance() error = %v", err)
	}
	if chosen.Name != "claude" || chosen.Kind != ai.KindAnthropic {
		t.Fatalf("instance = %+v, want the active one", chosen)
	}
	if string(chosen.Key) != "sk-ant-from-the-environment" {
		t.Fatalf("key = %q, want the one the reference points at", chosen.Key)
	}
	if chosen.Context != 16384 {
		t.Fatalf("context = %d, want the one the section sets", chosen.Context)
	}
}

func TestAIInstanceByName(t *testing.T) {
	settings := aiSettings()
	chosen, err := AIInstance(context.Background(), settings, secrets(t), "here")
	if err != nil {
		t.Fatalf("AIInstance() error = %v", err)
	}
	if chosen.Kind != ai.KindLocal || chosen.Model != "gemma-4-e4b-qat" {
		t.Fatalf("instance = %+v", chosen)
	}
	if chosen.Context != 4096 {
		t.Fatalf("context = %d, want the instance's own over the section's", chosen.Context)
	}
	if !chosen.Thinking {
		t.Fatal("thinking was configured and lost")
	}
	if len(chosen.Key) != 0 {
		t.Fatal("an instance with no key was given one")
	}
}

func TestAIInstanceFailures(t *testing.T) {
	settings := aiSettings()
	cases := map[string]struct {
		name    string
		secrets *secretref.Store
		want    string
	}{
		"no instance of that name": {name: "gemini", secrets: secrets(t), want: "no ai instance named"},
		"no secret store":          {name: "claude", secrets: nil, want: "without a secret store"},
		"the key is not there":     {name: "claude", secrets: secrets(t), want: "read the key"},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := AIInstance(context.Background(), settings, test.secrets, test.name)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("AIInstance() error = %v, want it to mention %q", err, test.want)
			}
		})
	}
}

func TestAIInstanceRefusesAKeyItCannotRead(t *testing.T) {
	settings := aiSettings()
	settings.AI.Instances[0].Key = "  "
	_, err := AIInstance(context.Background(), settings, secrets(t), "claude")
	if err == nil || !strings.Contains(err.Error(), "the key of") {
		t.Fatalf("AIInstance() error = %v, want it to name the instance", err)
	}
}

func TestAIInstanceWithNothingConfigured(t *testing.T) {
	settings := config.DefaultSettings()
	settings.AI.Instances = nil
	settings.AI.Active = ""
	if _, err := AIInstance(context.Background(), settings, secrets(t), ""); err == nil {
		t.Fatal("AIInstance() found an instance where none is configured")
	}
}
