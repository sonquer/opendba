package envfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	values, err := Parse(strings.NewReader(`
# a comment

XDG_CONFIG_HOME=.local/config
export XDG_STATE_HOME = .local/state
QUOTED="with spaces"
SINGLE='also quoted'
TRAILING=value # explained
EMPTY=
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := Values{
		"XDG_CONFIG_HOME": ".local/config",
		"XDG_STATE_HOME":  ".local/state",
		"QUOTED":          "with spaces",
		"SINGLE":          "also quoted",
		"TRAILING":        "value",
		"EMPTY":           "",
	}
	if len(values) != len(want) {
		t.Fatalf("values = %+v", values)
	}
	for name, expected := range want {
		if values[name] != expected {
			t.Errorf("%s = %q, want %q", name, values[name], expected)
		}
	}
}

func TestParseRejectsBrokenLines(t *testing.T) {
	cases := []string{"NAME", "=value", "   =x"}
	for _, line := range cases {
		if _, err := Parse(strings.NewReader(line)); err == nil {
			t.Errorf("Parse(%q) must fail", line)
		} else if !strings.Contains(err.Error(), FileName) {
			t.Errorf("the error must name the file: %v", err)
		}
	}
}

func TestLoadResolvesPathsAgainstTheRoot(t *testing.T) {
	root := t.TempDir()
	content := "XDG_CONFIG_HOME=.local/config\nXDG_STATE_HOME=/absolute/state\nOTHER=relative/stays\n"
	if err := os.WriteFile(filepath.Join(root, FileName), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	values, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if values["XDG_CONFIG_HOME"] != filepath.Join(root, ".local/config") {
		t.Errorf("relative paths must be resolved: %q", values["XDG_CONFIG_HOME"])
	}
	if values["XDG_STATE_HOME"] != "/absolute/state" {
		t.Errorf("absolute paths must be left alone: %q", values["XDG_STATE_HOME"])
	}
	if values["OTHER"] != "relative/stays" {
		t.Errorf("only path variables are resolved: %q", values["OTHER"])
	}
}

func TestLoadWithoutAFile(t *testing.T) {
	values, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(values) != 0 {
		t.Errorf("values = %+v", values)
	}
}

func TestLoadFailures(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, FileName), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(root); err == nil {
		t.Error("a directory cannot be an env file")
	}

	broken := t.TempDir()
	if err := os.WriteFile(filepath.Join(broken, FileName), []byte("NAME\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(broken); err == nil {
		t.Error("a broken env file must be reported")
	}
}

func TestEnvironmentAppendsToTheBase(t *testing.T) {
	values := Values{"B": "2", "A": "1"}
	environment := values.Environment([]string{"PATH=/bin"})
	if len(environment) != 3 || environment[0] != "PATH=/bin" {
		t.Fatalf("environment = %v", environment)
	}
	if environment[1] != "A=1" || environment[2] != "B=2" {
		t.Errorf("values must be added in a stable order: %v", environment)
	}
}

func TestDescribe(t *testing.T) {
	if got := (Values{"B": "2", "A": "1"}).Describe(); got != "A=1 B=2" {
		t.Errorf("Describe() = %q", got)
	}
	if got := (Values{}).Describe(); got != "" {
		t.Errorf("Describe() = %q", got)
	}
}
