package config

import "fmt"

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

	// Bar is the shape a measurement is drawn with. How well a glyph draws is
	// the font's business and a terminal program cannot choose the font it is
	// rendered in, so this is a choice rather than a constant.
	Bar string `toml:"bar,omitempty"`
}

type HistorySettings struct {
	Enabled  bool `toml:"enabled"`
	StoreSQL bool `toml:"store_sql"`
	Limit    int  `toml:"limit"`
}

type AISettings struct {
	Enabled  bool   `toml:"enabled"`
	Provider string `toml:"provider"`
	Model    string `toml:"model"`
	Endpoint string `toml:"endpoint,omitempty"`
}

type Settings struct {
	Appearance AppearanceSettings `toml:"appearance"`
	Safety     SafetySettings     `toml:"safety"`
	History    HistorySettings    `toml:"history"`
	AI         AISettings         `toml:"ai"`
}

func DefaultSettings() Settings {
	return Settings{
		Appearance: AppearanceSettings{Theme: "dark", Accent: "cyan", Bar: "pipes"},
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
		AI:      AISettings{Enabled: false, Provider: "local", Model: "gemma"},
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
	return s
}
