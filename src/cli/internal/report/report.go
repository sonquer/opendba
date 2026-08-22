package report

import (
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/sonquer/tui4db/src/cli/internal/driver"
)

const SchemaVersion = "1.0.0"

type Exactness string

const (
	ExactnessSampled     Exactness = "sampled"
	ExactnessCumulative  Exactness = "cumulative"
	ExactnessUnavailable Exactness = "unavailable"
)

type Connection struct {
	Name     string `json:"name"`
	Driver   string `json:"driver"`
	Version  string `json:"version"`
	Database string `json:"database"`
	User     string `json:"user"`
	Mode     string `json:"mode"`
	ReadOnly bool   `json:"read_only"`
}

type Finding struct {
	Subsystem string  `json:"subsystem"`
	Code      string  `json:"code"`
	Severity  string  `json:"severity"`
	Value     string  `json:"value"`
	Ratio     float64 `json:"ratio,omitempty"`
	Note      string  `json:"note,omitempty"`
}

type Counts struct {
	Healthy  int `json:"healthy"`
	Warnings int `json:"warnings"`
	Failing  int `json:"failing"`
}

type Table struct {
	Schema  string `json:"schema"`
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Rows    int64  `json:"rows"`
	Size    int64  `json:"size_bytes"`
	Columns int    `json:"columns,omitempty"`
}

type Index struct {
	Schema string `json:"schema"`
	Table  string `json:"table"`
	Name   string `json:"name"`
	Size   int64  `json:"size_bytes"`
	Scans  int64  `json:"scans"`
	Unique bool   `json:"unique"`
}

type Report struct {
	SchemaVersion string     `json:"schema_version"`
	GeneratedAt   time.Time  `json:"generated_at"`
	Connection    Connection `json:"connection"`
	Exactness     Exactness  `json:"exactness"`
	Findings      []Finding  `json:"findings,omitempty"`
	Tables        []Table    `json:"tables,omitempty"`
	Indexes       []Index    `json:"indexes,omitempty"`
	Counts        Counts     `json:"counts"`
	Statement     string     `json:"statement,omitempty"`
}

func New(info driver.ServerInfo, name string) Report {
	return Report{
		SchemaVersion: SchemaVersion,
		GeneratedAt:   time.Now().UTC(),
		Exactness:     ExactnessSampled,
		Connection: Connection{
			Name:     name,
			Driver:   info.Driver,
			Version:  info.Version,
			Database: info.Database,
			User:     info.User,
			Mode:     mode(info.ReadOnly),
			ReadOnly: info.ReadOnly,
		},
	}
}

func mode(readOnly bool) string {
	if readOnly {
		return "readonly"
	}
	return "readwrite"
}

func (r Report) WithFindings(findings []driver.Finding) Report {
	r.Findings = make([]Finding, 0, len(findings))
	for _, finding := range findings {
		r.Findings = append(r.Findings, Finding{
			Subsystem: finding.Subsystem,
			Code:      finding.Code,
			Severity:  string(finding.Severity),
			Value:     finding.Value,
			Ratio:     finding.Ratio,
			Note:      finding.Note,
		})
		switch finding.Severity {
		case driver.SeverityWarn:
			r.Counts.Warnings++
		case driver.SeverityCritical:
			r.Counts.Failing++
		default:
			r.Counts.Healthy++
		}
	}
	return r
}

func (r Report) WithTables(tables []driver.Table) Report {
	r.Tables = make([]Table, 0, len(tables))
	for _, table := range tables {
		r.Tables = append(r.Tables, Table{
			Schema: table.Schema,
			Name:   table.Name,
			Kind:   table.Kind,
			Rows:   table.Rows,
			Size:   table.Size,
		})
	}
	return r
}

func (r Report) WithIndexes(indexes []driver.Index) Report {
	r.Indexes = make([]Index, 0, len(indexes))
	for _, index := range indexes {
		r.Indexes = append(r.Indexes, Index{
			Schema: index.Schema,
			Table:  index.Table,
			Name:   index.Name,
			Size:   index.Size,
			Scans:  index.Scans,
			Unique: index.Unique,
		})
	}
	return r
}

func (r Report) WithStatement(statement string) Report {
	r.Statement = Normalize(statement)
	return r
}

func (r Report) Healthy() bool { return r.Counts.Failing == 0 }

func WriteJSON(w io.Writer, report Report) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		return fmt.Errorf("write the report: %w", err)
	}
	return nil
}

var (
	literal    = regexp.MustCompile(`'(?:[^']|'')*'|\b\d+(?:\.\d+)?\b`)
	whitespace = regexp.MustCompile(`\s+`)
)

func Normalize(statement string) string {
	placeholder := 0
	next := func(string) string {
		placeholder++
		return "$" + strconv.Itoa(placeholder)
	}
	normalized := literal.ReplaceAllStringFunc(statement, next)
	normalized = whitespace.ReplaceAllString(normalized, " ")
	return strings.TrimSpace(normalized)
}
