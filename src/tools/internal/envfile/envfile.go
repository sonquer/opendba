package envfile

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const FileName = ".env"

var pathVariables = map[string]bool{
	"XDG_CONFIG_HOME": true,
	"XDG_STATE_HOME":  true,
	"XDG_CACHE_HOME":  true,
	"XDG_DATA_HOME":   true,
}

type Values map[string]string

func Parse(r io.Reader) (Values, error) {
	values := Values{}
	scanner := bufio.NewScanner(r)
	line := 0
	for scanner.Scan() {
		line++
		name, value, err := parseLine(scanner.Text())
		if err != nil {
			return nil, fmt.Errorf("%s line %d: %w", FileName, line, err)
		}
		if name == "" {
			continue
		}
		values[name] = value
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read %s: %w", FileName, err)
	}
	return values, nil
}

func parseLine(raw string) (string, string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return "", "", nil
	}
	trimmed = strings.TrimPrefix(trimmed, "export ")
	name, value, found := strings.Cut(trimmed, "=")
	name = strings.TrimSpace(name)
	if !found || name == "" {
		return "", "", errors.New("expected NAME=value")
	}
	return name, unquote(strings.TrimSpace(value)), nil
}

func unquote(value string) string {
	if len(value) >= 2 {
		first, last := value[0], value[len(value)-1]
		if first == last && (first == '"' || first == '\'') {
			return value[1 : len(value)-1]
		}
	}
	if index := strings.Index(value, " #"); index >= 0 {
		return strings.TrimSpace(value[:index])
	}
	return value
}

func Load(root string) (Values, error) {
	data, err := os.ReadFile(filepath.Join(root, FileName))
	if errors.Is(err, fs.ErrNotExist) {
		return Values{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", FileName, err)
	}
	values, err := Parse(strings.NewReader(string(data)))
	if err != nil {
		return nil, err
	}
	return values.Resolve(root), nil
}

func (v Values) Resolve(root string) Values {
	resolved := Values{}
	for name, value := range v {
		if pathVariables[name] && value != "" && !filepath.IsAbs(value) {
			value = filepath.Join(root, value)
		}
		resolved[name] = value
	}
	return resolved
}

func (v Values) Environment(base []string) []string {
	merged := append([]string(nil), base...)
	for _, name := range v.Names() {
		merged = append(merged, name+"="+v[name])
	}
	return merged
}

func (v Values) Names() []string {
	names := make([]string, 0, len(v))
	for name := range v {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (v Values) Describe() string {
	parts := make([]string, 0, len(v))
	for _, name := range v.Names() {
		parts = append(parts, name+"="+v[name])
	}
	return strings.Join(parts, " ")
}
