package grammar

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sonquer/opendba/src/tools/internal/exec"
)

const generatedMarker = "// Code generated"

type Spec struct {
	Dialect string
	Package string
	Dir     string
	Lexer   string
	Parser  string
	Bases   []string
}

func Specs(moduleDir string) []Spec {
	root := filepath.Join(moduleDir, "internal", "parser", "generated")
	return []Spec{
		{
			Dialect: "postgresql",
			Package: "postgresql",
			Dir:     filepath.Join(root, "postgresql"),
			Lexer:   "PostgreSQLLexer.g4",
			Parser:  "PostgreSQLParser.g4",
			Bases:   []string{"postgresql_lexer_base.go", "postgresql_parser_base.go", "string_stack.go"},
		},
		{
			Dialect: "sqlite",
			Package: "sqlite",
			Dir:     filepath.Join(root, "sqlite"),
			Lexer:   "SQLiteLexer.g4",
			Parser:  "SQLiteParser.g4",
		},
		{
			Dialect: "tsql",
			Package: "tsql",
			Dir:     filepath.Join(root, "tsql"),
			Lexer:   "TSqlLexer.g4",
			Parser:  "TSqlParser.g4",
		},
	}
}

type Generator struct {
	Runner exec.Runner
	Java   string
	Jar    string
}

func (g Generator) Generate(ctx context.Context, spec Spec) ([]string, error) {
	staging, err := os.MkdirTemp("", "opendba-grammar-")
	if err != nil {
		return nil, fmt.Errorf("create staging directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(staging) }()

	for _, name := range []string{spec.Lexer, spec.Parser} {
		if err := stageGrammar(filepath.Join(spec.Dir, name), filepath.Join(staging, name), name == spec.Lexer); err != nil {
			return nil, err
		}
	}

	result, err := g.Runner.Run(ctx, staging, g.java(),
		"-jar", g.Jar,
		"-Dlanguage=Go",
		"-package", spec.Package,
		"-listener", "-no-visitor",
		spec.Lexer, spec.Parser,
	)
	if err != nil {
		return nil, fmt.Errorf("run antlr: %w", err)
	}
	if !result.OK() {
		return nil, fmt.Errorf("antlr failed for %s: %s", spec.Dialect, result.Output())
	}

	return install(staging, spec)
}

func (g Generator) java() string {
	if g.Java == "" {
		return "java"
	}
	return g.Java
}

func stageGrammar(source, target string, lexer bool) error {
	data, err := os.ReadFile(source)
	if err != nil {
		return fmt.Errorf("read grammar: %w", err)
	}
	if err := os.WriteFile(target, []byte(Transform(string(data), lexer)), 0o600); err != nil {
		return fmt.Errorf("stage grammar: %w", err)
	}
	return nil
}

func install(staging string, spec Spec) ([]string, error) {
	entries, err := os.ReadDir(staging)
	if err != nil {
		return nil, fmt.Errorf("read generated files: %w", err)
	}
	if err := removeStaleOutput(spec); err != nil {
		return nil, err
	}
	var written []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(staging, name))
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", name, err)
		}
		if err := os.WriteFile(filepath.Join(spec.Dir, name), data, 0o644); err != nil {
			return nil, fmt.Errorf("write %s: %w", name, err)
		}
		written = append(written, name)
	}
	if err := fixBasePackages(spec); err != nil {
		return nil, err
	}
	return written, nil
}

func removeStaleOutput(spec Spec) error {
	entries, err := os.ReadDir(spec.Dir)
	if err != nil {
		return fmt.Errorf("read %s: %w", spec.Dir, err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || isBase(spec, name) {
			continue
		}
		path := filepath.Join(spec.Dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}
		if !strings.HasPrefix(string(data), generatedMarker) {
			continue
		}
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("remove %s: %w", name, err)
		}
	}
	return nil
}

func isBase(spec Spec, name string) bool {
	for _, base := range spec.Bases {
		if base == name {
			return true
		}
	}
	return false
}

func fixBasePackages(spec Spec) error {
	for _, base := range spec.Bases {
		path := filepath.Join(spec.Dir, base)
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", base, err)
		}
		fixed := RewritePackage(string(data), spec.Package)
		if fixed == string(data) {
			continue
		}
		if err := os.WriteFile(path, []byte(fixed), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", base, err)
		}
	}
	return nil
}

func RewritePackage(content, name string) string {
	lines := strings.SplitAfter(content, "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "package ") {
			lines[i] = "package " + name + "\n"
			break
		}
	}
	return strings.Join(lines, "")
}
