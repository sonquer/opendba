package policy

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

const FileName = "dev.toml"

const DefaultTotal = 95.0

type Coverage struct {
	Total    float64            `toml:"total"`
	Packages float64            `toml:"packages"`
	Modules  map[string]float64 `toml:"modules"`
	Exempt   []string           `toml:"exempt"`
}

type Policy struct {
	Coverage Coverage           `toml:"coverage"`
	Package  map[string]float64 `toml:"package"`
}

func Default() Policy {
	return Policy{
		Coverage: Coverage{Total: DefaultTotal},
		Package:  map[string]float64{},
	}
}

func Load(root string) (Policy, error) {
	data, err := os.ReadFile(filepath.Join(root, FileName))
	if errors.Is(err, fs.ErrNotExist) {
		return Default(), nil
	}
	if err != nil {
		return Policy{}, fmt.Errorf("read %s: %w", FileName, err)
	}
	loaded := Default()
	if err := toml.Unmarshal(data, &loaded); err != nil {
		return Policy{}, fmt.Errorf("parse %s: %w", FileName, err)
	}
	if err := loaded.Validate(); err != nil {
		return Policy{}, err
	}
	return loaded, nil
}

func (p Policy) Validate() error {
	thresholds := map[string]float64{"coverage.total": p.Coverage.Total, "coverage.packages": p.Coverage.Packages}
	for name, value := range p.Coverage.Modules {
		thresholds["coverage.modules."+name] = value
	}
	for name, value := range p.Package {
		thresholds["package."+name] = value
	}
	names := make([]string, 0, len(thresholds))
	for name := range thresholds {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if value := thresholds[name]; value < 0 || value > 100 {
			return fmt.Errorf("%s must be between 0 and 100, got %g", name, value)
		}
	}
	return nil
}

func (p Policy) ModuleThreshold(module string) float64 {
	if threshold, ok := p.Coverage.Modules[module]; ok {
		return threshold
	}
	return p.Coverage.Total
}

func (p Policy) PackageThreshold(path string) float64 {
	best, bestLength := p.Coverage.Packages, -1
	for prefix, threshold := range p.Package {
		if !matches(path, prefix) || len(prefix) <= bestLength {
			continue
		}
		best, bestLength = threshold, len(prefix)
	}
	return best
}

func (p Policy) Exempt(path string) bool {
	for _, prefix := range p.Coverage.Exempt {
		if matches(path, prefix) {
			return true
		}
	}
	return false
}

func matches(path, prefix string) bool {
	return path == prefix || strings.HasPrefix(path, strings.TrimSuffix(prefix, "/")+"/")
}

func (p Policy) WithTotal(total float64) Policy {
	p.Coverage.Total = total
	p.Coverage.Modules = nil
	return p
}
