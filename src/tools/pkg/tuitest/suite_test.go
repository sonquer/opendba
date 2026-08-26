package tuitest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func lay(t *testing.T, suite string, scenarios map[string]string) (string, string) {
	t.Helper()
	repo := t.TempDir()
	root := filepath.Join(repo, "tests", "e2e")
	if err := os.MkdirAll(filepath.Join(root, scenariosDir), 0o755); err != nil {
		t.Fatalf("lay out = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, suiteFile), []byte(suite), 0o600); err != nil {
		t.Fatalf("write the suite = %v", err)
	}
	for name, body := range scenarios {
		path := filepath.Join(root, scenariosDir, name+".toml")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("write %s = %v", name, err)
		}
	}
	return repo, root
}

const oneSize = "sizes = [\"80x24\"]\ngoldens = \"screens\"\n"

func TestLoadReadsTheSuiteAndTheScenariosBesideIt(t *testing.T) {
	repo, root := lay(t, oneSize+"timeout = \"5s\"\nquiet = \"20ms\"\n", map[string]string{
		"second": "seed = \"core\"\n\n[[step]]\nwait = \"ready\"\n",
		"first":  "name = \"first\"\nseed = \"core\"\n\n[[step]]\nkey = \"e\"\n",
	})
	suite, err := Load(repo, root)
	if err != nil {
		t.Fatalf("Load = %v", err)
	}
	if len(suite.Scenarios) != 2 {
		t.Fatalf("Load found %d scenarios", len(suite.Scenarios))
	}
	if suite.Scenarios[0].Name != "first" || suite.Scenarios[1].Name != "second" {
		t.Errorf("the scenarios are not in order: %s, %s",
			suite.Scenarios[0].Name, suite.Scenarios[1].Name)
	}
	if suite.Timeout.Every() != 5*time.Second || suite.Quiet.Every() != 20*time.Millisecond {
		t.Errorf("timeout = %v, quiet = %v", suite.Timeout.Every(), suite.Quiet.Every())
	}
	if suite.Goldens != filepath.Join(repo, "screens") {
		t.Errorf("goldens = %q", suite.Goldens)
	}
}

func TestLoadFallsBackToWhatEverySuiteWants(t *testing.T) {
	repo, root := lay(t, oneSize, map[string]string{
		"one": "seed = \"core\"\n\n[[step]]\nkey = \"e\"\n",
	})
	suite, err := Load(repo, root)
	if err != nil {
		t.Fatalf("Load = %v", err)
	}
	if suite.Bar != defaultBar || suite.Timeout.Every() != defaultTimout {
		t.Errorf("bar = %q, timeout = %v", suite.Bar, suite.Timeout.Every())
	}
	if suite.Quiet.Every() != defaultQuiet {
		t.Errorf("quiet = %v", suite.Quiet.Every())
	}
}

func TestLoadRefusesASuiteThatCannotBeRun(t *testing.T) {
	good := "seed = \"core\"\n\n[[step]]\nkey = \"e\"\n"
	cases := map[string]struct {
		suite     string
		scenarios map[string]string
		says      string
	}{
		"no sizes":            {"goldens = \"s\"\n", map[string]string{"one": good}, "no sizes"},
		"a size that is not":  {"sizes = [\"wide\"]\n", map[string]string{"one": good}, "120x36"},
		"a mask that is not":  {oneSize + "[[mask]]\npattern = \"([\"\nwith = \"x\"\n", map[string]string{"one": good}, "mask"},
		"no scenarios at all": {oneSize, map[string]string{}, "there are none"},
		"a scenario with no steps": {oneSize, map[string]string{
			"one": "seed = \"core\"\n"}, "no steps"},
		"a scenario with no seed": {oneSize, map[string]string{
			"one": "[[step]]\nkey = \"e\"\n"}, "no seed"},
		"a step that does nothing": {oneSize, map[string]string{
			"one": "seed = \"core\"\n\n[[step]]\n"}, "does nothing"},
		"a step that does two things": {oneSize, map[string]string{
			"one": "seed = \"core\"\n\n[[step]]\nkey = \"e\"\nwait = \"x\"\n"}, "does key and wait"},
		"a key that does not exist": {oneSize, map[string]string{
			"one": "seed = \"core\"\n\n[[step]]\nkey = \"hyper+z\"\n"}, "is not a modifier"},
		"a key in a burst that does not exist": {oneSize, map[string]string{
			"one": "seed = \"core\"\n\n[[step]]\nkeys = [\"down\", \"hyper+z\"]\n"}, "is not a modifier"},
		"a pattern that will not compile": {oneSize, map[string]string{
			"one": "seed = \"core\"\n\n[[step]]\nmatch = \"([\"\n"}, "read the pattern"},
		"a size a step cannot resize to": {oneSize, map[string]string{
			"one": "seed = \"core\"\n\n[[step]]\nresize = \"huge\"\n"}, "120x36"},
		"two screens with one name": {oneSize, map[string]string{
			"one": "seed = \"core\"\n\n[[step]]\nshot = \"a\"\n\n[[step]]\nshot = \"a\"\n"}, "two steps"},
		"two scenarios with one name": {oneSize, map[string]string{
			"one": "name = \"same\"\nseed = \"core\"\n\n[[step]]\nkey = \"e\"\n",
			"two": "name = \"same\"\nseed = \"core\"\n\n[[step]]\nkey = \"e\"\n"}, "both called"},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			repo, root := lay(t, test.suite, test.scenarios)
			_, err := Load(repo, root)
			if err == nil {
				t.Fatal("the suite was accepted")
			}
			if !strings.Contains(err.Error(), test.says) {
				t.Errorf("Load = %v, want it to mention %q", err, test.says)
			}
		})
	}
}

func TestLoadFailsWhenThereIsNothingToRead(t *testing.T) {
	repo := t.TempDir()
	if _, err := Load(repo, filepath.Join(repo, "missing")); err == nil {
		t.Error("a suite that is not there was read")
	}
	root := filepath.Join(repo, "tests")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("lay out = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, suiteFile), []byte(oneSize), 0o600); err != nil {
		t.Fatalf("write = %v", err)
	}
	if _, err := Load(repo, root); err == nil {
		t.Error("a suite with no scenarios directory was read")
	}
}

func TestLoadReportsAScenarioThatIsNotTOML(t *testing.T) {
	repo, root := lay(t, oneSize, map[string]string{"one": "seed = ["})
	if _, err := Load(repo, root); err == nil {
		t.Error("a scenario that is not TOML was read")
	}
}

func TestLoadIgnoresWhatIsNotAScenario(t *testing.T) {
	repo, root := lay(t, oneSize, map[string]string{
		"one": "seed = \"core\"\n\n[[step]]\nkey = \"e\"\n",
	})
	if err := os.WriteFile(filepath.Join(root, scenariosDir, "notes.md"), []byte("x"), 0o600); err != nil {
		t.Fatalf("write = %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, scenariosDir, "old"), 0o755); err != nil {
		t.Fatalf("mkdir = %v", err)
	}
	suite, err := Load(repo, root)
	if err != nil {
		t.Fatalf("Load = %v", err)
	}
	if len(suite.Scenarios) != 1 {
		t.Errorf("Load found %d scenarios", len(suite.Scenarios))
	}
}

func TestParseSizeReadsWhatAScenarioNames(t *testing.T) {
	size, err := ParseSize("120x36")
	if err != nil {
		t.Fatalf("ParseSize = %v", err)
	}
	if size.Width != 120 || size.Height != 36 || size.String() != "120x36" {
		t.Errorf("ParseSize = %#v", size)
	}
	for _, value := range []string{"120", "widex36", "120xtall", "0x36", "120x0"} {
		if _, err := ParseSize(value); err == nil {
			t.Errorf("ParseSize(%q) was accepted", value)
		}
	}
}

func TestADurationIsReadTheWayItIsWritten(t *testing.T) {
	var every Duration
	if err := every.UnmarshalText([]byte("250ms")); err != nil {
		t.Fatalf("UnmarshalText = %v", err)
	}
	if every.Every() != 250*time.Millisecond {
		t.Errorf("Every() = %v", every.Every())
	}
	if err := every.UnmarshalText([]byte("soon")); err == nil {
		t.Error("a duration that is not one was read")
	}
}

func TestSizesForFallsBackToTheSuite(t *testing.T) {
	suite := Suite{Sizes: []string{"80x24", "120x36"}}
	if got := suite.SizesFor(Scenario{}); len(got) != 2 {
		t.Errorf("SizesFor = %#v", got)
	}
	own := Scenario{Sizes: []string{"60x14", "nonsense"}}
	got := suite.SizesFor(own)
	if len(got) != 1 || got[0].String() != "60x14" {
		t.Errorf("SizesFor = %#v", got)
	}
}

func TestSeedFileSitsBesideTheScenarios(t *testing.T) {
	suite := Suite{Root: filepath.Join("tests", "e2e")}
	want := filepath.Join("tests", "e2e", seedsDir, "core.sql")
	if got := suite.SeedFile(Scenario{Seed: "core"}); got != want {
		t.Errorf("SeedFile = %q", got)
	}
}

func TestGoldenPathIsNamedByTheSizeAndTheScreen(t *testing.T) {
	suite := Suite{Goldens: filepath.Join("repo", "screens")}
	want := filepath.Join("repo", "screens", "80x24", "editor.txt")
	if got := suite.GoldenPath(Size{Width: 80, Height: 24}, "editor"); got != want {
		t.Errorf("GoldenPath = %q", got)
	}
}

func TestAnAbsoluteGoldensDirectoryIsLeftAlone(t *testing.T) {
	absolute := t.TempDir()
	repo, root := lay(t, "sizes = [\"80x24\"]\ngoldens = "+quoted(absolute)+"\n", map[string]string{
		"one": "seed = \"core\"\n\n[[step]]\nkey = \"e\"\n",
	})
	suite, err := Load(repo, root)
	if err != nil {
		t.Fatalf("Load = %v", err)
	}
	if suite.Goldens != absolute {
		t.Errorf("goldens = %q, want %q", suite.Goldens, absolute)
	}
}

func quoted(value string) string { return "\"" + strings.ReplaceAll(value, `\`, `\\`) + "\"" }

func TestActionNamesTheOneThingAStepDoes(t *testing.T) {
	cases := map[string]struct {
		step Step
		want string
		only bool
	}{
		"a key":           {Step{Key: "e"}, "key", true},
		"a burst of keys": {Step{Keys: []string{"e"}}, "keys", true},
		"typing":          {Step{Type: "x"}, "type", true},
		"waiting":         {Step{Wait: "x"}, "wait", true},
		"waiting to go":   {Step{WaitGone: "x"}, "wait_gone", true},
		"expecting":       {Step{Expect: []string{"x"}}, "expect", true},
		"expecting none":  {Step{ExpectAbsent: []string{"x"}}, "expect_absent", true},
		"matching":        {Step{Match: "x"}, "match", true},
		"a screen":        {Step{Shot: "x"}, "shot", true},
		"a resize":        {Step{Resize: "80x24"}, "resize", true},
		"nothing":         {Step{}, "", false},
		"two things":      {Step{Key: "e", Shot: "x"}, "key and shot", false},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			got, only := test.step.Action()
			if got != test.want || only != test.only {
				t.Errorf("Action() = %q, %v", got, only)
			}
		})
	}
}
