// Package semver is the versioning scheme this repository releases under.
package semver

import (
	"fmt"
	"strconv"
	"strings"
)

const ModulePrefix = "src/cli"

type Version struct {
	Major int
	Minor int
	Patch int
	Pre   string
}

type Kind int

const (
	Patch Kind = iota
	Minor
	Major
	Exact
)

func Parse(text string) (Version, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return Version{}, fmt.Errorf("version is empty")
	}
	core, pre, hasPre := strings.Cut(trimmed, "-")
	if hasPre {
		if err := validPrerelease(pre); err != nil {
			return Version{}, err
		}
	}
	fields := strings.Split(core, ".")
	if len(fields) != 3 {
		return Version{}, fmt.Errorf("version %q is not major.minor.patch", trimmed)
	}
	numbers := make([]int, 3)
	for i, field := range fields {
		number, err := number(field)
		if err != nil {
			return Version{}, fmt.Errorf("version %q: %w", trimmed, err)
		}
		numbers[i] = number
	}
	return Version{Major: numbers[0], Minor: numbers[1], Patch: numbers[2], Pre: pre}, nil
}

func number(field string) (int, error) {
	if field == "" {
		return 0, fmt.Errorf("a component is empty")
	}
	if len(field) > 1 && field[0] == '0' {
		return 0, fmt.Errorf("component %q has a leading zero", field)
	}
	value, err := strconv.Atoi(field)
	if err != nil {
		return 0, fmt.Errorf("component %q is not a number", field)
	}
	return value, nil
}

func validPrerelease(pre string) error {
	if pre == "" {
		return fmt.Errorf("the prerelease is empty")
	}
	for _, r := range pre {
		alphanumeric := (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
		if !alphanumeric && r != '.' && r != '-' {
			return fmt.Errorf("prerelease %q holds %q", pre, r)
		}
	}
	return nil
}

func ParseKind(text string) (Kind, Version, error) {
	switch strings.TrimSpace(text) {
	case "patch":
		return Patch, Version{}, nil
	case "minor":
		return Minor, Version{}, nil
	case "major":
		return Major, Version{}, nil
	}
	exact, err := Parse(strings.TrimPrefix(strings.TrimSpace(text), "v"))
	if err != nil {
		return 0, Version{}, fmt.Errorf("expected patch, minor, major, or X.Y.Z: %w", err)
	}
	return Exact, exact, nil
}

// Bump patches a prerelease into the release it was leading up to, rather than
// stepping past it.
func (v Version) Bump(kind Kind) Version {
	switch kind {
	case Major:
		return Version{Major: v.Major + 1}
	case Minor:
		return Version{Major: v.Major, Minor: v.Minor + 1}
	default:
		if v.Pre != "" {
			return Version{Major: v.Major, Minor: v.Minor, Patch: v.Patch}
		}
		return Version{Major: v.Major, Minor: v.Minor, Patch: v.Patch + 1}
	}
}

// Snapshot bumps the patch before it appends the prerelease, so an unreleased
// build sorts after the release it was cut from rather than before it.
func (v Version) Snapshot(day, commit string) Version {
	next := v.Bump(Patch)
	next.Pre = "nightly." + day + "." + commit
	return next
}

func (v Version) String() string {
	core := strconv.Itoa(v.Major) + "." + strconv.Itoa(v.Minor) + "." + strconv.Itoa(v.Patch)
	if v.Pre == "" {
		return core
	}
	return core + "-" + v.Pre
}

func (v Version) Tag() string { return "v" + v.String() }

func (v Version) ModuleTag() string { return ModulePrefix + "/" + v.Tag() }

func (v Version) Branch() string { return "release/" + v.Tag() }
