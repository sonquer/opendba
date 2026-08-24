// Package sqlfiles is the statements you keep on disk, one directory per
// connection.
package sqlfiles

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sonquer/opendba/src/cli/internal/config"
)

// Extension is what a file has to end in to be one of these.
const Extension = ".sql"

const (
	dirMode  = 0o700
	fileMode = 0o600
)

// File is one statement kept on disk.
type File struct {
	Name string
	Path string
}

// Root is the directory this connection's files are kept in, or empty when
// there is nowhere to keep them.
func Root(paths config.Paths, setting string, connection config.Connection) string {
	base := strings.TrimSpace(setting)
	if base == "" {
		base = paths.SQLDir()
	}
	if !filepath.IsAbs(base) {
		return ""
	}
	return filepath.Join(base, folder(connection))
}

// folder is the connection's own directory, named after it when the name
// survives being turned into one and after its id when it does not.
func folder(connection config.Connection) string {
	if named := Clean(connection.Name); named != "" {
		return named
	}
	return Clean(connection.ID)
}

// Clean takes out what a file name cannot hold, so a tab called
// catalog.product_prices does not become a directory nobody meant.
func Clean(name string) string {
	cleaned := strings.Map(func(r rune) rune {
		if strings.ContainsRune(`/\:*?"<>|`, r) || r < ' ' {
			return '-'
		}
		if r == ' ' {
			return '-'
		}
		return r
	}, name)
	return strings.Trim(cleaned, "-")
}

// Named is what somebody typed, turned into a file name or refused.
func Named(typed string) (string, error) {
	name := strings.TrimSpace(typed)
	if name == "" {
		return "", errors.New("a file needs a name")
	}
	if strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") {
		return "", fmt.Errorf("%q is a path, not a name", typed)
	}
	if strings.HasPrefix(name, ".") {
		return "", fmt.Errorf("%q starts with a dot, which hides it", typed)
	}
	if !strings.HasSuffix(strings.ToLower(name), Extension) {
		name += Extension
	}
	if cleaned := Clean(name); cleaned != name {
		return "", fmt.Errorf("%q cannot be a file name", typed)
	}
	return name, nil
}

// Inside is where a name sits under a root, and an error when it would sit
// anywhere else.
func Inside(root, name string) (string, error) {
	if root == "" {
		return "", errors.New("there is nowhere to keep sql files")
	}
	base := filepath.Base(name)
	if base != name || name == "." || name == ".." {
		return "", fmt.Errorf("%q is a path, not a name", name)
	}
	path := filepath.Join(root, name)
	if filepath.Dir(path) != filepath.Clean(root) {
		return "", fmt.Errorf("%q is outside the workspace", name)
	}
	return path, nil
}

// List is every statement kept under root, by name. A root that is not there
// yet holds nothing, which is not a failure.
func List(root string) ([]File, error) {
	if root == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", root, err)
	}
	files := make([]File, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), Extension) {
			continue
		}
		files = append(files, File{
			Name: entry.Name(),
			Path: filepath.Join(root, entry.Name()),
		})
	}
	sort.Slice(files, func(a, b int) bool { return files[a].Name < files[b].Name })
	return files, nil
}

// Read is what one of them holds.
func Read(root, name string) (string, error) {
	path, err := Inside(root, name)
	if err != nil {
		return "", err
	}
	held, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", name, err)
	}
	return string(held), nil
}

// Write puts a statement in a file, replacing what was there.
func Write(root, name, statement string) (string, error) {
	path, err := Inside(root, name)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(root, dirMode); err != nil {
		return "", fmt.Errorf("make %s: %w", root, err)
	}
	temp := filepath.Join(root, "."+name+".tmp")
	if err := os.WriteFile(temp, []byte(statement), fileMode); err != nil {
		return "", fmt.Errorf("write %s: %w", name, err)
	}
	if err := os.Rename(temp, path); err != nil {
		_ = os.Remove(temp)
		return "", fmt.Errorf("write %s: %w", name, err)
	}
	return path, nil
}

// Create puts a statement in a file that is not there yet, and refuses to
// replace one that is.
func Create(root, name, statement string) (string, error) {
	path, err := Inside(root, name)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(path); err == nil {
		return "", fmt.Errorf("%s is already there", name)
	}
	return Write(root, name, statement)
}

// Remove takes one away.
func Remove(root, name string) error {
	path, err := Inside(root, name)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove %s: %w", name, err)
	}
	return nil
}
