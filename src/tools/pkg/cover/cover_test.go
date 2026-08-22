package cover

import (
	"os"
	"strings"
	"testing"
)

const sampleProfile = `mode: set
example.com/m/a/one.go:3.10,5.2 2 1
example.com/m/a/one.go:7.10,9.2 2 0
example.com/m/a/two.go:3.10,4.2 1 1
example.com/m/b/three.go:3.10,6.2 3 0
`

func parseSample(t *testing.T) Summary {
	t.Helper()
	s, err := Parse(strings.NewReader(sampleProfile))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return s
}

func TestParseAggregatesStatements(t *testing.T) {
	s := parseSample(t)
	if s.Statements != 8 || s.Covered != 3 {
		t.Fatalf("statements=%d covered=%d, want 8/3", s.Statements, s.Covered)
	}
	if got := s.Percent(); got < 37.4 || got > 37.6 {
		t.Errorf("Percent() = %v, want ~37.5", got)
	}
	if len(s.Packages) != 2 || len(s.Files) != 3 {
		t.Fatalf("packages=%d files=%d", len(s.Packages), len(s.Files))
	}
	if s.Packages[0].ImportPath != "example.com/m/a" {
		t.Errorf("packages not sorted: %v", s.Packages[0].ImportPath)
	}
	if s.Files[0].Path != "example.com/m/a/one.go" {
		t.Errorf("files not sorted: %v", s.Files[0].Path)
	}
}

func TestPackageAndFilePercent(t *testing.T) {
	s := parseSample(t)
	pkgA := s.Packages[0]
	if pkgA.Statements != 5 || pkgA.Covered != 3 {
		t.Fatalf("pkg a = %d/%d", pkgA.Covered, pkgA.Statements)
	}
	if got := pkgA.Percent(); got != 60 {
		t.Errorf("pkg a percent = %v, want 60", got)
	}
	if got := s.Files[0].Percent(); got != 50 {
		t.Errorf("one.go percent = %v, want 50", got)
	}
	if got := s.Files[0].Package(); got != "example.com/m/a" {
		t.Errorf("Package() = %q", got)
	}
}

func TestEmptySummaryIsHundredPercent(t *testing.T) {
	var s Summary
	if got := s.Percent(); got != 100 {
		t.Errorf("empty Percent() = %v, want 100", got)
	}
	if (File{}).Percent() != 100 || (Package{}).Percent() != 100 {
		t.Error("empty file and package must report 100")
	}
}

func TestMeetsAndBelow(t *testing.T) {
	s := parseSample(t)
	if s.Meets(95) {
		t.Error("37.5% must not meet 95")
	}
	if !s.Meets(30) {
		t.Error("37.5% must meet 30")
	}
	below := s.Below(70)
	if len(below) != 2 {
		t.Fatalf("Below(70) = %v", below)
	}
	if len(s.Below(0)) != 0 {
		t.Errorf("Below(0) must be empty, got %v", s.Below(0))
	}
}

func TestParseRejectsGarbage(t *testing.T) {
	if _, err := Parse(strings.NewReader("not a profile\n")); err == nil {
		t.Fatal("want parse error")
	}
}

func TestShortPath(t *testing.T) {
	const mod = "example.com/m"
	cases := map[string]string{
		mod:                       ".",
		mod + "/internal/ui":      "internal/ui",
		"other.com/x/internal/ui": "other.com/x/internal/ui",
	}
	for in, want := range cases {
		if got := ShortPath(in, mod); got != want {
			t.Errorf("ShortPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLineHitsTakesHighestCount(t *testing.T) {
	f := File{Blocks: []Block{
		{StartLine: 1, EndLine: 3, Count: 0, NumStmt: 1},
		{StartLine: 3, EndLine: 4, Count: 7, NumStmt: 1},
	}}
	hits := LineHits(f)
	if hits[1] != 0 || hits[3] != 7 || hits[4] != 7 {
		t.Fatalf("hits = %v", hits)
	}
	if _, ok := hits[5]; ok {
		t.Error("line 5 must not be tracked")
	}
}

func TestDirFallsBackToWholePath(t *testing.T) {
	if got := dir("main.go"); got != "main.go" {
		t.Errorf("dir() = %q", got)
	}
}

func TestParseFile(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/cover.out"
	if err := os.WriteFile(path, []byte(sampleProfile), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if s.Statements != 8 {
		t.Errorf("statements = %d", s.Statements)
	}
	if _, err := ParseFile(dir + "/missing.out"); err == nil {
		t.Fatal("want error for missing profile")
	}
}

func TestWithoutRemovesExcludedPackages(t *testing.T) {
	s := parseSample(t)
	filtered := s.Without(func(importPath string) bool { return strings.HasSuffix(importPath, "/b") })
	if len(filtered.Packages) != 1 || filtered.Packages[0].ImportPath != "example.com/m/a" {
		t.Fatalf("packages = %+v", filtered.Packages)
	}
	if filtered.Statements != 5 || filtered.Covered != 3 {
		t.Fatalf("statements = %d covered = %d", filtered.Statements, filtered.Covered)
	}
	if len(filtered.Files) != 2 {
		t.Fatalf("files = %d", len(filtered.Files))
	}
	if got := s.Without(nil); got.Statements != s.Statements {
		t.Error("a nil predicate must keep everything")
	}
}

func TestMergeCombinesSummaries(t *testing.T) {
	first := parseSample(t)
	second, err := Parse(strings.NewReader("mode: set\nexample.com/other/z.go:1.1,2.2 4 1\n"))
	if err != nil {
		t.Fatal(err)
	}
	merged := Merge(first, second)
	if merged.Statements != 12 || merged.Covered != 7 {
		t.Fatalf("statements = %d covered = %d", merged.Statements, merged.Covered)
	}
	if len(merged.Packages) != 3 || merged.Packages[0].ImportPath != "example.com/m/a" {
		t.Fatalf("packages = %+v", merged.Packages)
	}
	if len(merged.Files) != 4 {
		t.Fatalf("files = %d", len(merged.Files))
	}
	if Merge().Statements != 0 {
		t.Error("merging nothing must produce an empty summary")
	}
}
