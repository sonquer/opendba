package cli

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/sonquer/opendba/src/cli/internal/ai"
	"github.com/sonquer/opendba/src/cli/internal/ai/providers"
	"github.com/sonquer/opendba/src/cli/internal/ai/providers/local"
	"github.com/sonquer/opendba/src/cli/internal/ai/providers/local/llama"
	"github.com/sonquer/opendba/src/cli/internal/config"
	"github.com/sonquer/opendba/src/cli/pkg/secretref"
)

// Assistant is everything the conversation screen needs, gathered when the
// session opened.
type Assistant struct {
	Enabled  bool
	Instance ai.Instance
	Registry *providers.Registry
	Library  *local.Library
	Models   *local.Store
	Settings config.AISettings
	Trouble  string

	// Log is where the inference library writes what it is doing, which is the
	// only account of itself it leaves when it ends the process.
	Log string

	// Token is the resolved Hugging Face token, for the repositories that want one.
	Token []byte
}

// NewAssistant gathers what the conversation needs.
func NewAssistant(ctx context.Context, paths config.Paths, settings config.Settings, secrets *secretref.Store) Assistant {
	models := local.NewStore(paths.ModelsDir())
	assistant := Assistant{
		Registry: providers.All(models),
		Library:  local.NewLibrary(paths.LibDir()),
		Models:   models,
		Settings: settings.AI,
		Log:      paths.EngineLog(),
	}
	if settings.AI.Token != "" && secrets != nil {
		if reference, err := secretref.Parse(settings.AI.Token); err == nil {
			assistant.Token, _ = secrets.Get(ctx, reference)
		}
	}
	if !settings.AI.Enabled {
		return assistant
	}
	instance, err := AIInstance(ctx, settings, secrets, "")
	if err != nil {
		assistant.Trouble = err.Error()
		return assistant
	}
	assistant.Enabled, assistant.Instance = true, instance
	return assistant
}

// Open builds a client for the instance in use, which is where a local model is
// read off the disk and a key is first put on the wire.
func (a Assistant) Open() (ai.Client, error) {
	if !a.Enabled {
		if a.Trouble != "" {
			return nil, fmt.Errorf("%s", a.Trouble)
		}
		return nil, fmt.Errorf("the assistant is switched off in settings.toml")
	}
	return a.Registry.Open(a.Instance, ai.Deps{
		HTTP:   &http.Client{Timeout: replyTimeout},
		Engine: llama.New(a.Library.Dir()).LogTo(a.Log),
	})
}

// Memory is what the inference library reports for the largest device it found,
// which is the number a model has to fit in: the video memory of a graphics
// card, or the unified memory of a machine that has one pool for both.
func (a Assistant) Memory() int64 {
	if a.Library == nil || !a.Library.Present() {
		return 0
	}
	largest := int64(0)
	for _, device := range llama.New(a.Library.Dir()).LogTo(a.Log).Devices() {
		if device.TotalBytes > largest {
			largest = device.TotalBytes
		}
	}
	return largest
}

// Downloader is the client a download uses.
func Downloader() *http.Client { return &http.Client{CheckRedirect: local.KeepTokenHome} }

// replyTimeout is how long a whole answer may take. It is generous because a
// model that is thinking is not a model that has stopped.
const replyTimeout = 10 * time.Minute

// AIInstance resolves a configured instance into one that can be opened,
// fetching the key from wherever the profile says it is kept.
func AIInstance(ctx context.Context, settings config.Settings, secrets *secretref.Store, name string) (ai.Instance, error) {
	if name == "" {
		name = settings.AI.Active
	}
	configured, ok := settings.AI.Instance(name)
	if !ok {
		return ai.Instance{}, fmt.Errorf("no ai instance named %q is configured", name)
	}
	instance := ai.Instance{
		Name:     configured.Name,
		Kind:     ai.Kind(configured.Kind),
		Model:    configured.Model,
		Endpoint: configured.Endpoint,
		Context:  configured.Context,
		Thinking: configured.Thinking,
	}
	if instance.Context == 0 {
		instance.Context = settings.AI.Context
	}
	if configured.Key == "" {
		return instance, nil
	}
	key, err := aiKey(ctx, secrets, configured)
	if err != nil {
		return ai.Instance{}, err
	}
	instance.Key = key
	return instance, nil
}

func aiKey(ctx context.Context, secrets *secretref.Store, configured config.AIInstance) ([]byte, error) {
	if secrets == nil {
		return nil, fmt.Errorf("the key of %q cannot be read without a secret store", configured.Name)
	}
	reference, err := secretref.Parse(configured.Key)
	if err != nil {
		return nil, fmt.Errorf("the key of %q: %w", configured.Name, err)
	}
	key, err := secrets.Get(ctx, reference)
	if err != nil {
		return nil, fmt.Errorf("read the key of %q: %w", configured.Name, err)
	}
	return key, nil
}

// Hosted is a back-end somebody else runs: what it is called, what it answers
// with by default, and the environment variable people already keep its key in.
type Hosted struct {
	Kind  ai.Kind
	Title string
	Model string
	Env   string
	Note  string
}

// Hosts are the back-ends that are reached over the network, in the order a list
// should offer them.
func Hosts() []Hosted {
	return []Hosted{
		{Kind: ai.KindAnthropic, Title: "Anthropic", Model: "claude-sonnet-5", Env: "ANTHROPIC_API_KEY", Note: "needs a key"},
		{Kind: ai.KindOpenAI, Title: "OpenAI", Model: "gpt-5", Env: "OPENAI_API_KEY", Note: "needs a key"},
		{Kind: ai.KindGemini, Title: "Gemini", Model: "gemini-3-pro", Env: "GEMINI_API_KEY", Note: "needs a key"},
		{Kind: ai.KindOllama, Title: "Ollama", Note: "a daemon you run yourself"},
	}
}

// FromEnvironment reports the key a host is already configured with, as a
// reference rather than as the key.
func FromEnvironment(host Hosted, look func(string) string) (string, bool) {
	if host.Env == "" || look == nil {
		return "", false
	}
	if look(host.Env) == "" {
		return "", false
	}
	return "env:" + host.Env, true
}

// AddInstance writes an instance into the settings and makes it the one that
// answers.
func AddInstance(store config.Store, settings config.Settings, instance config.AIInstance) (config.Settings, error) {
	if instance.Name == "" {
		return settings, fmt.Errorf("an instance needs a name")
	}
	settings.AI.Enabled = true
	settings.AI.Active = instance.Name
	settings.AI.Instances = replaced(settings.AI.Instances, instance)
	if err := store.SaveSettings(settings); err != nil {
		return settings, err
	}
	return settings, nil
}

// Activate makes an instance that is already configured the one that answers.
func Activate(store config.Store, settings config.Settings, name string) (config.Settings, error) {
	if _, ok := settings.AI.Instance(name); !ok {
		return settings, fmt.Errorf("no ai instance named %q is configured", name)
	}
	settings.AI.Enabled = true
	settings.AI.Active = name
	if err := store.SaveSettings(settings); err != nil {
		return settings, err
	}
	return settings, nil
}

func replaced(instances []config.AIInstance, instance config.AIInstance) []config.AIInstance {
	for i, existing := range instances {
		if existing.Name == instance.Name {
			instances[i] = instance
			return instances
		}
	}
	return append(instances, instance)
}

// InstanceName is what an instance built from a host or a model is called.
func InstanceName(kind ai.Kind, model string) string {
	if kind == ai.KindLocal {
		return model
	}
	return string(kind)
}
