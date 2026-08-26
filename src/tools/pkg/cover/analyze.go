package cover

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strconv"
	"strings"
)

// LineRange is a run of consecutive lines.
type LineRange struct {
	From int
	To   int
}

// String renders a single line as a number and a run as from-to.
func (r LineRange) String() string {
	if r.From == r.To {
		return strconv.Itoa(r.From)
	}
	return strconv.Itoa(r.From) + "-" + strconv.Itoa(r.To)
}

// Stats is what one file or one package contributes to a report.
type Stats struct {
	Statements        int
	CoveredStatements int
	Lines             int
	CoveredLines      int
	Funcs             int
	CoveredFuncs      int
	Uncovered         []LineRange
}

func (s Stats) StatementPercent() float64 { return percent(s.CoveredStatements, s.Statements) }

func (s Stats) LinePercent() float64 { return percent(s.CoveredLines, s.Lines) }

func (s Stats) FuncPercent() float64 { return percent(s.CoveredFuncs, s.Funcs) }

func (s Stats) UncoveredList() string {
	parts := make([]string, 0, len(s.Uncovered))
	for _, r := range s.Uncovered {
		parts = append(parts, r.String())
	}
	return strings.Join(parts, ",")
}

func (s Stats) Add(other Stats) Stats {
	s.Statements += other.Statements
	s.CoveredStatements += other.CoveredStatements
	s.Lines += other.Lines
	s.CoveredLines += other.CoveredLines
	s.Funcs += other.Funcs
	s.CoveredFuncs += other.CoveredFuncs
	return s
}

// Analyze combines profile data with the source it came from. Without the source
// the function counts are zero and everything else still holds.
func Analyze(file File, source string) Stats {
	stats := Stats{Statements: file.Statements, CoveredStatements: file.Covered}
	hits := LineHits(file)
	stats.Lines = len(hits)
	uncovered := make([]int, 0, len(hits))
	for line, count := range hits {
		if count > 0 {
			stats.CoveredLines++
			continue
		}
		uncovered = append(uncovered, line)
	}
	sort.Ints(uncovered)
	stats.Uncovered = mergeRanges(uncovered)
	stats.Funcs, stats.CoveredFuncs = countFunctions(source, file.Blocks)
	return stats
}

func mergeRanges(lines []int) []LineRange {
	var ranges []LineRange
	for _, line := range lines {
		if n := len(ranges); n > 0 && ranges[n-1].To == line-1 {
			ranges[n-1].To = line
			continue
		}
		ranges = append(ranges, LineRange{From: line, To: line})
	}
	return ranges
}

func countFunctions(source string, blocks []Block) (int, int) {
	if strings.TrimSpace(source) == "" {
		return 0, 0
	}
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, "", source, 0)
	if err != nil {
		return 0, 0
	}
	total, covered := 0, 0
	ast.Inspect(parsed, func(node ast.Node) bool {
		var body *ast.BlockStmt
		switch declaration := node.(type) {
		case *ast.FuncDecl:
			body = declaration.Body
		case *ast.FuncLit:
			body = declaration.Body
		default:
			return true
		}
		if body == nil {
			return true
		}
		total++
		if functionCovered(fset.Position(body.Lbrace).Line, fset.Position(body.Rbrace).Line, blocks) {
			covered++
		}
		return true
	})
	return total, covered
}

func functionCovered(from, to int, blocks []Block) bool {
	for _, block := range blocks {
		if block.Count > 0 && block.StartLine >= from && block.EndLine <= to {
			return true
		}
	}
	return false
}

// TableRow is one line of the report: the totals, a package, or a file.
type TableRow struct {
	Label string
	Depth int
	Stats Stats
}

// Rows builds the report as totals first, then each package followed by its files.
func Rows(summary Summary, resolve Resolver, shorten func(string) string) []TableRow {
	if shorten == nil {
		shorten = func(path string) string { return path }
	}
	stats := map[string]Stats{}
	for _, file := range summary.Files {
		source, _ := readSource(file.Path, resolve)
		stats[file.Path] = Analyze(file, source)
	}
	rows := []TableRow{{Label: "All files"}}
	total := Stats{}
	for _, pkg := range summary.Packages {
		packageStats := Stats{}
		files := append([]File(nil), pkg.Files...)
		sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
		fileRows := make([]TableRow, 0, len(files))
		for _, file := range files {
			fileStats := stats[file.Path]
			packageStats = packageStats.Add(fileStats)
			fileRows = append(fileRows, TableRow{Label: base(file.Path), Depth: 2, Stats: fileStats})
		}
		rows = append(rows, TableRow{Label: shorten(pkg.ImportPath), Depth: 1, Stats: packageStats})
		rows = append(rows, fileRows...)
		total = total.Add(packageStats)
	}
	rows[0].Stats = total
	return rows
}

func base(path string) string {
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		return path[i+1:]
	}
	return path
}
