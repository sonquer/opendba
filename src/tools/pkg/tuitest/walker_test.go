package tuitest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"

	"github.com/sonquer/opendba/src/tools/pkg/tuitest/shot"
)

func drawing(t *testing.T) *Session {
	t.Helper()
	session := started(t, fakeOptions(t, 40, 10))
	session.Await(soon(), func(f Frame) bool { return f.Contains("READY") })
	return session
}

func TestAStepThatCannotBeTakenIsReported(t *testing.T) {
	session := drawing(t)
	walk := &walker{suite: Suite{}}
	cases := map[string]struct {
		step Step
		says string
	}{
		"a key that does not exist":       {Step{Key: "hyper+z"}, "is not a modifier"},
		"a burst with one that does not":  {Step{Keys: []string{"a", "hyper+z"}}, "is not a modifier"},
		"a pattern that will not compile": {Step{Match: "(["}, "error parsing regexp"},
		"a size that is not one":          {Step{Resize: "huge"}, "120x36"},
		"a step that does nothing":        {Step{}, "does nothing"},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			failure := walk.take(session, test.step, small, soon())
			if failure == nil {
				t.Fatal("the step was taken")
			}
			if !strings.Contains(failure.Reason, test.says) {
				t.Errorf("reason = %q, want it to mention %q", failure.Reason, test.says)
			}
		})
	}
}

func TestAScreenThatCannotBeKeptIsReported(t *testing.T) {
	session := drawing(t)
	blocked := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocked, nil, 0o600); err != nil {
		t.Fatalf("set up = %v", err)
	}
	walk := &walker{suite: Suite{Goldens: filepath.Join(blocked, "under")}, update: true}
	failure := walk.take(session, Step{Shot: "one"}, small, soon())
	if failure == nil {
		t.Fatal("a screen was kept where it cannot be written")
	}
}

func TestAScreenThatCannotBeReadIsReported(t *testing.T) {
	session := drawing(t)
	goldens := t.TempDir()
	if err := os.MkdirAll(filepath.Join(goldens, small.String(), "one.txt"), 0o755); err != nil {
		t.Fatalf("set up = %v", err)
	}
	walk := &walker{suite: Suite{Goldens: goldens}}
	failure := walk.take(session, Step{Shot: "one"}, small, soon())
	if failure == nil {
		t.Fatal("a directory was read as a screen")
	}
}

func TestSendingToAProgramThatHasGoneIsReported(t *testing.T) {
	session, err := Start(fakeOptions(t, 40, 10))
	if err != nil {
		t.Fatalf("Start = %v", err)
	}
	session.Await(soon(), func(f Frame) bool { return f.Contains("READY") })
	if err := session.Type("q"); err != nil {
		t.Fatalf("Type = %v", err)
	}
	if _, err := session.Close(); err != nil {
		t.Fatalf("Close = %v", err)
	}
	if err := session.Send([]byte("a")); err == nil {
		t.Error("bytes were sent to a terminal that is closed")
	}
	if err := session.Resize(20, 5); err == nil {
		t.Error("a closed terminal was resized")
	}
}

func TestTranslateReadsWhatACellWasDrawnWith(t *testing.T) {
	if got := translate(nil); got.Content != " " {
		t.Errorf("translate(nil) = %#v", got)
	}
	cell := &uv.Cell{Content: "x"}
	cell.Style.Attrs = uv.AttrBold
	cell.Style.Underline = uv.UnderlineSingle
	got := translate(cell)
	if got.Content != "x" || !got.Bold || !got.Underline {
		t.Errorf("translate = %#v", got)
	}
}

func TestTheGridIsTheScreenAsCells(t *testing.T) {
	session := drawing(t)
	grid := session.Grid()
	if len(grid) != 10 || len(grid[0]) != 40 {
		t.Fatalf("the grid is %d rows of %d", len(grid), len(grid[0]))
	}
	var first strings.Builder
	for _, cell := range grid[0] {
		first.WriteString(cell.Content)
	}
	if !strings.Contains(first.String(), "READY") {
		t.Errorf("the first row is %q", first.String())
	}
}

func TestArtifactsThatCannotBeWrittenAreReported(t *testing.T) {
	dir := t.TempDir()
	result := Result{Scenario: "one", Size: small}
	if err := os.MkdirAll(filepath.Join(dir, "frame.svg"), 0o755); err != nil {
		t.Fatalf("set up = %v", err)
	}
	if err := result.Keep(dir, [][]shot.Cell{{{Content: "x"}}}, time.Unix(0, 0)); err == nil {
		t.Error("a picture was written over a directory")
	}
	other := t.TempDir()
	if err := os.MkdirAll(filepath.Join(other, "session.cast"), 0o755); err != nil {
		t.Fatalf("set up = %v", err)
	}
	if err := result.Keep(other, nil, time.Unix(0, 0)); err == nil {
		t.Error("a recording was written over a directory")
	}
}

func TestSeedCannotReplaceWhatWillNotGoAway(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "core.sql")
	if err := os.WriteFile(script, []byte("CREATE TABLE t (id integer);"), 0o600); err != nil {
		t.Fatalf("write = %v", err)
	}
	stubborn := filepath.Join(dir, "held")
	if err := os.MkdirAll(filepath.Join(stubborn, "inside"), 0o755); err != nil {
		t.Fatalf("set up = %v", err)
	}
	if err := os.Chmod(stubborn, 0o500); err != nil {
		t.Fatalf("set up = %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(stubborn, 0o700) })
	if err := Seed(filepath.Join(stubborn, "inside"), script); err == nil {
		t.Skip("this platform lets a read-only directory be emptied")
	}
}
