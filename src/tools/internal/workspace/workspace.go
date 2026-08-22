package workspace

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/mod/modfile"
)

type Module struct {
	Name string
	Path string
	Dir  string
}

func (m Module) RelDir(root string) string {
	rel, err := filepath.Rel(root, m.Dir)
	if err != nil {
		return m.Dir
	}
	return filepath.ToSlash(rel)
}

type Workspace struct {
	Root    string
	Modules []Module
}

func (w Workspace) Find(name string) (Module, bool) {
	for _, m := range w.Modules {
		if m.Name == name {
			return m, true
		}
	}
	return Module{}, false
}

func (w Workspace) Names() []string {
	names := make([]string, 0, len(w.Modules))
	for _, m := range w.Modules {
		names = append(names, m.Name)
	}
	return names
}

func Discover(start string) (Workspace, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return Workspace{}, fmt.Errorf("resolve start directory: %w", err)
	}
	for {
		candidate := filepath.Join(dir, "go.work")
		if _, err := os.Stat(candidate); err == nil {
			return Load(dir)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return Workspace{}, fmt.Errorf("no go.work found above %s", start)
		}
		dir = parent
	}
}

func Load(root string) (Workspace, error) {
	path := filepath.Join(root, "go.work")
	data, err := os.ReadFile(path)
	if err != nil {
		return Workspace{}, fmt.Errorf("read go.work: %w", err)
	}
	work, err := modfile.ParseWork(path, data, nil)
	if err != nil {
		return Workspace{}, fmt.Errorf("parse go.work: %w", err)
	}
	ws := Workspace{Root: root}
	for _, use := range work.Use {
		dir := filepath.Join(root, filepath.FromSlash(use.Path))
		modPath, err := modulePath(dir)
		if err != nil {
			return Workspace{}, err
		}
		ws.Modules = append(ws.Modules, Module{
			Name: filepath.Base(dir),
			Path: modPath,
			Dir:  dir,
		})
	}
	if len(ws.Modules) == 0 {
		return Workspace{}, fmt.Errorf("go.work in %s declares no modules", root)
	}
	return ws, nil
}

func modulePath(dir string) (string, error) {
	path := filepath.Join(dir, "go.mod")
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read go.mod: %w", err)
	}
	parsed, err := modfile.Parse(path, data, nil)
	if err != nil {
		return "", fmt.Errorf("parse go.mod: %w", err)
	}
	if parsed.Module == nil {
		return "", fmt.Errorf("go.mod in %s has no module directive", dir)
	}
	return parsed.Module.Mod.Path, nil
}

func (m Module) SourcePath(importPath string) (string, bool) {
	if importPath == m.Path {
		return m.Dir, true
	}
	prefix := m.Path + "/"
	if len(importPath) <= len(prefix) || importPath[:len(prefix)] != prefix {
		return "", false
	}
	return filepath.Join(m.Dir, filepath.FromSlash(importPath[len(prefix):])), true
}

func (w Workspace) Resolve(importPath string) (string, bool) {
	for _, m := range w.Modules {
		if path, ok := m.SourcePath(importPath); ok {
			return path, true
		}
	}
	return "", false
}
