package cover

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func markdownRows() []TableRow {
	return []TableRow{
		{Label: "All files", Stats: Stats{Statements: 8, CoveredStatements: 3, Lines: 8, CoveredLines: 3, Funcs: 4, CoveredFuncs: 2}},
		{Label: "cli/pkg/sqlguard", Depth: 1, Stats: Stats{Statements: 4, CoveredStatements: 4, Lines: 4, CoveredLines: 4, Funcs: 2, CoveredFuncs: 2}},
		{Label: "guard.go", Depth: 2, Stats: Stats{Statements: 4, CoveredStatements: 1, Uncovered: []LineRange{{From: 3, To: 9}}}},
	}
}

func TestWriteMarkdown(t *testing.T) {
	var buffer bytes.Buffer
	if err := WriteMarkdown(&buffer, markdownRows(), 95, "tui4db"); err != nil {
		t.Fatalf("WriteMarkdown: %v", err)
	}
	out := buffer.String()
	for _, want := range []string{
		"## tui4db coverage",
		"**37.50%** of 8 statements · gate 95% failed",
		"| File | % Stmts | % Funcs | % Lines | Uncovered Lines |",
		"**All files**",
		"&nbsp;&nbsp;cli/pkg/sqlguard",
		"`3-9`",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("summary missing %q:\n%s", want, out)
		}
	}
}

func TestWriteMarkdownReportsAPassingGate(t *testing.T) {
	var buffer bytes.Buffer
	if err := WriteMarkdown(&buffer, markdownRows(), 10, "tui4db"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buffer.String(), "gate 10% passed") {
		t.Errorf("summary = %s", buffer.String())
	}
}

func TestWriteMarkdownWithoutRows(t *testing.T) {
	var buffer bytes.Buffer
	if err := WriteMarkdown(&buffer, nil, 95, "tui4db"); err != nil {
		t.Fatal(err)
	}
	if buffer.Len() != 0 {
		t.Errorf("empty input must produce no output, got %q", buffer.String())
	}
}

func TestWriteMarkdownReportsWriteErrors(t *testing.T) {
	if err := WriteMarkdown(failingWriter{}, markdownRows(), 95, "tui4db"); err == nil {
		t.Fatal("want write error")
	}
}

func TestMarkdownCellTruncatesLongLists(t *testing.T) {
	long := strings.Repeat("12,", 40)
	cell := markdownCell(long)
	if len([]rune(cell)) > 64 {
		t.Errorf("cell = %q", cell)
	}
	if markdownCell("") != "" {
		t.Error("an empty list must render as an empty cell")
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("no space") }
