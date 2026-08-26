// Package cover reads Go coverage profiles and reports them the way Istanbul
// does: a table of packages and files with statement, function and line
// percentages plus the uncovered line ranges, a single self-contained HTML
// document, and a markdown summary for continuous integration.
package cover

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"golang.org/x/tools/cover"
)

// Block is one counted block from a coverage profile.
type Block struct {
	StartLine int
	StartCol  int
	EndLine   int
	EndCol    int
	Count     int
	NumStmt   int
}

// File is the coverage of a single source file.
type File struct {
	Path       string
	Statements int
	Covered    int
	Blocks     []Block
}

// Percent is the share of statements that were covered.
func (f File) Percent() float64 { return percent(f.Covered, f.Statements) }

// Package returns the import path of the package the file belongs to.
func (f File) Package() string { return dir(f.Path) }

// Package is the coverage of every file in one package.
type Package struct {
	ImportPath string
	Statements int
	Covered    int
	Files      []File
}

func (p Package) Percent() float64 { return percent(p.Covered, p.Statements) }

// Summary is the coverage of everything in a profile.
type Summary struct {
	Packages   []Package
	Files      []File
	Statements int
	Covered    int
}

func (s Summary) Percent() float64 { return percent(s.Covered, s.Statements) }

func (s Summary) Meets(min float64) bool { return s.Percent() >= min }

func (s Summary) Below(min float64) []Package {
	var below []Package
	for _, p := range s.Packages {
		if p.Statements > 0 && p.Percent() < min {
			below = append(below, p)
		}
	}
	return below
}

func (s Summary) Without(excluded func(importPath string) bool) Summary {
	if excluded == nil {
		return s
	}
	filtered := Summary{}
	for _, pkg := range s.Packages {
		if excluded(pkg.ImportPath) {
			continue
		}
		filtered.Packages = append(filtered.Packages, pkg)
		filtered.Statements += pkg.Statements
		filtered.Covered += pkg.Covered
	}
	for _, file := range s.Files {
		if excluded(file.Package()) {
			continue
		}
		filtered.Files = append(filtered.Files, file)
	}
	return filtered
}

func percent(covered, statements int) float64 {
	if statements == 0 {
		return 100
	}
	return float64(covered) / float64(statements) * 100
}

// Merge combines summaries from several modules into one.
func Merge(summaries ...Summary) Summary {
	merged := Summary{}
	for _, summary := range summaries {
		merged.Packages = append(merged.Packages, summary.Packages...)
		merged.Files = append(merged.Files, summary.Files...)
		merged.Statements += summary.Statements
		merged.Covered += summary.Covered
	}
	sort.Slice(merged.Packages, func(i, j int) bool {
		return merged.Packages[i].ImportPath < merged.Packages[j].ImportPath
	})
	sort.Slice(merged.Files, func(i, j int) bool { return merged.Files[i].Path < merged.Files[j].Path })
	return merged
}

// Parse reads a Go coverage profile.
func Parse(r io.Reader) (Summary, error) {
	profiles, err := cover.ParseProfilesFromReader(r)
	if err != nil {
		return Summary{}, fmt.Errorf("parse coverage profile: %w", err)
	}
	return summarize(profiles), nil
}

// ParseFile reads a Go coverage profile from disk.
func ParseFile(path string) (Summary, error) {
	f, err := os.Open(path)
	if err != nil {
		return Summary{}, fmt.Errorf("open coverage profile: %w", err)
	}
	defer f.Close()
	return Parse(f)
}

func summarize(profiles []*cover.Profile) Summary {
	summary := Summary{}
	byPackage := map[string]*Package{}
	for _, profile := range profiles {
		file := File{Path: profile.FileName}
		for _, b := range profile.Blocks {
			file.Blocks = append(file.Blocks, Block{
				StartLine: b.StartLine,
				StartCol:  b.StartCol,
				EndLine:   b.EndLine,
				EndCol:    b.EndCol,
				Count:     b.Count,
				NumStmt:   b.NumStmt,
			})
			file.Statements += b.NumStmt
			if b.Count > 0 {
				file.Covered += b.NumStmt
			}
		}
		summary.Files = append(summary.Files, file)
		summary.Statements += file.Statements
		summary.Covered += file.Covered

		path := file.Package()
		pkg, ok := byPackage[path]
		if !ok {
			pkg = &Package{ImportPath: path}
			byPackage[path] = pkg
		}
		pkg.Files = append(pkg.Files, file)
		pkg.Statements += file.Statements
		pkg.Covered += file.Covered
	}
	summary.Packages = make([]Package, 0, len(byPackage))
	for _, pkg := range byPackage {
		summary.Packages = append(summary.Packages, *pkg)
	}
	sort.Slice(summary.Packages, func(i, j int) bool {
		return summary.Packages[i].ImportPath < summary.Packages[j].ImportPath
	})
	sort.Slice(summary.Files, func(i, j int) bool { return summary.Files[i].Path < summary.Files[j].Path })
	return summary
}

func dir(path string) string {
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		return path[:i]
	}
	return path
}

// ShortPath trims a module path off an import path.
func ShortPath(importPath, modulePath string) string {
	if importPath == modulePath {
		return "."
	}
	if trimmed := strings.TrimPrefix(importPath, modulePath+"/"); trimmed != importPath {
		return trimmed
	}
	return importPath
}

// LineHits maps each line of a file to the highest execution count of the blocks
// covering it.
func LineHits(f File) map[int]int {
	hits := map[int]int{}
	for _, b := range f.Blocks {
		for line := b.StartLine; line <= b.EndLine; line++ {
			if current, ok := hits[line]; !ok || b.Count > current {
				hits[line] = b.Count
			}
		}
	}
	return hits
}
