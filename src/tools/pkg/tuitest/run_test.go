package tuitest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sonquer/opendba/src/tools/pkg/tuitest/shot"
)

func suiteFor(t *testing.T, scenario string) (Suite, string) {
	t.Helper()
	repo := t.TempDir()
	root := filepath.Join(repo, "tests", "e2e")
	for _, dir := range []string{filepath.Join(root, scenariosDir), filepath.Join(root, seedsDir)} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("lay out = %v", err)
		}
	}
	write := func(path, body string) {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("write %s = %v", path, err)
		}
	}
	write(filepath.Join(root, suiteFile),
		"sizes = [\"40x10\"]\ngoldens = \"screens\"\ntimeout = \"10s\"\nquiet = \"40ms\"\n")
	write(filepath.Join(root, seedsDir, "core.sql"), "CREATE TABLE t (id integer primary key);")
	write(filepath.Join(root, scenariosDir, "one.toml"), scenario)
	suite, err := Load(repo, root)
	if err != nil {
		t.Fatalf("Load = %v", err)
	}
	return suite, repo
}

func runnerFor(t *testing.T, suite Suite, update bool) Runner {
	t.Helper()
	return Runner{
		Suite:    suite,
		Binary:   os.Args[0],
		Update:   update,
		Artifact: filepath.Join(t.TempDir(), "artifacts"),
		Env:      []string{fakeVariable + "=1"},
	}
}

var small = Size{Width: 40, Height: 10}

func TestRunWalksAScenarioAndKeepsTheScreen(t *testing.T) {
	suite, _ := suiteFor(t, `seed = "core"

[[step]]
wait = "READY"

[[step]]
key = "a"

[[step]]
expect = ["ALPHA"]

[[step]]
shot = "one"
`)
	runner := runnerFor(t, suite, true)
	result, err := runner.Run(suite.Scenarios[0], small)
	if err != nil {
		t.Fatalf("Run = %v", err)
	}
	if !result.OK() {
		t.Fatalf("Run failed: %v", result.Failures)
	}
	if len(result.Updated) != 1 {
		t.Fatalf("Run kept %d screens", len(result.Updated))
	}
	kept, err := os.ReadFile(result.Updated[0])
	if err != nil {
		t.Fatalf("read back = %v", err)
	}
	if !strings.Contains(string(kept), "ALPHA") {
		t.Errorf("the kept screen is %q", kept)
	}
}

func TestRunComparesAgainstTheScreenThatWasKept(t *testing.T) {
	suite, _ := suiteFor(t, `seed = "core"

[[step]]
wait = "READY"

[[step]]
shot = "one"
`)
	runner := runnerFor(t, suite, true)
	if _, err := runner.Run(suite.Scenarios[0], small); err != nil {
		t.Fatalf("Run = %v", err)
	}
	if err := Write(suite.GoldenPath(small, "one"), "something else"); err != nil {
		t.Fatalf("Write = %v", err)
	}
	runner.Update = false
	result, err := runner.Run(suite.Scenarios[0], small)
	if err != nil {
		t.Fatalf("Run = %v", err)
	}
	if result.OK() {
		t.Fatal("a screen that changed was accepted")
	}
	if result.Failures[0].Action != "shot" {
		t.Errorf("failure = %#v", result.Failures[0])
	}
	for _, name := range []string{"frame.txt", "frame.ans", "frame.svg", "session.cast", "steps.log", "diff.txt"} {
		if _, err := os.Stat(filepath.Join(result.Artifacts, name)); err != nil {
			t.Errorf("%s was not written: %v", name, err)
		}
	}
}

func TestRunReportsTheStepThatDidNotHappen(t *testing.T) {
	cases := map[string]struct {
		steps  string
		action string
		says   string
	}{
		"a wait that never came true": {
			"[[step]]\nwait = \"NEVER\"\n", "wait", "never appeared"},
		"a wait that never went away": {
			"[[step]]\nwait = \"READY\"\n\n[[step]]\nwait_gone = \"READY\"\n", "wait_gone", "never went away"},
		"an expectation that was not met": {
			"[[step]]\nwait = \"READY\"\n\n[[step]]\nexpect = [\"READY\", \"NOPE\"]\n", "expect", `never showed "NOPE"`},
		"something that should not be there": {
			"[[step]]\nwait = \"READY\"\n\n[[step]]\nexpect_absent = [\"READY\"]\n", "expect_absent", `showed "READY"`},
		"a pattern that matched nothing": {
			"[[step]]\nwait = \"READY\"\n\n[[step]]\nmatch = \"^NOPE$\"\n", "match", "matched"},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			suite, _ := suiteFor(t, "seed = \"core\"\n\n"+test.steps)
			result, err := runnerFor(t, suite, false).Run(suite.Scenarios[0], small)
			if err != nil {
				t.Fatalf("Run = %v", err)
			}
			if result.OK() {
				t.Fatal("the scenario passed")
			}
			if result.Failures[0].Action != test.action {
				t.Errorf("action = %q, want %q", result.Failures[0].Action, test.action)
			}
			if !strings.Contains(result.Failures[0].Reason, test.says) {
				t.Errorf("reason = %q, want it to mention %q", result.Failures[0].Reason, test.says)
			}
		})
	}
}

func TestRunTakesEveryStepAScenarioCanName(t *testing.T) {
	suite, _ := suiteFor(t, `seed = "core"

[[step]]
wait = "READY"

[[step]]
keys = ["a"]

[[step]]
wait = "ALPHA"

[[step]]
type = "b"

[[step]]
wait = "BETA"

[[step]]
match = "BET"

[[step]]
resize = "60x20"

[[step]]
key = "z"

[[step]]
wait_gone = "BETA"

[[step]]
expect_absent = ["ALPHA"]
`)
	result, err := runnerFor(t, suite, false).Run(suite.Scenarios[0], small)
	if err != nil {
		t.Fatalf("Run = %v", err)
	}
	if !result.OK() {
		t.Errorf("Run failed: %v", result.Failures)
	}
}

func TestRunHoldsTheProgramToTheCodeItShouldLeaveWith(t *testing.T) {
	suite, _ := suiteFor(t, `seed = "core"
exit = 0

[[step]]
wait = "READY"

[[step]]
key = "x"
`)
	result, err := runnerFor(t, suite, false).Run(suite.Scenarios[0], small)
	if err != nil {
		t.Fatalf("Run = %v", err)
	}
	if result.OK() {
		t.Fatal("a program that left with the wrong code was accepted")
	}
	if !strings.Contains(result.Failures[0].Reason, "left with 3, not 0") {
		t.Errorf("reason = %q", result.Failures[0].Reason)
	}
}

func TestRunReportsWhatTheProgramMustNeverDraw(t *testing.T) {
	suite, _ := suiteFor(t, "seed = \"core\"\n\n[[step]]\nwait = \"READY\"\n")
	suite.Forbid = []string{"READY"}
	result, err := runnerFor(t, suite, false).Run(suite.Scenarios[0], small)
	if err != nil {
		t.Fatalf("Run = %v", err)
	}
	if result.OK() {
		t.Fatal("a screen that leaked was accepted")
	}
	if result.Failures[0].Action != "forbid" {
		t.Errorf("failure = %#v", result.Failures[0])
	}
}

func TestRunReportsAnAccountOfAFailureTheProgramLeftBehind(t *testing.T) {
	suite, _ := suiteFor(t, "seed = \"core\"\n\n[[step]]\nwait = \"READY\"\n")
	runner := runnerFor(t, suite, false)
	runner.Env = append(runner.Env, crashVariable+"=state/opendba")
	result, err := runner.Run(suite.Scenarios[0], small)
	if err != nil {
		t.Fatalf("Run = %v", err)
	}
	if result.OK() {
		t.Fatal("a run that left a crash report behind was accepted")
	}
	last := result.Failures[len(result.Failures)-1]
	if last.Action != "crash" || !strings.Contains(last.Detail, "it fell over") {
		t.Errorf("failure = %#v", last)
	}
}

func TestRunStartsAScenarioAboutTheFirstRunWithNothingConfigured(t *testing.T) {
	suite, _ := suiteFor(t, "seed = \"core\"\nsetup = true\n\n[[step]]\nwait = \"READY\"\n")
	result, err := runnerFor(t, suite, false).Run(suite.Scenarios[0], small)
	if err != nil {
		t.Fatalf("Run = %v", err)
	}
	if !result.OK() {
		t.Errorf("Run failed: %v", result.Failures)
	}
}

func TestRunReportsWhatItCouldNotSetUp(t *testing.T) {
	suite, _ := suiteFor(t, "seed = \"missing\"\n\n[[step]]\nwait = \"READY\"\n")
	if _, err := runnerFor(t, suite, false).Run(suite.Scenarios[0], small); err == nil {
		t.Error("a scenario whose seed is not there was run")
	}
	suite, _ = suiteFor(t, "seed = \"core\"\n\n[[step]]\nwait = \"READY\"\n")
	runner := runnerFor(t, suite, false)
	runner.Workdir = filepath.Join(t.TempDir(), "never made")
	if _, err := runner.Run(suite.Scenarios[0], small); err == nil {
		t.Error("a scenario with nowhere to run was run")
	}
}

func TestAFailureNamesTheStepAndWhatWentWrong(t *testing.T) {
	plain := Failure{Step: 2, Action: "wait", Reason: "it never came"}
	if got := plain.String(); got != "step 2 (wait): it never came" {
		t.Errorf("String() = %q", got)
	}
	detailed := Failure{Step: 3, Action: "shot", Reason: "it changed", Detail: "  1 - a"}
	if !strings.Contains(detailed.String(), "\n  1 - a") {
		t.Errorf("String() = %q", detailed.String())
	}
}

func TestASecondConnectionIsSeededWhenThereIsOne(t *testing.T) {
	suite, _ := suiteFor(t, "seed = \"core\"\n\n[[step]]\nwait = \"READY\"\n")
	second := filepath.Join(suite.Root, seedsDir, "second.sql")
	if err := os.WriteFile(second, []byte("CREATE TABLE s (id integer);"), 0o600); err != nil {
		t.Fatalf("write = %v", err)
	}
	result, err := runnerFor(t, suite, false).Run(suite.Scenarios[0], small)
	if err != nil {
		t.Fatalf("Run = %v", err)
	}
	if !result.OK() {
		t.Errorf("Run failed: %v", result.Failures)
	}
}

func TestKeepWritesEverythingSomebodyLookingAtABrokenScreenNeeds(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "one", "40x10")
	result := Result{
		Scenario: "one",
		Size:     small,
		Frame:    Frame{Styled: "\x1b[31mREADY\x1b[m"},
		Chunks:   []Chunk{{At: time.Second, Data: []byte("READY")}},
		Failures: []Failure{{Step: 1, Action: "shot", Reason: "it changed", Detail: "1 - a"}},
	}
	grid := [][]shot.Cell{{{Content: "R"}}}
	if err := result.Keep(dir, grid, time.Unix(0, 0)); err != nil {
		t.Fatalf("Keep = %v", err)
	}
	frame, err := os.ReadFile(filepath.Join(dir, "frame.txt"))
	if err != nil || !strings.Contains(string(frame), "READY") {
		t.Errorf("frame.txt = %q, %v", frame, err)
	}
	diff, err := os.ReadFile(filepath.Join(dir, "diff.txt"))
	if err != nil || !strings.Contains(string(diff), "1 - a") {
		t.Errorf("diff.txt = %q, %v", diff, err)
	}
}

func TestKeepReportsWhereItCannotWrite(t *testing.T) {
	blocked := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocked, nil, 0o600); err != nil {
		t.Fatalf("set up = %v", err)
	}
	result := Result{Scenario: "one", Size: small}
	if err := result.Keep(filepath.Join(blocked, "under"), nil, time.Unix(0, 0)); err == nil {
		t.Error("artifacts were written under a file")
	}
}
