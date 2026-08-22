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
	Sessions        bool
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

// Groups a finding can belong to. They are what the interface lays out in
// columns, and what a refused permission degrades one of.
const (
	GroupMemory  = "memory"
	GroupLoad    = "load"
	GroupScans   = "scans"
	GroupStorage = "storage"
)

// ByteSize writes a size the way a person reads one. A negative size is a size
// the server did not report.
func ByteSize(bytes int64) string {
	const unit = 1024
	if bytes < 0 {
		return "n/a"
	}
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	value, exponent := float64(bytes), 0
	for value >= unit && exponent < 4 {
		value /= unit
		exponent++
	}
	return fmt.Sprintf("%.1f %ciB", value, "KMGT"[exponent-1])
}

// Duration writes a length of time at the precision that matters at that
// length.
func Duration(d time.Duration) string {
	switch {
	case d >= time.Minute:
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	case d >= time.Second:
		return fmt.Sprintf("%.2fs", d.Seconds())
	default:
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
}

// Session is one connection the server is holding, ours included.
type Session struct {
	ID          string
	User        string
	Application string
	Client      string
	State       string
	Wait        string
	Duration    time.Duration
	Statement   string
	Mine        bool
}

// Running reports whether this session is doing work rather than waiting for
// its client to say something.
func (s Session) Running() bool { return s.State == "active" }

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
	Group     string
	Subsystem string
	Code      string
	Severity  Severity
	Value     string
	Note      string

	// Ratio is where this measurement sits between nothing and everything, for
	// the checks that are a proportion of something. A check that is a count, a
	// size or a word leaves it at zero and says so with Measured.
	Ratio    float64
	Measured bool
}

// Measure records a proportion, clamped to what a proportion can be.
func (f Finding) Measure(part, whole float64) Finding {
	if whole <= 0 {
		return f
	}
	ratio := part / whole
	switch {
	case ratio < 0:
		ratio = 0
	case ratio > 1:
		ratio = 1
	}
	f.Ratio, f.Measured = ratio, true
	return f
}

type Conn interface {
	Info(ctx context.Context) (ServerInfo, error)
	Databases(ctx context.Context) ([]Database, error)
	Schemas(ctx context.Context) ([]Schema, error)
	Tables(ctx context.Context, schema string) ([]Table, error)
	Columns(ctx context.Context, schema, table string) ([]Column, error)
	Relations(ctx context.Context, schema, table string) ([]Relation, error)
	Indexes(ctx context.Context, schema string) ([]Index, error)
	Sessions(ctx context.Context) ([]Session, error)
	Stop(ctx context.Context, id string, terminate bool) error
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
