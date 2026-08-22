package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func fixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write(t, filepath.Join(root, "go.work"), "go 1.27.0\n\nuse (\n\t./src/cli\n\t./src/tools\n)\n")
	write(t, filepath.Join(root, "src", "cli", "go.mod"), "module example.com/m/src/cli\n\ngo 1.27.0\n")
	write(t, filepath.Join(root, "src", "tools", "go.mod"), "module example.com/m/src/tools\n\ngo 1.27.0\n")
	return root
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLoadReadsModules(t *testing.T) {
	root := fixture(t)
	ws, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(ws.Modules) != 2 {
		t.Fatalf("modules = %v", ws.Modules)
	}
	if got := ws.Names(); got[0] != "cli" || got[1] != "tools" {
		t.Errorf("Names() = %v", got)
	}
	cli, ok := ws.Find("cli")
	if !ok {
		t.Fatal("cli module not found")
	}
	if cli.Path != "example.com/m/src/cli" {
		t.Errorf("module path = %q", cli.Path)
	}
	if got := cli.RelDir(root); got != "src/cli" {
		t.Errorf("RelDir() = %q", got)
	}
	if _, ok := ws.Find("missing"); ok {
		t.Error("Find(missing) must report false")
	}
}

func TestRelDirFallsBackToAbsolutePath(t *testing.T) {
	m := Module{Dir: filepath.FromSlash("/tmp/x")}
	if got := m.RelDir("relative-root"); got != m.Dir {
		t.Errorf("RelDir() = %q, want %q", got, m.Dir)
	}
}

func TestDiscoverWalksUp(t *testing.T) {
	root := fixture(t)
	deep := filepath.Join(root, "src", "cli", "internal", "ui")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	ws, err := Discover(deep)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(ws.Modules) != 2 {
		t.Fatalf("modules = %v", ws.Modules)
	}
}

func TestDiscoverFailsWithoutWorkspace(t *testing.T) {
	if _, err := Discover(t.TempDir()); err == nil {
		t.Fatal("want error without go.work")
	}
}

func TestLoadFailures(t *testing.T) {
	t.Run("missing go.work", func(t *testing.T) {
		if _, err := Load(t.TempDir()); err == nil {
			t.Fatal("want error")
		}
	})
	t.Run("unparsable go.work", func(t *testing.T) {
		root := t.TempDir()
		write(t, filepath.Join(root, "go.work"), "this is not a workspace\n")
		if _, err := Load(root); err == nil {
			t.Fatal("want error")
		}
	})
	t.Run("no modules", func(t *testing.T) {
		root := t.TempDir()
		write(t, filepath.Join(root, "go.work"), "go 1.27.0\n")
		if _, err := Load(root); err == nil {
			t.Fatal("want error")
		}
	})
	t.Run("missing go.mod", func(t *testing.T) {
		root := t.TempDir()
		write(t, filepath.Join(root, "go.work"), "go 1.27.0\n\nuse ./src/cli\n")
		if _, err := Load(root); err == nil {
			t.Fatal("want error")
		}
	})
	t.Run("unparsable go.mod", func(t *testing.T) {
		root := t.TempDir()
		write(t, filepath.Join(root, "go.work"), "go 1.27.0\n\nuse ./src/cli\n")
		write(t, filepath.Join(root, "src", "cli", "go.mod"), "module\n")
		if _, err := Load(root); err == nil {
			t.Fatal("want error")
		}
	})
	t.Run("go.mod without module directive", func(t *testing.T) {
		root := t.TempDir()
		write(t, filepath.Join(root, "go.work"), "go 1.27.0\n\nuse ./src/cli\n")
		write(t, filepath.Join(root, "src", "cli", "go.mod"), "go 1.27.0\n")
		if _, err := Load(root); err == nil {
			t.Fatal("want error")
		}
	})
}

func TestSourcePathAndResolve(t *testing.T) {
	root := fixture(t)
	ws, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := ws.Resolve("example.com/m/src/cli/internal/ui/theme.go")
	if !ok {
		t.Fatal("Resolve must find the cli module")
	}
	want := filepath.Join(root, "src", "cli", "internal", "ui", "theme.go")
	if got != want {
		t.Errorf("Resolve() = %q, want %q", got, want)
	}
	if _, ok := ws.Resolve("other.com/x/y.go"); ok {
		t.Error("Resolve must reject foreign import paths")
	}
	module := ws.Modules[0]
	if dir, ok := module.SourcePath(module.Path); !ok || dir != module.Dir {
		t.Errorf("SourcePath(module) = %q, %v", dir, ok)
	}
	if _, ok := module.SourcePath(module.Path + "x"); ok {
		t.Error("prefix must match on a path boundary")
	}
}
