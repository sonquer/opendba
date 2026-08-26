package config

import (
	"fmt"
	"strings"
)

type AccessMode string

const (
	ReadOnly  AccessMode = "readonly"
	ReadWrite AccessMode = "readwrite"
)

func (m AccessMode) Valid() bool { return m == ReadOnly || m == ReadWrite }

func (m AccessMode) Label() string {
	if m == ReadWrite {
		return "READ / WRITE"
	}
	return "READ ONLY"
}

type Connection struct {
	ID       string `toml:"id"`
	Name     string `toml:"name"`
	Driver   string `toml:"driver"`
	Host     string `toml:"host,omitempty"`
	Port     int    `toml:"port,omitempty"`
	Database string `toml:"database,omitempty"`
	User     string `toml:"user,omitempty"`
	SSLMode  string `toml:"sslmode,omitempty"`
	File     string `toml:"file,omitempty"`
	Options  string `toml:"options,omitempty"`

	DefaultSchema string   `toml:"default_schema,omitempty"`
	Schemas       []string `toml:"schemas,omitempty"`

	Mode   AccessMode `toml:"mode"`
	Color  string     `toml:"color"`
	Secret string     `toml:"secret,omitempty"`
}

func (c Connection) Schema() string {
	if strings.TrimSpace(c.DefaultSchema) == "" {
		return ""
	}
	return c.DefaultSchema
}

// Filter is the set of schemas this session looks at. None of them means every
// schema, which is what an untouched profile asks for.
func (c Connection) Filter() []string {
	kept := make([]string, 0, len(c.Schemas))
	for _, schema := range c.Schemas {
		if strings.TrimSpace(schema) != "" {
			kept = append(kept, schema)
		}
	}
	return kept
}

// Only reports whether a schema is one of the chosen, which an empty filter
// answers for every schema.
func (c Connection) Only(schema string) bool {
	filter := c.Filter()
	if len(filter) == 0 {
		return true
	}
	for _, chosen := range filter {
		if chosen == schema {
			return true
		}
	}
	return false
}

func (c Connection) Validate() error {
	switch {
	case strings.TrimSpace(c.ID) == "":
		return fmt.Errorf("connection has no id")
	case strings.TrimSpace(c.Name) == "":
		return fmt.Errorf("connection %s has no name", c.ID)
	case strings.TrimSpace(c.Driver) == "":
		return fmt.Errorf("connection %s has no driver", c.Name)
	case !c.Mode.Valid():
		return fmt.Errorf("connection %s has an unknown access mode %q", c.Name, c.Mode)
	}
	if strings.Contains(strings.ToLower(c.Options), "password") {
		return fmt.Errorf("connection %s stores a password in its options; move it to a secret backend", c.Name)
	}
	return nil
}

type Profiles struct {
	Connections []Connection `toml:"connection"`
}

func (p Profiles) Validate() error {
	seen := map[string]bool{}
	names := map[string]bool{}
	for _, c := range p.Connections {
		if err := c.Validate(); err != nil {
			return err
		}
		if seen[c.ID] {
			return fmt.Errorf("duplicate connection id %s", c.ID)
		}
		if names[strings.ToLower(c.Name)] {
			return fmt.Errorf("duplicate connection name %s", c.Name)
		}
		seen[c.ID] = true
		names[strings.ToLower(c.Name)] = true
	}
	return nil
}

func (p Profiles) Find(id string) (Connection, bool) {
	for _, c := range p.Connections {
		if c.ID == id {
			return c, true
		}
	}
	return Connection{}, false
}

func (p Profiles) ByName(name string) (Connection, bool) {
	for _, c := range p.Connections {
		if strings.EqualFold(c.Name, name) {
			return c, true
		}
	}
	return Connection{}, false
}

func (p *Profiles) Upsert(connection Connection) error {
	if err := connection.Validate(); err != nil {
		return err
	}
	for i, existing := range p.Connections {
		if existing.ID == connection.ID {
			p.Connections[i] = connection
			return p.Validate()
		}
	}
	p.Connections = append(p.Connections, connection)
	return p.Validate()
}

func (p *Profiles) Remove(id string) bool {
	for i, existing := range p.Connections {
		if existing.ID == id {
			p.Connections = append(p.Connections[:i], p.Connections[i+1:]...)
			return true
		}
	}
	return false
}

func (p Profiles) IsEmpty() bool { return len(p.Connections) == 0 }
