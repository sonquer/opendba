package tuitest

import (
	"runtime"
	"testing"
)

func TestWalkRunsEveryScenarioAtEverySize(t *testing.T) {
	suite, _ := suiteFor(t, "seed = \"core\"\n\n[[step]]\nwait = \"READY\"\n")
	suite.Sizes = []string{"40x10", "30x8"}
	results := Walk(runnerFor(t, suite, false), Filter{}, 2)
	if len(results) != 2 {
		t.Fatalf("Walk ran %d times", len(results))
	}
	for _, result := range results {
		if !result.OK() {
			t.Errorf("%s at %s failed: %v", result.Scenario, result.Size, result.Failures)
		}
	}
	if results[0].Size.String() != "30x8" || results[1].Size.String() != "40x10" {
		t.Errorf("the results are not in order: %s, %s", results[0].Size, results[1].Size)
	}
}

func TestWalkNarrowsDownToWhatWasAskedFor(t *testing.T) {
	suite, _ := suiteFor(t, "seed = \"core\"\n\n[[step]]\nwait = \"READY\"\n")
	suite.Sizes = []string{"40x10", "30x8"}
	cases := map[string]struct {
		filter Filter
		want   int
	}{
		"one size":               {Filter{Size: "40x10"}, 1},
		"one scenario":           {Filter{Scenario: "one"}, 2},
		"one scenario, one size": {Filter{Scenario: "one", Size: "30x8"}, 1},
		"a scenario there is no": {Filter{Scenario: "other"}, 0},
		"a size there is no":     {Filter{Size: "99x99"}, 0},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			results := Walk(runnerFor(t, suite, false), test.filter, 1)
			if len(results) != test.want {
				t.Errorf("Walk ran %d times, want %d", len(results), test.want)
			}
		})
	}
}

func TestWalkReportsAScenarioThatCouldNotBeSetUpAtAll(t *testing.T) {
	suite, _ := suiteFor(t, "seed = \"missing\"\n\n[[step]]\nwait = \"READY\"\n")
	results := Walk(runnerFor(t, suite, false), Filter{}, 1)
	if len(results) != 1 {
		t.Fatalf("Walk ran %d times", len(results))
	}
	if results[0].OK() || results[0].Failures[0].Action != "run" {
		t.Errorf("result = %#v", results[0])
	}
	if results[0].Scenario != "one" {
		t.Errorf("the failure is not named: %q", results[0].Scenario)
	}
}

func TestJobsLeavesACoreForEverythingElse(t *testing.T) {
	if got := Jobs(3); got != 3 {
		t.Errorf("Jobs(3) = %d", got)
	}
	got := Jobs(0)
	if got < 1 || got > 8 {
		t.Errorf("Jobs(0) = %d, on a machine with %d cores", got, runtime.NumCPU())
	}
}
