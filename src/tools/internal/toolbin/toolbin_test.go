package toolbin

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/sonquer/tui4db/src/tools/internal/exec"
)

type recordingRunner struct {
	calls  []string
	dirs   []string
	result exec.Result
	err    error
	emit   bool
}

func (r *recordingRunner) Run(_ context.Context, dir string, name string, args ...string) (exec.Result, error) {
	r.calls = append(r.calls, exec.Format(name, args...))
	r.dirs = append(r.dirs, dir)
	if r.emit {
		for i, arg := range args {
			if arg == "-o" && i+1 < len(args) {
				if err := os.WriteFile(args[i+1], []byte("binary"), 0o755); err != nil {
					return exec.Result{}, err
				}
			}
		}
	}
	return r.result, r.err
}

func newBuilder(t *testing.T, runner exec.Runner) Builder {
	t.Helper()
	return Builder{Runner: runner, ToolsDir: t.TempDir(), BinDir: filepath.Join(t.TempDir(), ".local", "bin")}
}

func TestEnsureBuildsOnce(t *testing.T) {
	runner := &recordingRunner{emit: true}
	builder := newBuilder(t, runner)

	path, err := builder.Ensure(context.Background(), Lint)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the tool must be there: %v", err)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("calls = %v", runner.calls)
	}
	if !strings.Contains(runner.calls[0], "go build -o") || !strings.Contains(runner.calls[0], Lint.Package) {
		t.Errorf("command = %q", runner.calls[0])
	}
	if runner.dirs[0] != builder.ToolsDir {
		t.Error("the tool must be built from the module that pins it")
	}

	again, err := builder.Ensure(context.Background(), Lint)
	if err != nil || again != path {
		t.Fatalf("Ensure again = %q, %v", again, err)
	}
	if len(runner.calls) != 1 {
		t.Errorf("a tool that is already built must not be built again: %v", runner.calls)
	}
}

func TestEnsureReportsFailures(t *testing.T) {
	t.Run("runner error", func(t *testing.T) {
		builder := newBuilder(t, &recordingRunner{err: errors.New("no toolchain")})
		if _, err := builder.Ensure(context.Background(), Vuln); err == nil {
			t.Fatal("want an error")
		}
	})
	t.Run("build failed", func(t *testing.T) {
		builder := newBuilder(t, &recordingRunner{result: exec.Result{ExitCode: 1, Stderr: "compile error"}})
		_, err := builder.Ensure(context.Background(), Vuln)
		if err == nil || !strings.Contains(err.Error(), "compile error") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("unwritable directory", func(t *testing.T) {
		blocked := filepath.Join(t.TempDir(), "file")
		if err := os.WriteFile(blocked, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		builder := Builder{Runner: &recordingRunner{}, ToolsDir: t.TempDir(), BinDir: filepath.Join(blocked, "bin")}
		if _, err := builder.Ensure(context.Background(), Vuln); err == nil {
			t.Fatal("want an error")
		}
	})
}

func TestPathCarriesTheExtensionWindowsNeeds(t *testing.T) {
	builder := Builder{BinDir: "/bin"}
	path := builder.Path(Lint)
	if runtime.GOOS == "windows" && !strings.HasSuffix(path, ".exe") {
		t.Errorf("path = %q", path)
	}
	if runtime.GOOS != "windows" && strings.HasSuffix(path, ".exe") {
		t.Errorf("path = %q", path)
	}
}
