package driver

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/sonquer/opendba/src/cli/pkg/sqlguard"
)

type stubDriver struct{ name string }

func (s stubDriver) Name() string { return s.name }

func (s stubDriver) Title() string { return "Stub " + s.name }

func (s stubDriver) Capabilities() Capabilities { return Capabilities{Explain: true} }

func (s stubDriver) Connect(context.Context, Config) (Conn, error) { return nil, nil }

func TestConfigTarget(t *testing.T) {
	cases := []struct {
		name   string
		config Config
		want   string
	}{
		{"file", Config{File: "/tmp/app.db"}, "/tmp/app.db"},
		{"host and database", Config{Host: "db.example.com", Port: 5432, Database: "app"}, "db.example.com:5432/app"},
		{"no port", Config{Host: "db.example.com", Database: "app"}, "db.example.com/app"},
		{"no database", Config{Host: "db.example.com", Port: 5432}, "db.example.com:5432"},
		{"nothing", Config{}, "localhost"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.config.Target(); got != c.want {
				t.Errorf("Target() = %q, want %q", got, c.want)
			}
		})
	}
}

func TestConfigReadOnly(t *testing.T) {
	if !(Config{Mode: sqlguard.ModeReadOnly}).ReadOnly() {
		t.Error("read only mode must be read only")
	}
	if (Config{Mode: sqlguard.ModeReadWrite}).ReadOnly() == true {
		t.Error("read write mode must not be read only")
	}
	if !(Config{}).ReadOnly() {
		t.Error("an unset mode must be treated as read only")
	}
}

func TestDefaultTimeouts(t *testing.T) {
	timeouts := DefaultTimeouts()
	if timeouts.Statement != 15*time.Second || timeouts.Lock != 2*time.Second {
		t.Errorf("timeouts = %+v", timeouts)
	}
	if timeouts.Idle == 0 || timeouts.Connect == 0 {
		t.Errorf("every timeout must be set: %+v", timeouts)
	}
}

func TestTableQualified(t *testing.T) {
	if got := (Table{Schema: "public", Name: "users"}).Qualified(); got != "public.users" {
		t.Errorf("Qualified() = %q", got)
	}
	if got := (Table{Name: "users"}).Qualified(); got != "users" {
		t.Errorf("Qualified() = %q", got)
	}
}

func TestRegistry(t *testing.T) {
	registry := NewRegistry()
	registry.Register(stubDriver{name: "postgres"})
	registry.Register(stubDriver{name: "sqlite"})

	entries := registry.Entries()
	if len(entries) != 2 {
		t.Fatalf("entries = %+v", entries)
	}
	if entries[0].Name != "postgres" || entries[1].Name != "sqlite" {
		t.Errorf("registration order must be kept: %+v", entries)
	}
	if entries[0].Title != "Stub postgres" {
		t.Errorf("title = %q", entries[0].Title)
	}
	if names := registry.Names(); names[0] != "postgres" || names[1] != "sqlite" {
		t.Errorf("Names() must be sorted: %v", names)
	}

	found, err := registry.Get("POSTGRES")
	if err != nil || found.Name() != "postgres" {
		t.Fatalf("Get() = %v, %v", found, err)
	}
	_, err = registry.Get("mysql")
	if err == nil {
		t.Fatal("a driver that does not exist must be an error")
	}
	if !strings.Contains(err.Error(), "postgres, sqlite") {
		t.Errorf("the error must list what is there: %v", err)
	}
}

func TestRegisteringTwiceReplacesTheEntry(t *testing.T) {
	registry := NewRegistry()
	registry.Register(stubDriver{name: "sqlite"})
	registry.Register(stubDriver{name: "sqlite"})

	if entries := registry.Entries(); len(entries) != 1 {
		t.Fatalf("entries = %+v", entries)
	}
}
