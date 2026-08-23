package app

import (
	"strconv"
	"strings"
	"testing"

	"github.com/sonquer/tui4db/src/cli/internal/ui"
)

func result4Rows(t *testing.T, rows, height int) results {
	t.Helper()
	values := make([][]any, 0, rows)
	for i := range rows {
		values = append(values, []any{int64(i), "value-" + strconv.Itoa(i)})
	}
	return newResults(ui.Default(), queriedMsg{
		statement: "SELECT 1",
		columns:   []string{"id", "name"},
		rows:      values,
	}, 60, height)
}

// The pane only ever holds the rows it can show, so a result of any size costs
// the same to draw.
func TestOnlyTheRowsOnScreenAreGivenToTheTable(t *testing.T) {
	r := result4Rows(t, 5000, 12)
	if got := len(r.table.Rows()); got != r.visible() {
		t.Errorf("the table holds %d rows, the pane shows %d", got, r.visible())
	}
	moved := r.move(4000)
	if got := len(moved.table.Rows()); got != moved.visible() {
		t.Errorf("and still holds %d after moving", got)
	}
	if moved.first == 0 {
		t.Error("moving that far must have moved the window")
	}
}

// The cursor stays on screen wherever it is taken.
func TestTheCursorStaysOnScreen(t *testing.T) {
	r := result4Rows(t, 100, 12)
	for _, want := range []struct {
		name string
		step int
	}{
		{"down one", 1},
		{"a page", 10},
		{"past the end", 500},
		{"back past the start", -500},
	} {
		t.Run(want.name, func(t *testing.T) {
			r = r.move(want.step)
			if r.cursor < r.first || r.cursor >= r.first+r.visible() {
				t.Errorf("cursor %d is outside the window %d..%d",
					r.cursor, r.first, r.first+r.visible())
			}
			if r.cursor < 0 || r.cursor > 99 {
				t.Errorf("cursor = %d, it must stop at the ends", r.cursor)
			}
		})
	}
}

// A column keeps its width while scrolling, or every column would jump about as
// the rows under it changed.
func TestColumnsKeepTheirWidthWhileScrolling(t *testing.T) {
	values := make([][]any, 0, 60)
	for i := range 60 {
		values = append(values, []any{int64(i), strings.Repeat("x", 1+i)})
	}
	r := newResults(ui.Default(), queriedMsg{
		statement: "SELECT 1",
		columns:   []string{"id", "name"},
		rows:      values,
	}, 60, 10)
	first, _ := r.window()
	last, _ := r.move(59).window()
	if len(first) != len(last) {
		t.Fatalf("columns = %d then %d", len(first), len(last))
	}
	for i := range first {
		if first[i].Width != last[i].Width {
			t.Errorf("column %d was %d wide and is now %d",
				i, first[i].Width, last[i].Width)
		}
	}
}

// A result taller than its pane says where in it the cursor is.
func TestATallResultSaysWhereYouAre(t *testing.T) {
	r := result4Rows(t, 200, 10)
	if place := r.place4Row(); !strings.Contains(place, "row 1 of 200") {
		t.Errorf("place = %q", place)
	}
	short := result4Rows(t, 2, 10)
	if place := short.place4Row(); place != "" {
		t.Errorf("place = %q, a result that fits needs no directions", place)
	}
}

// A row of the pane maps back to the row of the result it is showing.
func TestALineOfThePaneMapsBackToARow(t *testing.T) {
	r := result4Rows(t, 100, 12).move(50)
	for _, want := range []struct {
		name string
		line int
		row  int
		ok   bool
	}{
		{"the header", 0, 0, false},
		{"the rule", 1, 0, false},
		{"the first row shown", resultHeaderRows, r.first, true},
		{"the third row shown", resultHeaderRows + 2, r.first + 2, true},
		{"below the rows", resultHeaderRows + 500, 0, false},
	} {
		t.Run(want.name, func(t *testing.T) {
			at, ok := r.rowAt(want.line)
			if ok != want.ok || (ok && at != want.row) {
				t.Errorf("rowAt(%d) = %d, %v; want %d, %v", want.line, at, ok, want.row, want.ok)
			}
		})
	}
}

// A value is copied whole, without the folding and cutting a table does to fit
// it in a column.
func TestACopiedValueIsNotTheOneOnScreen(t *testing.T) {
	long := strings.Repeat("x", 200)
	r := newResults(ui.Default(), queriedMsg{
		statement: "SELECT 1",
		columns:   []string{"note"},
		rows:      [][]any{{long + "\nsecond line"}},
	}, 60, 8)
	value, ok := r.cell()
	if !ok {
		t.Fatal("there is a value under the cursor")
	}
	if value != long+"\nsecond line" {
		t.Errorf("the value was changed on its way out: %q", value)
	}
	if !strings.Contains(r.cells[0][0], "…") {
		t.Error("while what is drawn is still cut to fit")
	}
}

// Nothing is picked before anything is dragged.
func TestNothingIsPickedBeforeADrag(t *testing.T) {
	r := result4Rows(t, 5, 8)
	if _, ok := r.picked(); ok {
		t.Error("no drag has started")
	}
	if empty := (results{}).startPicking(0); empty.anchor != 0 {
		t.Error("and a result that is not there cannot start one")
	}
	if empty := (results{}).pickTo(2); empty.cursor != 0 {
		t.Error("nor carry one")
	}
}

// A result wider than the pane walks sideways and says where it is, and stops
// at both ends.
func TestAWideResultStopsAtBothEnds(t *testing.T) {
	columns := make([]string, 0, 6)
	row := make([]any, 0, 6)
	for i := range 6 {
		columns = append(columns, "column_"+strconv.Itoa(i))
		row = append(row, strings.Repeat("x", 20))
	}
	r := newResults(ui.Default(), queriedMsg{
		statement: "SELECT 1", columns: columns, rows: [][]any{row},
	}, 40, 8)
	if r.shift(-1).column != 0 {
		t.Error("there is nothing left of the first column")
	}
	far := r
	for range 20 {
		far = far.shift(1)
	}
	if far.column != len(columns)-1 {
		t.Errorf("column = %d, it must stop at the last one", far.column)
	}
	if place := far.place(); place == "" {
		t.Error("a result too wide to show must say where it is")
	}
	if empty := (results{}).shift(1); empty.column != 0 {
		t.Error("a result that is not there does not walk")
	}
}

// A column is measured from the rows that arrived rather than from all of them,
// because measuring fifty thousand rows to draw the first twelve is work
// nobody is waiting for.
func TestAColumnIsMeasuredFromASample(t *testing.T) {
	cells := make([][]string, sampleRows+10)
	if len(sample(cells)) != sampleRows {
		t.Errorf("sample = %d, want %d", len(sample(cells)), sampleRows)
	}
	few := make([][]string, 3)
	if len(sample(few)) != 3 {
		t.Errorf("a short result is measured whole, got %d", len(sample(few)))
	}
}

// Nothing that is not there can be copied, walked or opened.
func TestAResultThatIsNotThereDoesNothing(t *testing.T) {
	for _, want := range []struct {
		name string
		from results
	}{
		{"nothing has run", results{}},
		{"it failed", results{present: true, failure: "boom"}},
		{"no rows", results{present: true, columns: []string{"a"}}},
	} {
		t.Run(want.name, func(t *testing.T) {
			if _, ok := want.from.cell(); ok {
				t.Error("there is no value to copy")
			}
			if _, ok := want.from.record(); ok {
				t.Error("nor a row to open")
			}
			if _, ok := want.from.rowAt(resultHeaderRows); ok {
				t.Error("nor a row under the pointer")
			}
		})
	}
	ragged := newResults(ui.Default(), queriedMsg{
		statement: "SELECT 1",
		columns:   []string{"a", "b", "c"},
		rows:      [][]any{{int64(1)}},
	}, 60, 8)
	ragged.column = 2
	if _, ok := ragged.cell(); ok {
		t.Error("a row that is shorter than its columns holds no value there")
	}
}

// The window follows the cursor in both directions.
func TestTheWindowFollowsTheCursorBothWays(t *testing.T) {
	r := result4Rows(t, 100, 12)
	down := r.move(60)
	if down.first == 0 {
		t.Fatal("going down must have moved the window")
	}
	up := down.move(-60)
	if up.first != 0 {
		t.Errorf("first = %d, coming back up must bring the window with it", up.first)
	}
	if up.cursor != 0 {
		t.Errorf("cursor = %d", up.cursor)
	}
}

// A drag that starts below the last row lands on the last row.
func TestADragPastTheEndLandsOnTheLastRow(t *testing.T) {
	r := result4Rows(t, 5, 12)
	if picked := r.startPicking(50); picked.anchor != 4 {
		t.Errorf("anchor = %d, want the last row", picked.anchor)
	}
	if carried := r.startPicking(0).pickTo(-5); carried.cursor != 0 {
		t.Errorf("cursor = %d, want the first row", carried.cursor)
	}
}

// A shifted key is the same key. Terminals send shift+backspace when the shift
// is held down for anything else, and an editor where holding shift stops
// backspace working is an editor that looks broken.
func TestAShiftedKeyIsStillTheKey(t *testing.T) {
	m := typeInto(t, workbench(t), "SELECT")
	for _, want := range []struct {
		name string
		key  string
		then string
	}{
		{"backspace", "backspace", "SELEC"},
		{"backspace with shift held", "shift+backspace", "SELEC"},
	} {
		t.Run(want.name, func(t *testing.T) {
			typed := typeInto(t, workbench(t), "SELECT")
			after, _ := press(t, typed, want.key)
			if after.statement() != want.then {
				t.Errorf("statement = %q, want %q", after.statement(), want.then)
			}
		})
	}
	shifted, _ := press(t, m, "shift+enter")
	if !strings.Contains(shifted.statement(), "\n") {
		t.Errorf("statement = %q, a shifted enter is still a newline", shifted.statement())
	}
}
