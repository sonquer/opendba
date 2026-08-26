package cover

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const analyzedSource = `package a

func Covered() int {
	return 1
}

func Uncovered() int {
	return 2
}
`

func analyzedFile() File {
	return File{
		Path: "example.com/m/a/one.go",
		Blocks: []Block{
			{StartLine: 3, EndLine: 5, Count: 4, NumStmt: 1},
			{StartLine: 7, EndLine: 9, Count: 0, NumStmt: 1},
		},
		Statements: 2,
		Covered:    1,
	}
}

func TestAnalyzeCountsStatementsLinesAndFunctions(t *testing.T) {
	stats := Analyze(analyzedFile(), analyzedSource)
	if stats.Statements != 2 || stats.CoveredStatements != 1 {
		t.Errorf("statements = %d/%d", stats.CoveredStatements, stats.Statements)
	}
	if stats.Lines != 6 || stats.CoveredLines != 3 {
		t.Errorf("lines = %d/%d", stats.CoveredLines, stats.Lines)
	}
	if stats.Funcs != 2 || stats.CoveredFuncs != 1 {
		t.Errorf("funcs = %d/%d", stats.CoveredFuncs, stats.Funcs)
	}
	if got := stats.UncoveredList(); got != "7-9" {
		t.Errorf("UncoveredList() = %q", got)
	}
	if got := stats.StatementPercent(); got != 50 {
		t.Errorf("StatementPercent() = %v", got)
	}
	if got := stats.LinePercent(); got != 50 {
		t.Errorf("LinePercent() = %v", got)
	}
	if got := stats.FuncPercent(); got != 50 {
		t.Errorf("FuncPercent() = %v", got)
	}
}

func TestAnalyzeCountsFunctionLiterals(t *testing.T) {
	source := "package a\n\nvar f = func() int {\n\treturn 1\n}\n"
	stats := Analyze(File{Blocks: []Block{{StartLine: 3, EndLine: 5, Count: 1, NumStmt: 1}}}, source)
	if stats.Funcs != 1 || stats.CoveredFuncs != 1 {
		t.Errorf("funcs = %d/%d", stats.CoveredFuncs, stats.Funcs)
	}
}

func TestAnalyzeWithoutSource(t *testing.T) {
	stats := Analyze(analyzedFile(), "")
	if stats.Funcs != 0 || stats.CoveredFuncs != 0 {
		t.Errorf("funcs = %d/%d", stats.CoveredFuncs, stats.Funcs)
	}
	if stats.Statements != 2 {
		t.Errorf("statements = %d", stats.Statements)
	}
}

func TestAnalyzeIgnoresUnparsableSource(t *testing.T) {
	if stats := Analyze(analyzedFile(), "package ???"); stats.Funcs != 0 {
		t.Errorf("funcs = %d", stats.Funcs)
	}
}

func TestAnalyzeSkipsFunctionsWithoutABody(t *testing.T) {
	stats := Analyze(File{}, "package a\n\nfunc External()\n")
	if stats.Funcs != 0 {
		t.Errorf("funcs = %d", stats.Funcs)
	}
}

func TestLineRangeFormatting(t *testing.T) {
	if got := (LineRange{From: 4, To: 4}).String(); got != "4" {
		t.Errorf("String() = %q", got)
	}
	if got := (LineRange{From: 4, To: 9}).String(); got != "4-9" {
		t.Errorf("String() = %q", got)
	}
}

func TestMergeRangesJoinsNeighbours(t *testing.T) {
	ranges := mergeRanges([]int{1, 2, 3, 7, 9, 10})
	if len(ranges) != 3 {
		t.Fatalf("ranges = %v", ranges)
	}
	if ranges[0].String() != "1-3" || ranges[1].String() != "7" || ranges[2].String() != "9-10" {
		t.Errorf("ranges = %v", ranges)
	}
	if mergeRanges(nil) != nil {
		t.Error("no uncovered lines must produce no ranges")
	}
}

func TestStatsAdd(t *testing.T) {
	first := Stats{Statements: 2, CoveredStatements: 1, Lines: 4, CoveredLines: 2, Funcs: 2, CoveredFuncs: 1}
	sum := first.Add(first)
	if sum.Statements != 4 || sum.CoveredStatements != 2 || sum.Lines != 8 || sum.CoveredLines != 4 || sum.Funcs != 4 || sum.CoveredFuncs != 2 {
		t.Fatalf("sum = %+v", sum)
	}
}

func TestEmptyStatsReportHundredPercent(t *testing.T) {
	var stats Stats
	if stats.StatementPercent() != 100 || stats.LinePercent() != 100 || stats.FuncPercent() != 100 {
		t.Error("an empty file must not be reported as uncovered")
	}
	if stats.UncoveredList() != "" {
		t.Error("an empty file has no uncovered lines")
	}
}

func TestRowsBuildATreeOfPackagesAndFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "one.go"), []byte(analyzedSource), 0o600); err != nil {
		t.Fatal(err)
	}
	resolve := func(importPath string) (string, bool) {
		if strings.HasSuffix(importPath, "one.go") {
			return filepath.Join(dir, "one.go"), true
		}
		return "", false
	}
	rows := Rows(parseSample(t), resolve, func(path string) string { return strings.TrimPrefix(path, "example.com/m/") })

	if rows[0].Label != "All files" || rows[0].Depth != 0 {
		t.Fatalf("first row = %+v", rows[0])
	}
	if rows[0].Stats.Statements != 8 || rows[0].Stats.CoveredStatements != 3 {
		t.Errorf("total = %+v", rows[0].Stats)
	}
	if rows[1].Label != "a" || rows[1].Depth != 1 {
		t.Errorf("package row = %+v", rows[1])
	}
	if rows[2].Label != "one.go" || rows[2].Depth != 2 {
		t.Errorf("file row = %+v", rows[2])
	}
	if rows[2].Stats.Funcs == 0 {
		t.Error("resolved sources must contribute function counts")
	}
}

func TestRowsWithoutAShortener(t *testing.T) {
	rows := Rows(parseSample(t), nil, nil)
	if rows[1].Label != "example.com/m/a" {
		t.Errorf("package label = %q", rows[1].Label)
	}
}

func TestBaseFallsBackToTheWholePath(t *testing.T) {
	if got := base("main.go"); got != "main.go" {
		t.Errorf("base() = %q", got)
	}
}
