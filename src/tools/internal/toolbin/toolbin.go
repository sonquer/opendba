package toolbin

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/sonquer/tui4db/src/tools/internal/exec"
)

const DirName = ".local/bin"

type Tool struct {
	Name    string
	Package string
}

var (
	Lint = Tool{Name: "golangci-lint", Package: "github.com/golangci/golangci-lint/v2/cmd/golangci-lint"}
	Vuln = Tool{Name: "govulncheck", Package: "golang.org/x/vuln/cmd/govulncheck"}
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
	result, err := b.Runner.Run(ctx, b.ToolsDir, "go", "build", "-o", path, tool.Package)
	if err != nil {
		return "", fmt.Errorf("build %s: %w", tool.Name, err)
	}
	if !result.OK() {
		return "", fmt.Errorf("build %s: %s", tool.Name, result.Output())
	}
	return path, nil
}
