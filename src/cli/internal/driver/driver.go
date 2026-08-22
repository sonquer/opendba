package driver

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/sonquer/tui4db/src/cli/pkg/sqlguard"
)

type Capabilities struct {
	Explain         bool
	Relations       bool
	Health          bool
	IndexStats      bool
	ReadOnlySession bool
	Cancel          bool
}

type Timeouts struct {
	Statement time.Duration
	Lock      time.Duration
	Idle      time.Duration
	Connect   time.Duration
}

func DefaultTimeouts() Timeouts {
	return Timeouts{
		Statement: 15 * time.Second,
		Lock:      2 * time.Second,
		Idle:      30 * time.Second,
		Connect:   10 * time.Second,
	}
}

type Config struct {
	Host        string
	Port        int
	Database    string
	User        string
	Password    []byte
	SSLMode     string
	File        string
	Options     string
	Mode        sqlguard.Mode
	Application string
	Timeouts    Timeouts
	RowLimit    int
}

func (c Config) Target() string {
	if c.File != "" {
		return c.File
	}
	host := c.Host
	if host == "" {
		host = "localhost"
	}
	if c.Port > 0 {
		host = fmt.Sprintf("%s:%d", host, c.Port)
	}
	if c.Database == "" {
		return host
	}
	return host + "/" + c.Database
}

func (c Config) ReadOnly() bool { return !c.Mode.Writable() }

type ServerInfo struct {
	Driver      string
	Version     string
	Database    string
	User        string
	ReadOnly    bool
	CanWrite    bool
	Superuser   bool
	ConnectedAt time.Time
}

type Database struct {
	Name    string
	Current bool
	Comment string
}

type Schema struct {
	Name   string
	Tables int
	System bool
}

type Table struct {
	Schema  string
	Name    string
	Kind    string
	Rows    int64
	Size    int64
	Comment string
}

func (t Table) Qualified() string {
	if t.Schema == "" {
		return t.Name
	}
	return t.Schema + "." + t.Name
}

type Column struct {
	Name       string
	Type       string
	Nullable   bool
	Default    string
	PrimaryKey bool
	ForeignKey string
	Position   int
	Comment    string
}

type Relation struct {
	Name          string
	FromSchema    string
	FromTable     string
	FromColumns   []string
	ToSchema      string
	ToTable       string
	ToColumns     []string
	Inbound       bool
	OnDelete      string
	Deferrable    bool
	ConstraintDef string
}

type Index struct {
	Schema     string
	Table      string
	Name       string
	Definition string
	Size       int64
	Scans      int64
	Unique     bool
	Primary    bool
}

type ResultSet interface {
	Columns() []string
	Next() bool
	Values() []any
	Err() error
	Truncated() bool
	Duration() time.Duration
	Close() error
}

type PlanNode struct {
	Name     string
	Detail   string
	Cost     float64
	Rows     int64
	Duration time.Duration
	Depth    int
	Children []PlanNode
}

type Plan struct {
	Root  PlanNode
	Text  string
	Total float64
}

type Severity string

const (
	SeverityOK       Severity = "ok"
	SeverityWarn     Severity = "warn"
	SeverityCritical Severity = "fail"
	SeverityInfo     Severity = "info"
	SeverityUnknown  Severity = "n/a"
)

type Finding struct {
	Subsystem string
	Code      string
	Severity  Severity
	Value     string
	Note      string
}

type Conn interface {
	Info(ctx context.Context) (ServerInfo, error)
	Databases(ctx context.Context) ([]Database, error)
	Schemas(ctx context.Context) ([]Schema, error)
	Tables(ctx context.Context, schema string) ([]Table, error)
	Columns(ctx context.Context, schema, table string) ([]Column, error)
	Relations(ctx context.Context, schema, table string) ([]Relation, error)
	Indexes(ctx context.Context, schema string) ([]Index, error)
	Query(ctx context.Context, sql string) (ResultSet, error)
	Explain(ctx context.Context, sql string, analyze bool) (Plan, error)
	Health(ctx context.Context) ([]Finding, error)
	Close() error
}

type Driver interface {
	Name() string
	Title() string
	Capabilities() Capabilities
	Connect(ctx context.Context, config Config) (Conn, error)
}

type Registration struct {
	Driver Driver
	Name   string
	Title  string
}

type Registry struct {
	entries map[string]Registration
	order   []string
}

func NewRegistry() *Registry { return &Registry{entries: map[string]Registration{}} }

func (r *Registry) Register(driver Driver) {
	entry := Registration{Driver: driver, Name: driver.Name(), Title: driver.Title()}
	if _, seen := r.entries[entry.Name]; !seen {
		r.order = append(r.order, entry.Name)
	}
	r.entries[entry.Name] = entry
}

func (r *Registry) Get(name string) (Driver, error) {
	entry, ok := r.entries[strings.ToLower(name)]
	if !ok {
		return nil, fmt.Errorf("no driver named %q, have %s", name, strings.Join(r.Names(), ", "))
	}
	return entry.Driver, nil
}

func (r *Registry) Entries() []Registration {
	entries := make([]Registration, 0, len(r.order))
	for _, name := range r.order {
		entries = append(entries, r.entries[name])
	}
	return entries
}

func (r *Registry) Names() []string {
	names := append([]string(nil), r.order...)
	sort.Strings(names)
	return names
}
