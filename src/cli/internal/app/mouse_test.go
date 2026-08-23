package app

import (
	"math"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/sonquer/tui4db/src/cli/internal/export"
	"github.com/sonquer/tui4db/src/cli/internal/ui"
)

// The mouse can be handed back to the terminal, because a terminal reporting
// the mouse to a program is a terminal that cannot select text with it.
func TestTheMouseCanBeHandedBackToTheTerminal(t *testing.T) {
	m := workbench(t)
	if !m.mouse || m.View().MouseMode != tea.MouseModeCellMotion {
		t.Fatal("the mouse is read to begin with")
	}
	given, cmd := m.Update(mouseMsg{})
	off := given.(Model)
	if off.mouse || off.View().MouseMode != tea.MouseModeNone {
		t.Error("the command must stop the terminal reporting it")
	}
	if cmd == nil {
		t.Error("and must say so, because nothing else on screen changes")
	}

	second, _ := press(t, off, "ctrl+n")
	clicked, _ := second.Update(click(ui.Gutter+1, ui.TabTop))
	if clicked.(Model).sheet != 1 {
		t.Error("a click the terminal kept must not reach the tabs")
	}
	rolled, _ := second.Update(wheel(ui.Gutter+1, tea.MouseWheelDown))
	if rolled.(Model).sidebar.cursor != second.sidebar.cursor {
		t.Error("nor a wheel")
	}

	back, _ := off.Update(mouseMsg{})
	if !back.(Model).mouse {
		t.Error("and it can be taken again")
	}
}

// A profile that says the mouse is not wanted never asks for it.
func TestAProfileCanRefuseTheMouseFromTheStart(t *testing.T) {
	m := workbench(t)
	m.mouse = false
	if m.View().MouseMode != tea.MouseModeNone {
		t.Error("a terminal that was never asked must not be reporting")
	}
	if strings.Contains(plain(m.content()), "\x1b") {
		t.Error("nothing about the setting belongs on the screen")
	}
}

// Dragging over the result selects rows and letting go puts them on the
// clipboard, because a selection nobody copied was a selection for nothing.
func TestDraggingOverTheResultCopiesIt(t *testing.T) {
	m := manyRows(t, 20)
	top := m.resultsTop() + resultHeaderRows

	picked, _ := m.Update(click(80, top))
	held := picked.(Model)
	if held.focus != focusResults || held.results.anchor != 0 {
		t.Fatalf("a click in the result starts a selection: focus %v anchor %d",
			held.focus, held.results.anchor)
	}

	moved, _ := held.Update(tea.MouseMotionMsg{X: 80, Y: top + 3, Button: tea.MouseLeft})
	dragged := moved.(Model)
	if dragged.results.cursor != 3 {
		t.Fatalf("cursor = %d, the selection must follow the pointer", dragged.results.cursor)
	}
	if !strings.Contains(plain(dragged.content()), "4 selected") {
		t.Errorf("the screen must say how much is selected:\n%s", plain(dragged.content()))
	}

	let4Go, cmd := dragged.Update(tea.MouseReleaseMsg{X: 80, Y: top + 3, Button: tea.MouseLeft})
	if let4Go.(Model).results.anchor >= 0 {
		t.Error("letting go must end the selection")
	}
	if cmd == nil {
		t.Fatal("and must copy it")
	}
	if said := let4Go.(Model).text; !strings.Contains(said, "copied 4 rows") {
		t.Errorf("and say so, got %q", said)
	}
}

// A drag that never left the row it started on is one value, which is what
// somebody reaching for the mouse to grab an id wanted.
func TestADragThatWentNowhereCopiesTheValue(t *testing.T) {
	m := manyRows(t, 5)
	top := m.resultsTop() + resultHeaderRows
	picked, _ := m.Update(click(80, top+1))
	let4Go, cmd := picked.(Model).Update(tea.MouseReleaseMsg{X: 80, Y: top + 1, Button: tea.MouseLeft})
	if cmd == nil {
		t.Fatal("letting go must copy something")
	}
	if said := let4Go.(Model).text; !strings.Contains(said, "copied the value") {
		t.Errorf("one row under the pointer is one value, got %q", said)
	}
}

// A drag with nothing under it does nothing at all.
func TestADragOutsideTheResultIsIgnored(t *testing.T) {
	m := manyRows(t, 5)
	away, _ := m.Update(tea.MouseMotionMsg{X: 80, Y: 1, Button: tea.MouseLeft})
	if away.(Model).results.anchor >= 0 {
		t.Error("nothing was being dragged")
	}
	let4Go, cmd := away.(Model).Update(tea.MouseReleaseMsg{X: 80, Y: 1, Button: tea.MouseLeft})
	if cmd != nil || let4Go.(Model).results.anchor >= 0 {
		t.Error("and nothing is copied")
	}
}

// y copies the value, Y the row, and the palette the whole result in whichever
// format was asked for.
func TestCopyingWithoutAMouse(t *testing.T) {
	m := manyRows(t, 4)
	onResults, _ := press(t, m, "tab")
	if onResults.focus != focusResults {
		t.Fatalf("focus = %v", onResults.focus)
	}
	for _, want := range []struct {
		name string
		key  string
		said string
	}{
		{"the value", "y", "copied the value"},
		{"the row", "Y", "copied the row"},
	} {
		t.Run(want.name, func(t *testing.T) {
			copied, cmd := press(t, onResults, want.key)
			if cmd == nil {
				t.Fatal("that key must copy something")
			}
			if !strings.Contains(copied.text, want.said) {
				t.Errorf("the screen must say %q, got %q", want.said, copied.text)
			}
		})
	}
	for _, format := range []string{"csv", "json", "markdown"} {
		t.Run(format, func(t *testing.T) {
			copied, cmd := m.Update(copyMsg{whole: true, format: export.Format(format)})
			if cmd == nil {
				t.Fatal("the command must copy something")
			}
			said := copied.(Model).text
			if !strings.Contains(said, "copied 4 rows as "+format) {
				t.Errorf("the screen must name what went over:\n%s", said)
			}
		})
	}
}

// There is nothing to copy before anything has run.
func TestThereIsNothingToCopyYet(t *testing.T) {
	m := workbench(t)
	for _, name := range []string{"a value", "a result"} {
		t.Run(name, func(t *testing.T) {
			var copied tea.Model
			var cmd tea.Cmd
			if name == "a value" {
				copied, cmd = m.copiedCell()
			} else {
				copied, cmd = m.copied(copyMsg{whole: true, format: export.FormatCSV})
			}
			if cmd == nil {
				t.Fatal("it has to say so")
			}
			if said := copied.(Model).text; !strings.Contains(said, "nothing to copy") {
				t.Errorf("and say what it is saying, got %q", said)
			}
		})
	}
}

// manyRows is a model with a result on the editor screen, big enough to scroll.
func manyRows(t *testing.T, rows int) Model {
	t.Helper()
	m := typeInto(t, workbench(t), "SELECT 1")
	m.width, m.height = 120, 36
	values := make([][]any, 0, rows)
	for i := range rows {
		values = append(values, []any{int64(i), "value-" + strconv.Itoa(i)})
	}
	m.results = newResults(m.theme, queriedMsg{
		statement: "SELECT 1",
		columns:   []string{"id", "name"},
		rows:      values,
	}, m.paneWidth(), m.resultsHeight())
	return m
}

// Clicking a row of the schema puts the cursor on it, and clicking the row it
// is already on opens it.
func TestClickingTheSchemaMovesTheCursorAndThenOpens(t *testing.T) {
	m := workbench(t)
	m.width, m.height = 110, 34
	m.sidebar = m.sidebar.withTables(m.tables, m.fields)
	if len(m.sidebar.rows) < 2 {
		t.Fatalf("rows = %d", len(m.sidebar.rows))
	}

	line := m.lineOfTree(t, 1)
	moved, _ := m.Update(click(ui.Gutter+2, line))
	on := moved.(Model)
	if on.focus != focusSidebar || on.sidebar.cursor != 1 {
		t.Fatalf("focus = %v cursor = %d, a click must land on the row", on.focus, on.sidebar.cursor)
	}

	opened, _ := on.Update(click(ui.Gutter+2, line))
	if len(opened.(Model).sheets) != 2 {
		t.Error("clicking the row the cursor is on must open it")
	}
}

// A click on a heading, a connector or below the schema lands on nothing.
func TestAClickOnNothingInTheSchemaDoesNothing(t *testing.T) {
	m := workbench(t)
	m.width, m.height = 110, 34
	m.sidebar = m.sidebar.withTables(m.tables, m.fields)
	for _, miss := range []struct {
		name string
		y    int
	}{
		{"above the rows", ui.BodyTop(true)},
		{"far below them", ui.BodyTop(true) + 30},
	} {
		t.Run(miss.name, func(t *testing.T) {
			missed, _ := m.Update(click(ui.Gutter+2, miss.y))
			if missed.(Model).focus == focusSidebar {
				t.Error("that click was not on a row")
			}
		})
	}
	hidden := m
	hidden.sidebar.hidden = true
	if _, ok := hidden.treeAt(ui.Gutter+2, ui.BodyTop(true)+2); ok {
		t.Error("a schema that is put away takes no clicks")
	}
}

// lineOfTree finds the screen row a row of the schema is drawn on.
func (m Model) lineOfTree(t *testing.T, row int) int {
	t.Helper()
	width := ui.FrameWidth(m.width)
	side := m.sidebar.width(width)
	for line := range m.workbenchHeight() {
		if at, ok := m.sidebar.rowAt(side, m.workbenchHeight(), line, false); ok && at == row {
			return line + ui.BodyTop(true)
		}
	}
	t.Fatalf("row %d is not drawn", row)
	return 0
}

// A format that does not exist is refused rather than put on the clipboard as
// nothing.
func TestCopyingAFormatThatDoesNotExistIsRefused(t *testing.T) {
	if _, err := text4Clipboard([]string{"a"}, [][]any{{1}}, "parquet"); err == nil {
		t.Error("want an error")
	}
	m := manyRows(t, 2)
	refused, cmd := m.copied(copyMsg{whole: true, format: "parquet"})
	if cmd == nil {
		t.Fatal("it has to say so")
	}
	if said := refused.(Model).text; !strings.Contains(said, "could not be written") {
		t.Errorf("text = %q", said)
	}
}

// A value the format cannot hold stops the copy rather than putting half of it
// on the clipboard.
func TestAValueTheFormatCannotHoldStopsTheCopy(t *testing.T) {
	if _, err := text4Clipboard([]string{"ratio"},
		[][]any{{math.Inf(1)}}, export.FormatJSON); err == nil {
		t.Error("infinity is not a number JSON can hold")
	}
	if _, err := text4Clipboard([]string{strings.Repeat("n", 9000)},
		nil, export.FormatCSV); err != nil {
		t.Errorf("a long column name is still a column name: %v", err)
	}
}

// Where the panes are depends on what is on screen, and the mouse has to agree
// with what was drawn.
func TestWhereThePanesAreDependsOnWhatIsDrawn(t *testing.T) {
	m := manyRows(t, 4)
	if _, ok := m.rowAt(ui.Gutter+1, m.resultsTop()+resultHeaderRows); ok {
		t.Error("the left of the screen is the schema, not the result")
	}
	hidden := m
	hidden.sidebar.hidden = true
	if !hidden.overResults(ui.Gutter + 1) {
		t.Error("with the schema put away the whole width is the editor")
	}
	zoomed := m
	zoomed.zoomed = true
	if zoomed.resultsTop() != ui.BodyTop(false) {
		t.Errorf("resultsTop = %d, a zoomed result starts at the top of the body",
			zoomed.resultsTop())
	}
	if _, ok := m.treeAt(ui.Gutter+1, ui.BodyTop(true)+treeTop); !ok {
		t.Error("and the schema is where it was drawn")
	}
}

// The line between the statement and its result can be carried with the
// pointer, which is what somebody reaching for a mouse in an editor expects of
// a line between two panes.
func TestTheSplitCanBeDragged(t *testing.T) {
	m := manyRows(t, 20)
	before := m.editorRows()
	line := ui.BodyTop(true) + before
	if !m.onSplit(80, line) {
		t.Fatalf("the line is on row %d", line)
	}
	if m.onSplit(80, line+1) || m.onSplit(ui.Gutter+1, line) {
		t.Error("and only there")
	}

	held, _ := m.Update(click(80, line))
	dragging := held.(Model)
	if !dragging.dragging {
		t.Fatal("a click on the line must take hold of it")
	}
	moved, _ := dragging.Update(tea.MouseMotionMsg{X: 80, Y: line + 3, Button: tea.MouseLeft})
	taller := moved.(Model)
	if taller.editorRows() <= before {
		t.Errorf("editor = %d, dragging down must give the statement more room",
			taller.editorRows())
	}
	let4Go, _ := taller.Update(tea.MouseReleaseMsg{X: 80, Y: line + 3, Button: tea.MouseLeft})
	if let4Go.(Model).dragging {
		t.Error("letting go must let go")
	}
	if let4Go.(Model).editorRows() != taller.editorRows() {
		t.Error("and leave the panes where they were put")
	}
}

// The line stops where each pane still has the room it needs.
func TestTheSplitStopsWhereThePanesNeedTheRoom(t *testing.T) {
	m := manyRows(t, 20)
	held, _ := m.Update(click(80, ui.BodyTop(true)+m.editorRows()))
	dragging := held.(Model)

	top, _ := dragging.resizedBy(0)
	if top.(Model).editorRows() < minEditorRows {
		t.Errorf("editor = %d, below its floor", top.(Model).editorRows())
	}
	bottom, _ := dragging.resizedBy(500)
	if bottom.(Model).resultsHeight() < minResultsRows {
		t.Errorf("results = %d, below its floor", bottom.(Model).resultsHeight())
	}
}

// A drag with nothing held is a drag of nothing, and a zoomed result has no
// line to hold.
func TestNothingIsDraggedWhenNothingIsHeld(t *testing.T) {
	m := manyRows(t, 20)
	loose, _ := m.Update(tea.MouseMotionMsg{X: 80, Y: 12, Button: tea.MouseLeft})
	if loose.(Model).dragging || loose.(Model).split != 0 {
		t.Error("nothing was taken hold of")
	}
	zoomed := m
	zoomed.zoomed = true
	if zoomed.onSplit(80, ui.BodyTop(true)+zoomed.editorRows()) {
		t.Error("a zoomed result has no line beside it")
	}
	off := m
	off.mouse = false
	held, _ := off.Update(tea.MouseMotionMsg{X: 80, Y: 12, Button: tea.MouseLeft})
	if held.(Model).dragging {
		t.Error("and a terminal that kept the mouse sends nothing here")
	}
}
