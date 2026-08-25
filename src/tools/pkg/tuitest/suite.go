package tuitest

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// Duration is a length of time written the way a person writes it.
type Duration time.Duration

// UnmarshalText reads a duration such as "20s".
func (d *Duration) UnmarshalText(text []byte) error {
	parsed, err := time.ParseDuration(string(text))
	if err != nil {
		return fmt.Errorf("read a duration: %w", err)
	}
	*d = Duration(parsed)
	return nil
}

// Every is the duration as the standard library holds one.
func (d Duration) Every() time.Duration { return time.Duration(d) }

// Size is a terminal size.
type Size struct {
	Width  int
	Height int
}

// String renders the size the way a scenario names it.
func (s Size) String() string { return strconv.Itoa(s.Width) + "x" + strconv.Itoa(s.Height) }

// ParseSize reads a size such as "120x36".
func ParseSize(value string) (Size, error) {
	width, height, found := strings.Cut(value, "x")
	if !found {
		return Size{}, fmt.Errorf("a size looks like 120x36, not %q", value)
	}
	columns, err := strconv.Atoi(width)
	if err != nil || columns <= 0 {
		return Size{}, fmt.Errorf("read the width of %q", value)
	}
	rows, err := strconv.Atoi(height)
	if err != nil || rows <= 0 {
		return Size{}, fmt.Errorf("read the height of %q", value)
	}
	return Size{Width: columns, Height: rows}, nil
}

// Mask replaces what a screen shows but cannot show the same way twice.
type Mask struct {
	Pattern string `toml:"pattern"`
	With    string `toml:"with"`

	compiled *regexp.Regexp
}

// Suite is the whole set of scenarios and the settings they share.
type Suite struct {
	Sizes   []string `toml:"sizes"`
	Goldens string   `toml:"goldens"`
	Bar     string   `toml:"bar"`
	Timeout Duration `toml:"timeout"`
	Quiet   Duration `toml:"quiet"`
	Forbid  []string `toml:"forbid"`
	Masks   []Mask   `toml:"mask"`

	Root      string     `toml:"-"`
	Scenarios []Scenario `toml:"-"`
}

// Scenario is one path through the interface.
type Scenario struct {
	Name       string   `toml:"name"`
	Seed       string   `toml:"seed"`
	Connection string   `toml:"connection"`
	Sizes      []string `toml:"sizes"`
	Steps      []Step   `toml:"step"`
	Exit       *int     `toml:"exit"`
	Setup      bool     `toml:"setup"`

	Path string `toml:"-"`
}

// Step is one thing a scenario does, and there is exactly one of them per step.
type Step struct {
	Key          string   `toml:"key"`
	Keys         []string `toml:"keys"`
	Type         string   `toml:"type"`
	Wait         string   `toml:"wait"`
	WaitGone     string   `toml:"wait_gone"`
	Expect       []string `toml:"expect"`
	ExpectAbsent []string `toml:"expect_absent"`
	Match        string   `toml:"match"`
	Shot         string   `toml:"shot"`
	Resize       string   `toml:"resize"`
}

// Action names what a step does, and reports whether the step names exactly one
// thing to do.
func (s Step) Action() (string, bool) {
	named := []string{}
	for name, set := range map[string]bool{
		"key":           s.Key != "",
		"keys":          len(s.Keys) > 0,
		"type":          s.Type != "",
		"wait":          s.Wait != "",
		"wait_gone":     s.WaitGone != "",
		"expect":        len(s.Expect) > 0,
		"expect_absent": len(s.ExpectAbsent) > 0,
		"match":         s.Match != "",
		"shot":          s.Shot != "",
		"resize":        s.Resize != "",
	} {
		if set {
			named = append(named, name)
		}
	}
	sort.Strings(named)
	if len(named) != 1 {
		return strings.Join(named, " and "), false
	}
	return named[0], true
}

const (
	suiteFile     = "suite.toml"
	scenariosDir  = "scenarios"
	seedsDir      = "seed"
	defaultBar    = "pipes"
	defaultTimout = 20 * time.Second
)

// Load reads a suite and every scenario beside it. Paths inside it are resolved
// against repo, so that a suite reads the same from any working directory.
func Load(repo, root string) (Suite, error) {
	suite := Suite{Root: root}
	if _, err := toml.DecodeFile(filepath.Join(root, suiteFile), &suite); err != nil {
		return Suite{}, fmt.Errorf("read %s: %w", suiteFile, err)
	}
	suite.Root = root
	if suite.Timeout == 0 {
		suite.Timeout = Duration(defaultTimout)
	}
	if suite.Quiet == 0 {
		suite.Quiet = Duration(defaultQuiet)
	}
	if suite.Bar == "" {
		suite.Bar = defaultBar
	}
	if !filepath.IsAbs(suite.Goldens) {
		suite.Goldens = filepath.Join(repo, suite.Goldens)
	}
	if len(suite.Sizes) == 0 {
		return Suite{}, fmt.Errorf("read %s: no sizes are listed", suiteFile)
	}
	for i := range suite.Masks {
		compiled, err := regexp.Compile(suite.Masks[i].Pattern)
		if err != nil {
			return Suite{}, fmt.Errorf("read a mask in %s: %w", suiteFile, err)
		}
		suite.Masks[i].compiled = compiled
	}
	scenarios, err := loadScenarios(filepath.Join(root, scenariosDir))
	if err != nil {
		return Suite{}, err
	}
	suite.Scenarios = scenarios
	if err := suite.validate(); err != nil {
		return Suite{}, err
	}
	return suite, nil
}

func loadScenarios(dir string) ([]Scenario, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read the scenarios: %w", err)
	}
	var scenarios []Scenario
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".toml" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		var scenario Scenario
		if _, err := toml.DecodeFile(path, &scenario); err != nil {
			return nil, fmt.Errorf("read %s: %w", entry.Name(), err)
		}
		scenario.Path = path
		if scenario.Name == "" {
			scenario.Name = strings.TrimSuffix(entry.Name(), ".toml")
		}
		scenarios = append(scenarios, scenario)
	}
	sort.Slice(scenarios, func(i, j int) bool { return scenarios[i].Name < scenarios[j].Name })
	return scenarios, nil
}

func (s Suite) validate() error {
	if len(s.Scenarios) == 0 {
		return fmt.Errorf("read the scenarios: there are none in %s", filepath.Join(s.Root, scenariosDir))
	}
	seen := map[string]string{}
	for _, scenario := range s.Scenarios {
		if previous, taken := seen[scenario.Name]; taken {
			return fmt.Errorf("%s and %s are both called %q", previous, scenario.Path, scenario.Name)
		}
		seen[scenario.Name] = scenario.Path
		if err := scenario.validate(); err != nil {
			return err
		}
	}
	for _, size := range s.Sizes {
		if _, err := ParseSize(size); err != nil {
			return fmt.Errorf("read %s: %w", suiteFile, err)
		}
	}
	return nil
}

func (s Scenario) validate() error {
	if len(s.Steps) == 0 {
		return fmt.Errorf("%s: a scenario with no steps proves nothing", s.Path)
	}
	if s.Seed == "" {
		return fmt.Errorf("%s: no seed is named", s.Path)
	}
	shots := map[string]bool{}
	for i, step := range s.Steps {
		action, only := step.Action()
		if !only {
			if action == "" {
				return fmt.Errorf("%s: step %d does nothing", s.Path, i+1)
			}
			return fmt.Errorf("%s: step %d does %s, and a step does one thing", s.Path, i+1, action)
		}
		if err := step.check(s.Path, i+1); err != nil {
			return err
		}
		if step.Shot != "" {
			if shots[step.Shot] {
				return fmt.Errorf("%s: two steps both take a screen called %q", s.Path, step.Shot)
			}
			shots[step.Shot] = true
		}
	}
	return nil
}

func (s Step) check(path string, number int) error {
	names := append([]string{s.Key}, s.Keys...)
	for _, name := range names {
		if name == "" {
			continue
		}
		if _, err := Encode(name); err != nil {
			return fmt.Errorf("%s: step %d: %w", path, number, err)
		}
	}
	if s.Match != "" {
		if _, err := regexp.Compile(s.Match); err != nil {
			return fmt.Errorf("%s: step %d: read the pattern: %w", path, number, err)
		}
	}
	if s.Resize != "" {
		if _, err := ParseSize(s.Resize); err != nil {
			return fmt.Errorf("%s: step %d: %w", path, number, err)
		}
	}
	return nil
}

// SizesFor is the list of sizes a scenario runs at.
func (s Suite) SizesFor(scenario Scenario) []Size {
	names := scenario.Sizes
	if len(names) == 0 {
		names = s.Sizes
	}
	sizes := make([]Size, 0, len(names))
	for _, name := range names {
		size, err := ParseSize(name)
		if err != nil {
			continue
		}
		sizes = append(sizes, size)
	}
	return sizes
}

// SeedFile is the path to the statements a scenario starts from.
func (s Suite) SeedFile(scenario Scenario) string {
	return filepath.Join(s.Root, seedsDir, scenario.Seed+".sql")
}
