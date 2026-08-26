package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAIMigratesAnOlderFile(t *testing.T) {
	store := newStore(t)
	written := "[ai]\n  enabled = true\n  provider = \"ollama\"\n  model = \"qwen3.5:9b\"\n  endpoint = \"http://box:11434\"\n"
	if err := os.WriteFile(store.Paths.SettingsFile(), []byte(written), 0o600); err != nil {
		t.Fatal(err)
	}
	settings, err := store.LoadSettings()
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if len(settings.AI.Instances) != 1 {
		t.Fatalf("instances = %+v, want the old fields folded into one", settings.AI.Instances)
	}
	instance := settings.AI.Instances[0]
	if instance.Name != "ollama" || instance.Kind != "ollama" || instance.Model != "qwen3.5:9b" {
		t.Fatalf("instance = %+v", instance)
	}
	if instance.Endpoint != "http://box:11434" {
		t.Fatalf("endpoint = %q, want the one that was configured", instance.Endpoint)
	}
	if settings.AI.Active != "ollama" {
		t.Fatalf("active = %q, want the only instance", settings.AI.Active)
	}
}

func TestAIKeepsInstancesOverTheOlderFields(t *testing.T) {
	store := newStore(t)
	written := strings.Join([]string{
		"[ai]",
		`  enabled = true`,
		`  provider = "local"`,
		`  active = "claude"`,
		"",
		"[[ai.instance]]",
		`  name = "claude"`,
		`  kind = "anthropic"`,
		`  model = "claude-sonnet-5"`,
		`  key = "keyring:ai-claude"`,
		"",
		"[[ai.instance]]",
		`  name = "here"`,
		`  kind = "local"`,
		`  model = "gemma-4-e4b-qat"`,
		"",
	}, "\n")
	if err := os.WriteFile(store.Paths.SettingsFile(), []byte(written), 0o600); err != nil {
		t.Fatal(err)
	}
	settings, err := store.LoadSettings()
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if len(settings.AI.Instances) != 2 {
		t.Fatalf("instances = %+v, want both and nothing folded in", settings.AI.Instances)
	}
	chosen, ok := settings.AI.Chosen()
	if !ok || chosen.Kind != "anthropic" || chosen.Key != "keyring:ai-claude" {
		t.Fatalf("Chosen() = %+v, %v", chosen, ok)
	}
	if _, ok := settings.AI.Instance("nobody"); ok {
		t.Fatal("Instance() found one that was never configured")
	}
}

func TestAIRoundTrip(t *testing.T) {
	store := newStore(t)
	settings := DefaultSettings()
	settings.AI.Enabled = true
	settings.AI.Instances = []AIInstance{
		{Name: "claude", Kind: "anthropic", Model: "claude-sonnet-5", Key: "keyring:ai-claude", Context: 200000},
		{Name: "here", Kind: "local", Model: "gemma-4-e4b-qat", Thinking: true},
	}
	settings.AI.Active = "here"
	if err := store.SaveSettings(settings); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
	written, err := os.ReadFile(store.Paths.SettingsFile())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(written), "[[ai.instance]]") {
		t.Fatalf("the instances were not written as their own tables:\n%s", written)
	}
	loaded, err := store.LoadSettings()
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if len(loaded.AI.Instances) != 2 || loaded.AI.Active != "here" {
		t.Fatalf("AI = %+v", loaded.AI)
	}
	if loaded.AI.Instances[0].Context != 200000 || !loaded.AI.Instances[1].Thinking {
		t.Fatalf("instances = %+v, want every field kept", loaded.AI.Instances)
	}
}

func TestAIValidation(t *testing.T) {
	cases := map[string]struct {
		ai   AISettings
		want string
	}{
		"a key written into the file": {
			ai:   AISettings{Instances: []AIInstance{{Name: "claude", Kind: "anthropic", Key: "sk-ant-api03-secret"}}},
			want: "must be kept in one of",
		},
		"a key under an unknown scheme": {
			ai:   AISettings{Instances: []AIInstance{{Name: "claude", Kind: "anthropic", Key: "s3:bucket/key"}}},
			want: "must be kept in one of",
		},
		"a key reference with nothing after the scheme": {
			ai:   AISettings{Instances: []AIInstance{{Name: "claude", Kind: "anthropic", Key: "keyring:"}}},
			want: "has no value",
		},
		"a hugging face token written into the file": {
			ai:   AISettings{Token: "hf_a_real_token"},
			want: "hugging face token must be kept",
		},
		"an instance with no name": {
			ai:   AISettings{Instances: []AIInstance{{Kind: "anthropic"}}},
			want: "needs a name",
		},
		"an unknown back-end": {
			ai:   AISettings{Instances: []AIInstance{{Name: "x", Kind: "cohere"}}},
			want: "unknown back-end",
		},
		"two instances with one name": {
			ai: AISettings{Instances: []AIInstance{
				{Name: "claude", Kind: "anthropic"},
				{Name: "claude", Kind: "openai"},
			}},
			want: "both named",
		},
		"an active instance that is not there": {
			ai: AISettings{
				Active:    "gemini",
				Instances: []AIInstance{{Name: "claude", Kind: "anthropic"}},
			},
			want: "not configured",
		},
		"a negative context": {
			ai:   AISettings{Context: -1},
			want: "cannot be negative",
		},
		"a negative context on an instance": {
			ai:   AISettings{Instances: []AIInstance{{Name: "claude", Kind: "anthropic", Context: -1}}},
			want: "negative context",
		},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			settings := DefaultSettings()
			settings.AI = test.ai
			err := settings.Validate()
			if err == nil {
				t.Fatalf("Validate() accepted %+v", test.ai)
			}
			if !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %q, want it to mention %q", err, test.want)
			}
		})
	}
}

func TestAIValidationAcceptsEverySecretScheme(t *testing.T) {
	for _, scheme := range KnownSecretSchemes {
		t.Run(scheme, func(t *testing.T) {
			settings := DefaultSettings()
			settings.AI = AISettings{Instances: []AIInstance{{
				Name: "claude", Kind: "anthropic", Key: scheme + ":something",
			}}}
			if err := settings.Validate(); err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
		})
	}
}

func TestAIValidationAcceptsEveryKind(t *testing.T) {
	for _, kind := range KnownKinds {
		t.Run(kind, func(t *testing.T) {
			settings := DefaultSettings()
			settings.AI = AISettings{Instances: []AIInstance{{Name: "x", Kind: kind}}}
			if err := settings.Validate(); err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
		})
	}
}

func TestAISettingsRefusedOnLoad(t *testing.T) {
	store := newStore(t)
	written := "[[ai.instance]]\n  name = \"claude\"\n  kind = \"anthropic\"\n  key = \"sk-ant-secret\"\n"
	if err := os.WriteFile(store.Paths.SettingsFile(), []byte(written), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadSettings(); err == nil {
		t.Fatal("LoadSettings must refuse a file with a key written into it")
	}
}

func TestDataPaths(t *testing.T) {
	paths := Paths{Data: filepath.Join("/x", "data", "opendba")}
	if got := paths.ModelsDir(); got != filepath.Join("/x", "data", "opendba", "models") {
		t.Fatalf("ModelsDir() = %q", got)
	}
	if got := paths.LibDir(); got != filepath.Join("/x", "data", "opendba", "lib") {
		t.Fatalf("LibDir() = %q", got)
	}
}

func TestPathsNeedAHomeForData(t *testing.T) {
	_, err := PathsFor(envFrom(map[string]string{
		"XDG_CONFIG_HOME": "/x/config",
		"XDG_STATE_HOME":  "/x/state",
	}))
	if err == nil || !strings.Contains(err.Error(), "data directory") {
		t.Fatalf("PathsFor() error = %v, want it to name the data directory", err)
	}
}

func TestEnsureSkipsADirectoryNobodyAskedFor(t *testing.T) {
	root := t.TempDir()
	paths := Paths{Config: filepath.Join(root, "config")}
	if err := paths.Ensure(); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	if _, err := os.Stat(paths.Config); err != nil {
		t.Fatalf("the directory that was asked for is missing: %v", err)
	}
}

func TestAIAcceptsATokenReference(t *testing.T) {
	settings := DefaultSettings()
	settings.AI.Token = "env:HF_TOKEN"
	if err := settings.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}
