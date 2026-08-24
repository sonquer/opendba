package app

import (
	"strings"
	"testing"

	"github.com/sonquer/tui4db/src/cli/internal/ui"
)

// Two things said at once are both shown, newest first, rather than one of them
// quietly replacing the other.
func TestTwoThingsSaidAtOnceAreBothShown(t *testing.T) {
	var said toaster
	if cmd := said.notify("the file is written"); cmd == nil {
		t.Fatal("saying something must arrange to stop saying it")
	}
	if cmd := said.alarm("the query failed"); cmd == nil {
		t.Fatal("and so must a complaint")
	}
	drawn := plain(said.render(ui.Default()))
	for _, want := range []string{"the file is written", "the query failed"} {
		if !strings.Contains(drawn, want) {
			t.Errorf("the corner must show %q:\n%s", want, drawn)
		}
	}
	newest := strings.Index(drawn, "the query failed")
	oldest := strings.Index(drawn, "the file is written")
	if newest > oldest {
		t.Errorf("the newest goes on top:\n%s", drawn)
	}
	if said.text() != "the query failed" {
		t.Errorf("text = %q, the newest is the one a screen with one line shows", said.text())
	}
	if strings.Contains(drawn, "·") {
		t.Errorf("a sentence is not a list and takes no bullet:\n%s", drawn)
	}
}

// One of them going does not take the others with it.
func TestOneGoingDoesNotTakeTheOthers(t *testing.T) {
	var said toaster
	said.notify("first")
	said.notify("second")
	said.expire(toastMsg{sequence: 1})
	drawn := plain(said.render(ui.Default()))
	if strings.Contains(drawn, "first") {
		t.Errorf("the one that expired must go:\n%s", drawn)
	}
	if !strings.Contains(drawn, "second") {
		t.Errorf("and the other must stay:\n%s", drawn)
	}
	said.expire(toastMsg{sequence: 99})
	if said.text() != "second" {
		t.Error("a sequence nobody knows takes nothing")
	}
	said.expire(toastMsg{sequence: 2})
	if said.render(ui.Default()) != "" || said.text() != "" {
		t.Error("the last one going leaves nothing drawn")
	}
}

// The corner does not fill up with things said a minute ago.
func TestTheCornerDoesNotFillUp(t *testing.T) {
	var said toaster
	for _, text := range []string{"one", "two", "three", "four", "five", "six"} {
		said.notify(text)
	}
	if len(said.notes) != toastsShown {
		t.Errorf("notes = %d, want %d", len(said.notes), toastsShown)
	}
	drawn := plain(said.render(ui.Default()))
	if strings.Contains(drawn, "one") || strings.Contains(drawn, "two") {
		t.Errorf("the oldest must go:\n%s", drawn)
	}
	if !strings.Contains(drawn, "six") {
		t.Errorf("and the newest must be there:\n%s", drawn)
	}
}

// A long sentence wraps rather than running off the screen, and every line of
// it is drawn on the same ground.
func TestALongSentenceWrapsOntoOneGround(t *testing.T) {
	var said toaster
	said.notify(strings.Repeat("a sentence that will not fit ", 4))
	drawn := plain(said.render(ui.Default()))
	lines := strings.Split(drawn, "\n")
	if len(lines) < 2 {
		t.Fatalf("it must wrap:\n%s", drawn)
	}
	widest := 0
	for _, line := range lines {
		if len([]rune(line)) > widest {
			widest = len([]rune(line))
		}
	}
	for i, line := range lines {
		if len([]rune(line)) != widest {
			t.Errorf("line %d is %d wide and the block is %d; the ground must be square",
				i, len([]rune(line)), widest)
		}
	}
	if widest > toastWidth+toastFrame {
		t.Errorf("width = %d, it must stop at %d plus its frame", widest, toastWidth)
	}
}
