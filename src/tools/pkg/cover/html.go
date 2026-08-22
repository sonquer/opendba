package cover

import (
	"fmt"
	"html/template"
	"io"
	"os"
	"path"
	"sort"
	"strings"
)

// Resolver maps the import path of a file to its location on disk.
type Resolver func(importPath string) (string, bool)

// ReportOptions configures the HTML report.
type ReportOptions struct {
	Title     string
	Min       float64
	Resolve   Resolver
	Generated string
	Sections  []string
}

type metric struct {
	Percent  string
	Label    string
	Fraction string
	Level    string
}

type sourceLine struct {
	Number int
	Hits   string
	State  string
	Text   string
}

type fileView struct {
	ID      string
	Package string
	Name    string
	Metrics []metric
	Level   string
	Lines   []sourceLine
	Missing bool
}

type row struct {
	Name       string
	Link       string
	Percent    string
	Covered    int
	Statements int
	Level      string
	Fill       string
	Empty      string
	IsPackage  bool
}

type reportView struct {
	Title     string
	Generated string
	Sections  string
	Metrics   []metric
	Level     string
	Rows      []row
	Files     []fileView
}

// WriteReportFile writes the report to a file. See WriteReport.
func WriteReportFile(path string, s Summary, opts ReportOptions) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create coverage report: %w", err)
	}
	defer func() { _ = f.Close() }()
	return WriteReport(f, s, opts)
}

// WriteReport writes one self-contained HTML document in the shape Istanbul
// uses: a summary of the whole run, a table of packages and files, and the
// source of every file it can resolve with covered and uncovered lines marked.
// It needs no javascript and no external assets.
func WriteReport(w io.Writer, s Summary, opts ReportOptions) error {
	if opts.Title == "" {
		opts.Title = "coverage"
	}
	view := reportView{
		Title:     opts.Title,
		Generated: opts.Generated,
		Sections:  strings.Join(opts.Sections, ", "),
		Metrics:   summaryMetrics(s, opts),
		Level:     level(s.Percent(), opts.Min),
	}
	for _, pkg := range s.Packages {
		view.Rows = append(view.Rows, packageRow(pkg, opts))
		files := append([]File(nil), pkg.Files...)
		sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
		for _, file := range files {
			view.Rows = append(view.Rows, fileRow(pkg, file, opts))
			view.Files = append(view.Files, fileViewFor(pkg, file, opts))
		}
	}
	if err := reportTemplate.Execute(w, view); err != nil {
		return fmt.Errorf("render coverage report: %w", err)
	}
	return nil
}

func summaryMetrics(s Summary, opts ReportOptions) []metric {
	stats := Stats{}
	for _, file := range s.Files {
		source, _ := readSource(file.Path, opts.Resolve)
		stats = stats.Add(Analyze(file, source))
	}
	return metricsFor(stats, opts.Min)
}

func metricsFor(stats Stats, min float64) []metric {
	return []metric{
		{
			Percent:  format(stats.StatementPercent()),
			Label:    "Statements",
			Fraction: fmt.Sprintf("%d/%d", stats.CoveredStatements, stats.Statements),
			Level:    level(stats.StatementPercent(), min),
		},
		{
			Percent:  format(stats.FuncPercent()),
			Label:    "Functions",
			Fraction: fmt.Sprintf("%d/%d", stats.CoveredFuncs, stats.Funcs),
			Level:    level(stats.FuncPercent(), min),
		},
		{
			Percent:  format(stats.LinePercent()),
			Label:    "Lines",
			Fraction: fmt.Sprintf("%d/%d", stats.CoveredLines, stats.Lines),
			Level:    level(stats.LinePercent(), min),
		},
	}
}

func packageRow(pkg Package, opts ReportOptions) row {
	return row{
		Name:       pkg.ImportPath,
		Percent:    format(pkg.Percent()),
		Covered:    pkg.Covered,
		Statements: pkg.Statements,
		Level:      level(pkg.Percent(), opts.Min),
		Fill:       width(pkg.Percent()),
		Empty:      width(100 - pkg.Percent()),
		IsPackage:  true,
	}
}

func fileRow(pkg Package, file File, opts ReportOptions) row {
	return row{
		Name:       path.Base(file.Path),
		Link:       "#" + anchor(file.Path),
		Percent:    format(file.Percent()),
		Covered:    file.Covered,
		Statements: file.Statements,
		Level:      level(file.Percent(), opts.Min),
		Fill:       width(file.Percent()),
		Empty:      width(100 - file.Percent()),
	}
}

func fileViewFor(pkg Package, file File, opts ReportOptions) fileView {
	view := fileView{
		ID:      anchor(file.Path),
		Package: pkg.ImportPath,
		Name:    path.Base(file.Path),
		Level:   level(file.Percent(), opts.Min),
	}
	source, ok := readSource(file.Path, opts.Resolve)
	if !ok {
		view.Missing = true
		view.Metrics = metricsFor(Stats{Statements: file.Statements, CoveredStatements: file.Covered}, opts.Min)
		return view
	}
	stats := Analyze(file, source)
	view.Metrics = metricsFor(stats, opts.Min)
	view.Lines = annotate(source, LineHits(file))
	return view
}

func annotate(source string, hits map[int]int) []sourceLine {
	raw := strings.Split(strings.ReplaceAll(source, "\r\n", "\n"), "\n")
	if n := len(raw); n > 0 && raw[n-1] == "" {
		raw = raw[:n-1]
	}
	lines := make([]sourceLine, 0, len(raw))
	for i, text := range raw {
		number := i + 1
		line := sourceLine{Number: number, Text: text, State: "neutral"}
		if hit, ok := hits[number]; ok {
			line.Hits = fmt.Sprintf("%dx", hit)
			line.State = "yes"
			if hit == 0 {
				line.Hits = ""
				line.State = "no"
			}
		}
		lines = append(lines, line)
	}
	return lines
}

func readSource(importPath string, resolve Resolver) (string, bool) {
	if resolve == nil {
		return "", false
	}
	file, ok := resolve(importPath)
	if !ok {
		return "", false
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return "", false
	}
	return string(data), true
}

func anchor(importPath string) string {
	return strings.NewReplacer("/", "-", ".", "-", ":", "-").Replace(importPath)
}

func format(pct float64) string { return fmt.Sprintf("%.2f%%", pct) }

func width(pct float64) string { return fmt.Sprintf("%.2f", pct) }

func level(pct, min float64) string {
	if min <= 0 {
		min = 90
	}
	switch {
	case pct >= min:
		return "high"
	case pct >= min/2:
		return "medium"
	default:
		return "low"
	}
}

var reportTemplate = template.Must(template.New("report").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Code coverage report for {{.Title}}</title>
<style>
body { font-family: "Helvetica Neue", Helvetica, Arial, sans-serif; font-size: 14px; color: #333;
       background: #fff; margin: 0; padding: 0; }
.wrapper { max-width: 1100px; margin: 0 auto; padding: 0 20px 60px; }
h1 { font-size: 20px; font-weight: normal; margin: 24px 0 8px; }
h1 .parent { color: #777; }
.quiet { color: #7f7f7f; }
.strong { font-weight: bold; }
.fraction { font-family: Consolas, "Liberation Mono", Menlo, Courier, monospace; font-size: 10px;
            color: #555; background: #e8e8e8; padding: 1px 4px; margin-left: 4px; border-radius: 2px; }
.pad1y { padding: 10px 0; }
.space-right2 { margin-right: 24px; }
.clearfix { overflow: auto; }
.fl { float: left; }
.status-line { height: 10px; margin: 0 0 16px; }
.status-line.high { background: rgb(77,146,33); }
.status-line.medium { background: #f9cd0b; }
.status-line.low { background: #c21f39; }
.meta { color: #7f7f7f; font-size: 12px; padding-bottom: 10px; }
table.coverage-summary { width: 100%; border-collapse: collapse; margin-bottom: 32px; }
table.coverage-summary td, table.coverage-summary th { border: 1px solid #bbb; padding: 10px; }
table.coverage-summary th { text-align: left; font-weight: normal; background: #f6f6f6; }
table.coverage-summary td.pct, table.coverage-summary td.abs { text-align: right; white-space: nowrap; }
table.coverage-summary td.file a { color: #333; text-decoration: none; border-bottom: 1px dotted #999; }
table.coverage-summary tr.package td { background: #f2f2f2; font-weight: bold; }
table.coverage-summary tr.file td.file { padding-left: 26px; }
td.pct.high, .metric.high .strong { color: rgb(77,146,33); }
td.pct.medium, .metric.medium .strong { color: #a08000; }
td.pct.low, .metric.low .strong { color: #c21f39; }
.chart { width: 120px; border: 1px solid #555; height: 12px; padding: 0; line-height: 0; }
.cover-fill, .cover-empty { display: inline-block; height: 12px; }
.cover-fill { background: rgb(77,146,33); }
.cover-empty { background: #fff; }
.file-report { border-top: 1px solid #ddd; margin-top: 32px; padding-top: 8px; }
.file-report h2 { font-size: 16px; font-weight: normal; margin: 8px 0; }
.file-report h2 .parent { color: #777; }
.back { float: right; font-size: 12px; color: #777; text-decoration: none; padding-top: 12px; }
pre { font-family: Consolas, "Liberation Mono", Menlo, Courier, monospace; font-size: 12px;
      line-height: 1.4; margin: 0; tab-size: 2; }
.source { border: 1px solid #ddd; border-collapse: collapse; width: 100%; }
.source td { padding: 0; vertical-align: top; }
.source td.line-count { text-align: right; padding: 0 8px; min-width: 20px; color: #aaa;
                        border-right: 1px solid #ddd; user-select: none; }
.source td.line-coverage { text-align: right; padding: 0 10px 0 0; min-width: 20px; user-select: none; }
.source td.text { width: 100%; padding-left: 10px; overflow-x: auto; }
.cline-yes { background: rgb(230,245,208); color: rgb(77,146,33); }
.cline-no { background: #fce1e5; color: #c21f39; }
.cline-neutral { background: #fff; }
.missing { color: #7f7f7f; padding: 12px 0; }
</style></head>
<body>
<div class="wrapper">
<h1>Code coverage report for <span class="strong">{{.Title}}</span></h1>
<div class="clearfix">
{{range .Metrics}}
  <div class="fl pad1y space-right2 metric {{.Level}}">
    <span class="strong">{{.Percent}}</span>
    <span class="quiet">{{.Label}}</span>
    <span class="fraction">{{.Fraction}}</span>
  </div>
{{end}}
</div>
<div class="status-line {{.Level}}"></div>
<div class="meta">{{if .Sections}}{{.Sections}}{{end}}{{if .Generated}} · generated {{.Generated}}{{end}}</div>

<table class="coverage-summary">
<thead><tr><th>File</th><th>&nbsp;</th><th class="pct">Statements</th><th class="abs">&nbsp;</th></tr></thead>
<tbody>
{{range .Rows}}
<tr class="{{if .IsPackage}}package{{else}}file{{end}}">
  <td class="file">{{if .Link}}<a href="{{.Link}}">{{.Name}}</a>{{else}}{{.Name}}{{end}}</td>
  <td class="pic"><div class="chart"><div class="cover-fill" style="width: {{.Fill}}%"></div><div class="cover-empty" style="width: {{.Empty}}%"></div></div></td>
  <td class="pct {{.Level}}">{{.Percent}}</td>
  <td class="abs">{{.Covered}}/{{.Statements}}</td>
</tr>
{{end}}
</tbody>
</table>

{{range .Files}}
<section class="file-report" id="{{.ID}}">
<a class="back" href="#">All files</a>
<h2><span class="parent">{{.Package}}/</span>{{.Name}}</h2>
<div class="clearfix">
{{range .Metrics}}
  <div class="fl pad1y space-right2 metric {{.Level}}">
    <span class="strong">{{.Percent}}</span>
    <span class="quiet">{{.Label}}</span>
    <span class="fraction">{{.Fraction}}</span>
  </div>
{{end}}
</div>
<div class="status-line {{.Level}}"></div>
{{if .Missing}}<p class="missing">source not available</p>{{else}}
<table class="source"><tbody>
{{range .Lines}}<tr class="cline-{{.State}}"><td class="line-count">{{.Number}}</td><td class="line-coverage">{{.Hits}}</td><td class="text"><pre>{{.Text}}</pre></td></tr>
{{end}}</tbody></table>
{{end}}
</section>
{{end}}
</div>
</body></html>
`))
