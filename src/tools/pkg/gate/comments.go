// Package gate enforces the rule that no comment in this repository may explain
// code.
//
// Documentation comments are the point of the exception: a comment attached to a
// package clause, a type, a function, a constant, a variable, a struct field or
// an interface method is what pkg.go.dev renders and is always allowed. Anything
// else, a comment inside a function body, a comment trailing a line of code, a
// block of commented-out code, is a finding, because code that needs prose to be
// understood should be renamed or split instead.
package gate

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Finding is one comment that explains code instead of documenting a declaration.
type Finding struct {
	File string
	Line int
	Text string
}

// String renders the finding as file:line: text.
func (f Finding) String() string {
	return fmt.Sprintf("%s:%d: %s", f.File, f.Line, f.Text)
}

var allowedPrefixes = []string{
	"//go:build",
	"//go:generate",
	"//go:embed",
	"//go:noinline",
	"//go:linkname",
	"//line ",
	"//export ",
	"// +build",
}

// IsDirective reports whether a comment is an instruction to the toolchain
// rather than prose.
func IsDirective(text string) bool {
	for _, p := range allowedPrefixes {
		if strings.HasPrefix(text, p) {
			return true
		}
	}
	return false
}

var generatedPattern = regexp.MustCompile(`^// Code generated .* DO NOT EDIT\.$`)

// IsGenerated reports whether a file carries the standard generated marker above
// its package clause.
func IsGenerated(source []byte) bool {
	for line := range strings.Lines(string(source)) {
		trimmed := strings.TrimRight(line, "\r\n")
		if strings.HasPrefix(trimmed, "package ") {
			return false
		}
		if generatedPattern.MatchString(trimmed) {
			return true
		}
	}
	return false
}

var skippedDirs = map[string]bool{
	"generated":    true,
	".git":         true,
	"vendor":       true,
	"node_modules": true,
	"testdata":     true,
	"bin":          true,
	"dist":         true,
}

// ScanFS walks a file system and reports every comment that does not document a
// declaration, sorted by file and line. Generated files and vendored directories
// are skipped.
func ScanFS(fsys fs.FS) ([]Finding, error) {
	var findings []Finding
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != "." && (skippedDirs[d.Name()] || strings.HasPrefix(d.Name(), ".")) {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") {
			return nil
		}
		src, err := fs.ReadFile(fsys, p)
		if err != nil {
			return fmt.Errorf("read %s: %w", p, err)
		}
		if IsGenerated(src) {
			return nil
		}
		found, err := scanSource(p, src)
		if err != nil {
			return err
		}
		findings = append(findings, found...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		return findings[i].Line < findings[j].Line
	})
	return findings, nil
}

func scanSource(name string, src []byte) ([]Finding, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, name, src, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", name, err)
	}
	allowed := documentationComments(file)
	var findings []Finding
	for _, group := range file.Comments {
		for _, c := range group.List {
			if IsDirective(c.Text) || allowed[c.Pos()] {
				continue
			}
			findings = append(findings, Finding{
				File: path.Clean(name),
				Line: fset.Position(c.Pos()).Line,
				Text: firstLine(c.Text),
			})
		}
	}
	return findings, nil
}

func documentationComments(file *ast.File) map[token.Pos]bool {
	allowed := map[token.Pos]bool{}
	allow := func(group *ast.CommentGroup) {
		if group == nil {
			return
		}
		for _, c := range group.List {
			allowed[c.Pos()] = true
		}
	}
	allow(file.Doc)
	for _, declaration := range file.Decls {
		switch typed := declaration.(type) {
		case *ast.FuncDecl:
			allow(typed.Doc)
		case *ast.GenDecl:
			allow(typed.Doc)
			for _, spec := range typed.Specs {
				allowSpec(spec, allow)
			}
		}
	}
	return allowed
}

func allowSpec(spec ast.Spec, allow func(*ast.CommentGroup)) {
	switch typed := spec.(type) {
	case *ast.TypeSpec:
		allow(typed.Doc)
		allowFields(typed.Type, allow)
	case *ast.ValueSpec:
		allow(typed.Doc)
	case *ast.ImportSpec:
		allow(typed.Doc)
	}
}

func allowFields(node ast.Expr, allow func(*ast.CommentGroup)) {
	var fields *ast.FieldList
	switch typed := node.(type) {
	case *ast.StructType:
		fields = typed.Fields
	case *ast.InterfaceType:
		fields = typed.Methods
	default:
		return
	}
	if fields == nil {
		return
	}
	for _, field := range fields.List {
		allow(field.Doc)
	}
}

func firstLine(text string) string {
	if i := strings.IndexByte(text, '\n'); i >= 0 {
		text = text[:i] + " …"
	}
	if len(text) > 72 {
		text = text[:72] + "…"
	}
	return strings.TrimSpace(text)
}

// Scan walks a directory on disk. See ScanFS.
func Scan(root string) ([]Finding, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("scan root: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("scan root %s is not a directory", filepath.Clean(root))
	}
	return ScanFS(os.DirFS(root))
}
