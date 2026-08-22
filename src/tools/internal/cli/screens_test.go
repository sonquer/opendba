package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sonquer/tui4db/src/tools/internal/exec"
)

func TestEveryScriptIsNamedAndDoesSomething(t *testing.T) {
	seen := map[string]bool{}
	for _, current := range scripts {
		if current.name == "" || current.keys == "" {
			t.Errorf("a script needs a name and keys: %+v", current)
		}
		if seen[current.name] {
			t.Errorf("two scripts called %q would overwrite each other", current.name)
		}
		seen[current.name] = true
		if !strings.Contains(current.keys, "shot:"+current.name) {
			t.Errorf("%q must take a screenshot of itself: %q", current.name, current.keys)
		}
	}
	if len(sizes) < 2 {
		t.Error("screens are rendered at more than one size, which is where layout breaks")
	}
}

func TestDifferenceShowsWhatMoved(t *testing.T) {
	before := "one\ntwo\nthree"
	after := "one\ntwo and a half\nthree"
	got := difference(before, after)
	if !strings.Contains(got, "- two") || !strings.Contains(got, "+ two and a half") {
		t.Errorf("difference() = %q", got)
	}
	if strings.Contains(got, "one") || strings.Contains(got, "three") {
		t.Errorf("only the lines that moved belong in a diff: %q", got)
	}
	if got := difference("a", "a"); got != "  (only spacing)" {
		t.Errorf("difference() = %q", got)
	}
	if got := difference("", "a\nb"); !strings.Contains(got, "+ a") || !strings.Contains(got, "+ b") {
		t.Errorf("a new screen is all additions: %q", got)
	}
	if got := difference("a\nb", ""); !strings.Contains(got, "- a") {
		t.Errorf("a screen that lost lines says so: %q", got)
	}
}

func TestAtIsSafePastTheEnd(t *testing.T) {
	lines := []string{"one"}
	if at(lines, 0) != "one" || at(lines, 4) != "" {
		t.Error("reading past the end must be empty, not a panic")
	}
}

type drawer struct {
	calls []string
	fail  bool
	text  string
}

func (d *drawer) Run(_ context.Context, dir, name string, args ...string) (exec.Result, error) {
	d.calls = append(d.calls, name+" "+strings.Join(args, " "))
	if d.fail {
		return exec.Result{ExitCode: 1, Stderr: "no fixture"}, nil
	}
	return exec.Result{Stdout: d.text}, nil
}

func TestScreensWritesEveryScreenAtEverySize(t *testing.T) {
	out := t.TempDir()
	runner := &drawer{text: "a screen\n"}
	h := newHarness(t, runner)
	stdout := h.stdout

	if code := h.app.Run(context.Background(), []string{"screens", "--out", out}); code != ExitOK {
		t.Fatalf("exit = %d\n%s", code, stdout)
	}
	if len(runner.calls) != len(scripts)*len(sizes) {
		t.Fatalf("calls = %d, want %d", len(runner.calls), len(scripts)*len(sizes))
	}
	written, err := os.ReadFile(filepath.Join(out, sizes[0], scripts[0].name+".txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(written) != "a screen\n" {
		t.Errorf("written = %q", written)
	}
	if !strings.Contains(stdout.String(), "different") {
		t.Errorf("the run must say how much moved: %q", stdout)
	}

	runner.text = "another screen\n"
	stdout.Reset()
	if code := h.app.Run(context.Background(), []string{"screens", "--out", out, "--against", out}); code != ExitOK {
		t.Fatalf("exit = %d\n%s", code, stdout)
	}
	if !strings.Contains(stdout.String(), "+ another screen") {
		t.Errorf("a second run must print what moved:\n%s", stdout)
	}
}

func TestScreensReportsARenderThatFailed(t *testing.T) {
	h := newHarness(t, &drawer{fail: true})
	if code := h.app.Run(context.Background(), []string{"screens", "--out", t.TempDir()}); code != ExitFailure {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(h.err(), "no fixture") {
		t.Errorf("output = %q", h.err())
	}
}

func TestScreensPassesTheConnectionThrough(t *testing.T) {
	runner := &drawer{text: "x"}
	h := newHarness(t, runner)
	h.app.Run(context.Background(), []string{"screens", "--out", t.TempDir(), "--connection", "localhost"})
	if len(runner.calls) == 0 || !strings.Contains(runner.calls[0], "--connection localhost") {
		t.Errorf("calls = %v", runner.calls)
	}
}
