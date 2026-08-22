package report

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sonquer/tui4db/src/cli/internal/driver"
)

func info() driver.ServerInfo {
	return driver.ServerInfo{
		Driver:   "postgres",
		Version:  "16.3",
		Database: "app",
		User:     "readonly",
		ReadOnly: true,
	}
}

func findings() []driver.Finding {
	return []driver.Finding{
		{Subsystem: "cache", Code: "cache_hit_ratio", Severity: driver.SeverityOK, Value: "99.2%"},
		{Subsystem: "indexes", Code: "unused_indexes", Severity: driver.SeverityWarn, Value: "43.0 GiB", Note: "27 with zero scans"},
		{Subsystem: "locks", Code: "waiting_locks", Severity: driver.SeverityCritical, Value: "3 waiting", Note: "blocked"},
	}
}

func TestNewCarriesTheConnection(t *testing.T) {
	report := New(info(), "production-eu")
	if report.SchemaVersion != SchemaVersion {
		t.Errorf("schema version = %q", report.SchemaVersion)
	}
	if report.GeneratedAt.IsZero() || report.GeneratedAt.Location().String() != "UTC" {
		t.Errorf("generated at = %v", report.GeneratedAt)
	}
	if report.Connection.Name != "production-eu" || report.Connection.Mode != "readonly" {
		t.Fatalf("connection = %+v", report.Connection)
	}
	writable := info()
	writable.ReadOnly = false
	if got := New(writable, "local").Connection.Mode; got != "readwrite" {
		t.Errorf("mode = %q", got)
	}
}

func TestWithFindingsCounts(t *testing.T) {
	report := New(info(), "production-eu").WithFindings(findings())
	if len(report.Findings) != 3 {
		t.Fatalf("findings = %+v", report.Findings)
	}
	if report.Counts.Healthy != 1 || report.Counts.Warnings != 1 || report.Counts.Failing != 1 {
		t.Fatalf("counts = %+v", report.Counts)
	}
	if report.Healthy() {
		t.Error("a report with a failing subsystem is not healthy")
	}
	if !New(info(), "x").WithFindings(findings()[:1]).Healthy() {
		t.Error("a report without failures is healthy")
	}
}

func TestWithTablesAndIndexes(t *testing.T) {
	report := New(info(), "x").
		WithTables([]driver.Table{{Schema: "public", Name: "users", Kind: "table", Rows: 1200, Size: 8192}}).
		WithIndexes([]driver.Index{{Schema: "public", Table: "users", Name: "users_pkey", Size: 4096, Scans: 12, Unique: true}})
	if len(report.Tables) != 1 || report.Tables[0].Name != "users" {
		t.Fatalf("tables = %+v", report.Tables)
	}
	if len(report.Indexes) != 1 || !report.Indexes[0].Unique {
		t.Fatalf("indexes = %+v", report.Indexes)
	}
}

func TestWithStatementNormalises(t *testing.T) {
	report := New(info(), "x").WithStatement("SELECT * FROM users WHERE email = 'a@b.c'")
	if strings.Contains(report.Statement, "a@b.c") {
		t.Fatalf("the statement must be free of data: %q", report.Statement)
	}
}

func TestWriteJSON(t *testing.T) {
	var buffer bytes.Buffer
	report := New(info(), "production-eu").WithFindings(findings())
	if err := WriteJSON(&buffer, report); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(buffer.Bytes(), &decoded); err != nil {
		t.Fatalf("the output must be valid json: %v", err)
	}
	if decoded["schema_version"] != SchemaVersion {
		t.Errorf("schema version = %v", decoded["schema_version"])
	}
	if _, ok := decoded["counts"]; !ok {
		t.Error("the counts must always be present")
	}
	if _, ok := decoded["tables"]; ok {
		t.Error("empty sections must be left out")
	}
}

func TestWriteJSONReportsWriteErrors(t *testing.T) {
	if err := WriteJSON(failingWriter{}, New(info(), "x")); err == nil {
		t.Fatal("want a write error")
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errFailed }

var errFailed = errors.New("no space")

func TestNormalize(t *testing.T) {
	cases := map[string]string{
		"SELECT * FROM users WHERE email = 'a@b.c' AND id = 42": "SELECT * FROM users WHERE email = $1 AND id = $2",
		"SELECT  *\n\tFROM orders":                              "SELECT * FROM orders",
		"SELECT 'it''s here'":                                   "SELECT $1",
		"SELECT total FROM orders WHERE total > 10.5":           "SELECT total FROM orders WHERE total > $1",
		"": "",
	}
	for statement, want := range cases {
		if got := Normalize(statement); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", statement, got, want)
		}
	}
}

func TestEveryFieldIsDescribedByTheSchema(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "schema", "tui4db.report.v1.json"))
	if err != nil {
		t.Fatalf("read the schema: %v", err)
	}
	var schema struct {
		Properties map[string]struct {
			Properties map[string]json.RawMessage `json:"properties"`
			Items      struct {
				Properties map[string]json.RawMessage `json:"properties"`
			} `json:"items"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("the schema must be valid json: %v", err)
	}

	document := New(info(), "production-eu").
		WithFindings(findings()).
		WithTables([]driver.Table{{Schema: "public", Name: "users"}}).
		WithIndexes([]driver.Index{{Schema: "public", Table: "users", Name: "users_pkey"}}).
		WithStatement("SELECT 1")

	var buffer bytes.Buffer
	if err := WriteJSON(&buffer, document); err != nil {
		t.Fatal(err)
	}
	var produced map[string]json.RawMessage
	if err := json.Unmarshal(buffer.Bytes(), &produced); err != nil {
		t.Fatal(err)
	}
	for field := range produced {
		if _, ok := schema.Properties[field]; !ok {
			t.Errorf("the report carries %q, which the schema does not describe", field)
		}
	}
	for _, section := range []string{"connection", "counts"} {
		var nested map[string]json.RawMessage
		if err := json.Unmarshal(produced[section], &nested); err != nil {
			t.Fatal(err)
		}
		for field := range nested {
			if _, ok := schema.Properties[section].Properties[field]; !ok {
				t.Errorf("%s carries %q, which the schema does not describe", section, field)
			}
		}
	}
	for _, section := range []string{"findings", "tables", "indexes"} {
		var items []map[string]json.RawMessage
		if err := json.Unmarshal(produced[section], &items); err != nil {
			t.Fatal(err)
		}
		for field := range items[0] {
			if _, ok := schema.Properties[section].Items.Properties[field]; !ok {
				t.Errorf("%s carries %q, which the schema does not describe", section, field)
			}
		}
	}
}

func TestSchemaVersionMatchesTheContract(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "schema", "tui4db.report.v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"pattern": "^\\d+\\.\\d+\\.\\d+$"`) {
		t.Error("the schema must pin the shape of the version")
	}
	if !strings.HasPrefix(SchemaVersion, "1.") {
		t.Errorf("this file describes version 1, the code says %q", SchemaVersion)
	}
}
