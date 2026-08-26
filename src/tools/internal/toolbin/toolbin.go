package toolbin

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/sonquer/opendba/src/tools/internal/exec"
)

const DirName = ".local/bin"

type Tool struct {
	Name    string
	Package string
	Version string
}

var (
	Lint = Tool{Name: "golangci-lint", Package: "github.com/golangci/golangci-lint/v2/cmd/golangci-lint"}
	Vuln = Tool{Name: "govulncheck", Package: "golang.org/x/vuln/cmd/govulncheck"}
	// Actions names its own version because it cannot be a tool dependency of
	// src/tools: it needs go.yaml.in/yaml/v4 at rc.3, and gosec, which arrives
	// with golangci-lint, needs rc.6.
	Actions = Tool{
		Name:    "actionlint",
		Package: "github.com/rhysd/actionlint/cmd/actionlint",
		Version: "v1.7.12",
	}
)

type Builder struct {
	Runner    exec.Runner
	ToolsDir  string
	BinDir    string
	Rebuilder func(name string) bool
}

func (b Builder) Path(tool Tool) string {
	name := tool.Name
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(b.BinDir, name)
}

func (b Builder) Ensure(ctx context.Context, tool Tool) (string, error) {
	path := b.Path(tool)
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	if err := os.MkdirAll(b.BinDir, 0o755); err != nil {
		return "", fmt.Errorf("create the tool directory: %w", err)
	}
	dir, cleanup, err := b.moduleFor(ctx, tool)
	if err != nil {
		return "", err
	}
	defer cleanup()
	result, err := b.Runner.Run(ctx, dir, "go", "build", "-o", path, tool.Package)
	if err != nil {
		return "", fmt.Errorf("build %s: %w", tool.Name, err)
	}
	if !result.OK() {
		return "", fmt.Errorf("build %s: %s", tool.Name, result.Output())
	}
	return path, nil
}

// moduleFor builds a tool that carries its own version outside the workspace,
// because the reason it carries one is that it cannot share its dependencies.
func (b Builder) moduleFor(ctx context.Context, tool Tool) (string, func(), error) {
	if tool.Version == "" {
		return b.ToolsDir, func() {}, nil
	}
	dir, err := os.MkdirTemp("", "opendba-tool-")
	if err != nil {
		return "", nil, fmt.Errorf("build %s: %w", tool.Name, err)
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	stanza := "module opendba.local/tool\n\ngo 1.26\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(stanza), 0o644); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("build %s: %w", tool.Name, err)
	}
	result, err := b.Runner.Run(ctx, dir, "go", "get", "-tool", tool.Package+"@"+tool.Version)
	if err != nil {
		cleanup()
		return "", nil, fmt.Errorf("fetch %s: %w", tool.Name, err)
	}
	if !result.OK() {
		cleanup()
		return "", nil, fmt.Errorf("fetch %s: %s", tool.Name, result.Output())
	}
	return dir, cleanup, nil
}
