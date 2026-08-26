package cover

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sampleSource(t *testing.T) Resolver {
	t.Helper()
	dir := t.TempDir()
	source := "package a\n\nfunc One() int {\n\treturn 1\n}\n\nfunc Two() int {\n\treturn 2\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "one.go"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	return func(importPath string) (string, bool) {
		if strings.HasSuffix(importPath, "one.go") {
			return filepath.Join(dir, "one.go"), true
		}
		return "", false
	}
}

func render(t *testing.T, opts ReportOptions) string {
	t.Helper()
	var buffer bytes.Buffer
	if err := WriteReport(&buffer, parseSample(t), opts); err != nil {
		t.Fatalf("WriteReport: %v", err)
	}
	return buffer.String()
}

func TestReportIsASelfContainedDocument(t *testing.T) {
	html := render(t, ReportOptions{
		Title:     "opendba",
		Min:       95,
		Resolve:   sampleSource(t),
		Generated: "2026-08-21",
		Sections:  []string{"cli", "tools"},
	})
	for _, want := range []string{
		"<!doctype html>", "Code coverage report for", "opendba", "<style>", "37.50%", "3/8",
		"cli, tools", "2026-08-21", "example.com/m/a", "one.go",
		"coverage-summary", "status-line", "cover-fill", "Statements", "Functions", "Lines",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("report missing %q", want)
		}
	}
	if strings.Contains(html, "<script") {
		t.Error("the report must not need javascript")
	}
	if strings.Contains(html, "prefers-color-scheme") {
		t.Error("the report is light only")
	}
	if !strings.Contains(html, "Helvetica Neue") || !strings.Contains(html, "Menlo") {
		t.Error("headings are sans serif and source is monospace")
	}
	if strings.Contains(html, "<details") {
		t.Error("sections are linked, not collapsed")
	}
	if strings.Count(html, "<html") != 1 {
		t.Error("the report must be a single document")
	}
}

func TestReportMarksCoveredAndUncoveredLines(t *testing.T) {
	html := render(t, ReportOptions{Resolve: sampleSource(t)})
	if !strings.Contains(html, `cline-yes`) || !strings.Contains(html, `cline-no`) {
		t.Error("source lines must be marked as hit or missed")
	}
	if !strings.Contains(html, `cline-neutral`) {
		t.Error("lines without statements must be neutral")
	}
	if !strings.Contains(html, `line-count`) || !strings.Contains(html, `line-coverage`) {
		t.Error("the gutter must carry line numbers and hit counts")
	}
	if !strings.Contains(html, "1x") {
		t.Error("covered lines must show how often they ran")
	}
}

func TestReportDegradesWithoutSource(t *testing.T) {
	html := render(t, ReportOptions{})
	if !strings.Contains(html, "source not available") {
		t.Error("unresolvable files must say so")
	}
	if !strings.Contains(html, "<title>Code coverage report for coverage</title>") {
		t.Error("a default title must be used")
	}
}

func TestReportMarksTheLevelAgainstTheGate(t *testing.T) {
	if !strings.Contains(render(t, ReportOptions{Min: 95}), `status-line low`) {
		t.Error("a run below the gate must be marked low")
	}
	if !strings.Contains(render(t, ReportOptions{Min: 10}), `status-line high`) {
		t.Error("a run above the gate must be marked high")
	}
	if !strings.Contains(render(t, ReportOptions{Min: 60}), `status-line medium`) {
		t.Error("a run halfway to the gate must be marked medium")
	}
}

func TestWriteReportFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "coverage.html")
	if err := WriteReportFile(path, parseSample(t), ReportOptions{Title: "opendba"}); err != nil {
		t.Fatalf("WriteReportFile: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	if !strings.HasPrefix(string(data), "<!doctype html>") {
		t.Errorf("report starts with %q", string(data[:20]))
	}
}

func TestWriteReportFileRejectsBadPath(t *testing.T) {
	if err := WriteReportFile(filepath.Join(t.TempDir(), "missing", "coverage.html"), Summary{}, ReportOptions{}); err == nil {
		t.Fatal("want create error")
	}
}

func TestAnnotateHandlesLineEndings(t *testing.T) {
	lines := annotate("a\r\nb\r\n", map[int]int{1: 0})
	if len(lines) != 2 {
		t.Fatalf("lines = %d", len(lines))
	}
	if lines[0].State != "no" || lines[1].State != "neutral" {
		t.Fatalf("states = %q %q", lines[0].State, lines[1].State)
	}
	if lines[0].Text != "a" {
		t.Errorf("carriage return not stripped: %q", lines[0].Text)
	}
}

func TestReadSourceFailures(t *testing.T) {
	if _, ok := readSource("x", nil); ok {
		t.Error("a nil resolver must fail")
	}
	resolve := func(string) (string, bool) { return filepath.Join(t.TempDir(), "missing.go"), true }
	if _, ok := readSource("x", resolve); ok {
		t.Error("a missing file must fail")
	}
	if _, ok := readSource("x", func(string) (string, bool) { return "", false }); ok {
		t.Error("an unresolvable path must fail")
	}
}

func TestFormatPercent(t *testing.T) {
	if got := format(37.5); got != "37.50%" {
		t.Errorf("format() = %q", got)
	}
}

func TestLevelWithoutAGate(t *testing.T) {
	if level(95, 0) != "high" || level(60, 0) != "medium" || level(10, 0) != "low" {
		t.Error("without a gate the levels fall back to the usual thresholds")
	}
}

func TestAnchorIsSafeForAFragment(t *testing.T) {
	if got := anchor("example.com/m/a/one.go"); got != "example-com-m-a-one-go" {
		t.Errorf("anchor() = %q", got)
	}
}
