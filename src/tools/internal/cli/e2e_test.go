package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func laySuite(t *testing.T, h *harness, scenario string) {
	t.Helper()
	root := filepath.Join(h.root, suiteDir)
	writeFile(t, filepath.Join(root, "suite.toml"),
		"sizes = [\"40x10\"]\ngoldens = \"screens\"\ntimeout = \"10s\"\nquiet = \"40ms\"\n")
	writeFile(t, filepath.Join(root, "seed", "core.sql"), "CREATE TABLE t (id integer primary key);")
	writeFile(t, filepath.Join(root, "scenarios", "one.toml"), scenario)
}

const ready = "seed = \"core\"\n\n[[step]]\nwait = \"READY\"\n"

func TestE2ERefusesASuiteThatCannotBeRead(t *testing.T) {
	h := newHarness(t, fakeRunner{})
	if code := h.app.Run(context.Background(), []string{"e2e"}); code != ExitFailure {
		t.Fatalf("exit = %d\n%s", code, h.err())
	}
	if !strings.Contains(h.err(), "suite.toml") {
		t.Errorf("stderr = %q", h.err())
	}
}

func TestE2EReportsABuildThatWouldNotCompile(t *testing.T) {
	h := newHarness(t, fakeRunner{fail: map[string]bool{"go": true}})
	laySuite(t, h, ready)
	if code := h.app.Run(context.Background(), []string{"e2e"}); code != ExitFailure {
		t.Fatalf("exit = %d\n%s", code, h.err())
	}
	if !strings.Contains(h.err(), "build the program") {
		t.Errorf("stderr = %q", h.err())
	}
}

func TestE2EReportsABuildThatCouldNotBeStarted(t *testing.T) {
	h := newHarness(t, fakeRunner{missing: map[string]bool{"go": true}})
	laySuite(t, h, ready)
	if code := h.app.Run(context.Background(), []string{"e2e"}); code != ExitFailure {
		t.Fatalf("exit = %d\n%s", code, h.err())
	}
}

func TestE2ERefusesAFlagItDoesNotHave(t *testing.T) {
	h := newHarness(t, fakeRunner{})
	if code := h.app.Run(context.Background(), []string{"e2e", "--nonsense"}); code != ExitUsage {
		t.Errorf("exit = %d", code)
	}
	if code := h.app.Run(context.Background(), []string{"e2e", "-h"}); code != ExitOK {
		t.Errorf("exit = %d", code)
	}
}

func TestE2EWalksTheSuiteAndSaysWhatItProved(t *testing.T) {
	h := newHarness(t, fakeRunner{})
	laySuite(t, h, ready+"\n[[step]]\nshot = \"one\"\n")
	summary := filepath.Join(t.TempDir(), "summary.md")
	code := h.app.Run(context.Background(), []string{
		"e2e", "--binary", os.Args[0], "--update", "--summary", summary, "--jobs", "1",
	})
	if code != ExitOK {
		t.Fatalf("exit = %d\n%s\n%s", code, h.out(), h.err())
	}
	out := h.out()
	for _, want := range []string{"ok    one 40x10", "1 screens kept", "1 of 1 screens"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	written, err := os.ReadFile(summary)
	if err != nil {
		t.Fatalf("read the summary = %v", err)
	}
	for _, want := range []string{"## screens", "| one | 40x10 | ok |"} {
		if !strings.Contains(string(written), want) {
			t.Errorf("the summary is missing %q:\n%s", want, written)
		}
	}
}

func TestE2EFailsWhenAScreenIsNotTheOneThatWasKept(t *testing.T) {
	h := newHarness(t, fakeRunner{})
	laySuite(t, h, ready+"\n[[step]]\nshot = \"one\"\n")
	writeFile(t, filepath.Join(h.root, "screens", "40x10", "one.txt"), "something else\n")
	summary := filepath.Join(t.TempDir(), "summary.md")
	code := h.app.Run(context.Background(), []string{
		"e2e", "--binary", os.Args[0], "--summary", summary, "--jobs", "1",
	})
	if code != ExitFailure {
		t.Fatalf("exit = %d\n%s", code, h.out())
	}
	out := h.out()
	for _, want := range []string{"FAIL  one 40x10", "what the failures looked like is in"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	written, err := os.ReadFile(summary)
	if err != nil {
		t.Fatalf("read the summary = %v", err)
	}
	for _, want := range []string{"**failed**", "### one at 40x10", "<details>"} {
		if !strings.Contains(string(written), want) {
			t.Errorf("the summary is missing %q:\n%s", want, written)
		}
	}
}

func TestE2EWritesWhatFailedWhereItWasAskedTo(t *testing.T) {
	h := newHarness(t, fakeRunner{})
	laySuite(t, h, "seed = \"core\"\n\n[[step]]\nwait = \"NEVER\"\n")
	artifacts := filepath.Join(t.TempDir(), "elsewhere")
	code := h.app.Run(context.Background(), []string{
		"e2e", "--binary", os.Args[0], "--out", artifacts, "--only", "one", "--size", "40x10",
	})
	if code != ExitFailure {
		t.Fatalf("exit = %d\n%s", code, h.out())
	}
	if _, err := os.Stat(filepath.Join(artifacts, "one", "40x10", "frame.txt")); err != nil {
		t.Errorf("the frame was not written where it was asked for: %v", err)
	}
}

func TestASummaryThatCannotBeWrittenIsNotFatal(t *testing.T) {
	h := newHarness(t, fakeRunner{})
	laySuite(t, h, ready)
	code := h.app.Run(context.Background(), []string{
		"e2e", "--binary", os.Args[0], "--summary", t.TempDir(), "--jobs", "1",
	})
	if code != ExitOK {
		t.Errorf("exit = %d\n%s", code, h.err())
	}
}

func TestABinarySuffixIsOnlyNeededOnWindows(t *testing.T) {
	if got := binarySuffix(); got != "" && got != ".exe" {
		t.Errorf("binarySuffix() = %q", got)
	}
}

func TestIndentPutsEveryLineUnderTheStepItBelongsTo(t *testing.T) {
	if got := indent("one\ntwo\n"); got != "        one\n        two" {
		t.Errorf("indent() = %q", got)
	}
}
