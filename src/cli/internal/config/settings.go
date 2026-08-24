package config

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/sonquer/opendba/src/cli/pkg/secretref"
)

type SafetySettings struct {
	DefaultMode    AccessMode `toml:"default_mode"`
	ConfirmQueries bool       `toml:"confirm_queries"`
	QueryTimeout   string     `toml:"query_timeout"`
	LockTimeout    string     `toml:"lock_timeout"`
	RowLimit       int        `toml:"row_limit"`
	SlowQuery      string     `toml:"slow_query"`
	StuckQuery     string     `toml:"stuck_query"`
}

type AppearanceSettings struct {
	Theme  string `toml:"theme"`
	Accent string `toml:"accent"`

	// Bar is the shape a measurement is drawn with.
	Bar string `toml:"bar,omitempty"`

	// Mouse is whether the program asks the terminal to report the mouse.
	Mouse string `toml:"mouse,omitempty"`

	// OwnSessions is whether the dashboard draws the connections this program made.
	OwnSessions bool `toml:"own_sessions,omitempty"`
}

// MouseWanted reports whether the terminal should be asked for the mouse.
func (a AppearanceSettings) MouseWanted() bool { return a.Mouse != MouseOff }

// The two answers to whether the program takes the mouse.
const (
	MouseOn  = "on"
	MouseOff = "off"
)

type HistorySettings struct {
	Enabled  bool `toml:"enabled"`
	StoreSQL bool `toml:"store_sql"`
	Limit    int  `toml:"limit"`
}

// ChatSettings is what becomes of a conversation with the assistant once it is
// over.
type ChatSettings struct {
	Enabled bool `toml:"enabled"`
	Limit   int  `toml:"limit"`
}

// AIInstance is one configured way to reach a model: which back-end, which
// model, and where the key is kept.
type AIInstance struct {
	Name     string `toml:"name"`
	Kind     string `toml:"kind"`
	Model    string `toml:"model"`
	Endpoint string `toml:"endpoint,omitempty"`
	Key      string `toml:"key,omitempty"`
	Context  int    `toml:"context,omitempty"`
	Thinking bool   `toml:"thinking,omitempty"`
}

type AISettings struct {
	Enabled   bool         `toml:"enabled"`
	Active    string       `toml:"active,omitempty"`
	Context   int          `toml:"context,omitempty"`
	Instances []AIInstance `toml:"instance,omitempty"`

	// Token is a reference to a Hugging Face token, for the repositories that
	// want one. Like every other key here it is a reference and never a secret.
	Token string `toml:"token,omitempty"`

	// Provider, Model and Endpoint are what this section held before it could
	// describe more than one instance.
	Provider string `toml:"provider"`
	Model    string `toml:"model"`
	Endpoint string `toml:"endpoint,omitempty"`
}

// KnownKinds are the back-ends an instance may name.
var KnownKinds = []string{"anthropic", "openai", "gemini", "ollama", "compatible", "local"}

// KnownSecretSchemes are the ways a key may be kept.
var KnownSecretSchemes = []string{"keyring", "vault", "env", "command"}

type Settings struct {
	Appearance AppearanceSettings `toml:"appearance"`
	Safety     SafetySettings     `toml:"safety"`
	Workspace  WorkspaceSettings  `toml:"workspace"`
	History    HistorySettings    `toml:"history"`
	Chats      ChatSettings       `toml:"chats"`
	AI         AISettings         `toml:"ai"`
}

// WorkspaceSettings is where the statements you keep are kept.
type WorkspaceSettings struct {
	// Root is the directory the files list reads, with one of its own under it
	// per connection. Empty means the data directory, worked out at startup.
	Root string `toml:"root,omitempty"`
}

func DefaultSettings() Settings {
	return Settings{
		Appearance: AppearanceSettings{
			Theme: "dark", Accent: "cyan", Bar: "pipes", Mouse: MouseOn,
		},
		Safety: SafetySettings{
			DefaultMode:    ReadOnly,
			ConfirmQueries: true,
			QueryTimeout:   "15s",
			LockTimeout:    "2s",
			RowLimit:       1000,
			SlowQuery:      "30s",
			StuckQuery:     "5m",
		},
		History: HistorySettings{Enabled: true, StoreSQL: true, Limit: 500},
		Chats:   ChatSettings{Enabled: true, Limit: 100},
		AI: AISettings{
			Enabled:  false,
			Provider: "local",
			Model:    "gemma-4-e4b-qat",
			Context:  8192,
		}.migrated(),
	}
}

func (s Settings) Validate() error {
	if !s.Safety.DefaultMode.Valid() {
		return fmt.Errorf("unknown default access mode %q", s.Safety.DefaultMode)
	}
	if s.Safety.RowLimit < 1 {
		return fmt.Errorf("row limit must be positive, got %d", s.Safety.RowLimit)
	}
	if s.History.Limit < 0 {
		return fmt.Errorf("history limit cannot be negative, got %d", s.History.Limit)
	}
	if s.Chats.Limit < 0 {
		return fmt.Errorf("conversation limit cannot be negative, got %d", s.Chats.Limit)
	}
	if s.Workspace.Root != "" && !filepath.IsAbs(s.Workspace.Root) {
		return fmt.Errorf("the workspace root must be a full path, got %q", s.Workspace.Root)
	}
	return s.AI.validate()
}

func (a AISettings) validate() error {
	if a.Context < 0 {
		return fmt.Errorf("ai context cannot be negative, got %d", a.Context)
	}
	seen := map[string]bool{}
	for _, instance := range a.Instances {
		if err := instance.validate(); err != nil {
			return err
		}
		if seen[instance.Name] {
			return fmt.Errorf("two ai instances are both named %q", instance.Name)
		}
		seen[instance.Name] = true
	}
	if err := reference("the hugging face token", a.Token); err != nil {
		return err
	}
	if a.Active != "" && len(a.Instances) > 0 && !seen[a.Active] {
		return fmt.Errorf("the active ai instance %q is not configured", a.Active)
	}
	return nil
}

func (i AIInstance) validate() error {
	if strings.TrimSpace(i.Name) == "" {
		return fmt.Errorf("an ai instance needs a name")
	}
	if !slices.Contains(KnownKinds, i.Kind) {
		return fmt.Errorf("ai instance %q names an unknown back-end %q, have %s",
			i.Name, i.Kind, strings.Join(KnownKinds, ", "))
	}
	if i.Context < 0 {
		return fmt.Errorf("ai instance %q cannot have a negative context, got %d", i.Name, i.Context)
	}
	return i.validateKey()
}

// validateKey refuses anything that is not a reference to one of the secret
// backends, which is what stops a key from being written into the file.
func (i AIInstance) validateKey() error {
	return reference(fmt.Sprintf("the key of ai instance %q", i.Name), i.Key)
}

// reference refuses a secret written into the file instead of a pointer to one.
func reference(what, written string) error {
	if written == "" {
		return nil
	}
	parsed, err := secretref.Parse(written)
	if err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	if !slices.Contains(KnownSecretSchemes, parsed.Scheme) {
		return fmt.Errorf("%s must be kept in one of %s, not in this file",
			what, strings.Join(KnownSecretSchemes, ", "))
	}
	return nil
}

func (s Settings) withDefaults() Settings {
	defaults := DefaultSettings()
	if s.Appearance.Theme == "" {
		s.Appearance.Theme = defaults.Appearance.Theme
	}
	if s.Appearance.Accent == "" {
		s.Appearance.Accent = defaults.Appearance.Accent
	}
	if s.Safety.DefaultMode == "" {
		s.Safety.DefaultMode = defaults.Safety.DefaultMode
	}
	if s.Safety.QueryTimeout == "" {
		s.Safety.QueryTimeout = defaults.Safety.QueryTimeout
	}
	if s.Safety.LockTimeout == "" {
		s.Safety.LockTimeout = defaults.Safety.LockTimeout
	}
	if s.Safety.SlowQuery == "" {
		s.Safety.SlowQuery = defaults.Safety.SlowQuery
	}
	if s.Safety.StuckQuery == "" {
		s.Safety.StuckQuery = defaults.Safety.StuckQuery
	}
	if s.Safety.RowLimit == 0 {
		s.Safety.RowLimit = defaults.Safety.RowLimit
	}
	if s.AI.Provider == "" {
		s.AI.Provider = defaults.AI.Provider
	}
	if s.AI.Context == 0 {
		s.AI.Context = defaults.AI.Context
	}
	s.AI = s.AI.migrated()
	return s
}

// migrated folds the single back-end an older file could describe into the list
// of instances, and settles which of them is the active one.
func (a AISettings) migrated() AISettings {
	if len(a.Instances) == 0 && a.Provider != "" {
		a.Instances = []AIInstance{{
			Name:     a.Provider,
			Kind:     a.Provider,
			Model:    a.Model,
			Endpoint: a.Endpoint,
		}}
	}
	if a.Active == "" && len(a.Instances) > 0 {
		a.Active = a.Instances[0].Name
	}
	return a
}

// Instance returns the instance a name belongs to.
func (a AISettings) Instance(name string) (AIInstance, bool) {
	for _, instance := range a.Instances {
		if instance.Name == name {
			return instance, true
		}
	}
	return AIInstance{}, false
}

// Chosen returns the instance that is in use.
func (a AISettings) Chosen() (AIInstance, bool) { return a.Instance(a.Active) }
