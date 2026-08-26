package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/sonquer/opendba/src/cli/internal/driver"
)

func longList(n int) []driver.Index {
	indexes := make([]driver.Index, n)
	for i := range indexes {
		indexes[i] = driver.Index{
			Schema: "dbo", Table: fmt.Sprintf("Table%d", i%400),
			Name: fmt.Sprintf("IX_Something_%d", i), Size: int64(i) * 1024,
			Scans: -1, Rows: -1,
		}
	}
	return indexes
}

// TestALongListIsAsLongAsItSays is what the whole scheme rests on: a row nobody
// will see is left blank rather than dressed, so it still takes its line and
// scrolling is unchanged.
func TestALongListIsAsLongAsItSays(t *testing.T) {
	theme := Default()
	indexes := longList(2000)
	whole := theme.IndexList(indexes, List{Cursor: 0, Width: 160, Sort: -1})
	windowed := theme.IndexList(indexes, List{Cursor: 0, Width: 160, Sort: -1, Offset: 900, Height: 40})

	if got, want := lines(windowed), lines(whole); got != want {
		t.Fatalf("a windowed list is %d lines, want the %d it would be drawn in full", got, want)
	}
	if MaxOffset(windowed, 40) != MaxOffset(whole, 40) {
		t.Error("a windowed list must scroll exactly as far as a whole one")
	}
}

func TestOnlyTheRowsThatWillBeSeenAreDrawn(t *testing.T) {
	theme := Default()
	windowed := theme.IndexList(longList(2000), List{Cursor: 0, Width: 160, Sort: -1, Offset: 900, Height: 40})
	shown, _ := Window(windowed, 900, 40)
	if strings.TrimSpace(shown) == "" {
		t.Fatal("the part of the list that is on screen must be drawn")
	}
	if !strings.Contains(shown, "IX_Something_9") {
		t.Errorf("the window must hold the rows it is scrolled to:\n%s", shown)
	}
	far, _ := Window(windowed, 0, 4)
	if strings.Contains(far, "IX_Something_1999") {
		t.Error("a row far outside the window is not drawn")
	}
}

func TestAListThatWasNotToldItsHeightDrawsAllOfIt(t *testing.T) {
	theme := Default()
	whole := theme.IndexList(longList(50), List{Cursor: -1, Width: 160, Sort: -1})
	for _, want := range []string{"IX_Something_0", "IX_Something_49"} {
		if !strings.Contains(whole, want) {
			t.Errorf("a list written out in full must hold %q", want)
		}
	}
}

func TestSeenCoversWhatSitsAboveTheRows(t *testing.T) {
	list := List{Offset: 100, Height: 40}
	if !list.Seen(100 - lead) {
		t.Error("the rows a screen draws above its list must be allowed for")
	}
	if list.Seen(100-lead-1) || list.Seen(140) {
		t.Error("a row well outside the window is not drawn")
	}
	if !(List{}).Seen(999999) {
		t.Error("a list with no height draws everything")
	}
}

func lines(content string) int { return len(strings.Split(content, "\n")) }
