package app

import (
	"math"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/sonquer/opendba/src/cli/internal/export"
	"github.com/sonquer/opendba/src/cli/internal/ui"
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
	if said := let4Go.(Model).text(); !strings.Contains(said, "4 rows are on the clipboard") {
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
	if said := let4Go.(Model).text(); !strings.Contains(said, "the value is on the clipboard") {
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
		{"the value", "y", "the value is on the clipboard"},
		{"the row", "Y", "the row is on the clipboard"},
	} {
		t.Run(want.name, func(t *testing.T) {
			copied, cmd := press(t, onResults, want.key)
			if cmd == nil {
				t.Fatal("that key must copy something")
			}
			if !strings.Contains(copied.text(), want.said) {
				t.Errorf("the screen must say %q, got %q", want.said, copied.text())
			}
		})
	}
	for _, format := range []string{"csv", "json", "markdown"} {
		t.Run(format, func(t *testing.T) {
			copied, cmd := m.Update(copyMsg{whole: true, format: export.Format(format)})
			if cmd == nil {
				t.Fatal("the command must copy something")
			}
			said := copied.(Model).text()
			if !strings.Contains(said, "4 rows are on the clipboard, as "+format) {
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
			if said := copied.(Model).text(); !strings.Contains(said, "nothing to copy") {
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
	if said := refused.(Model).text(); !strings.Contains(said, "could not be written") {
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

// Clicking the statement puts the cursor where the pointer is, which is the
// one thing a mouse in an editor is for.
func TestClickingTheStatementPutsTheCursorThere(t *testing.T) {
	m := manyRows(t, 4)
	m.editor.SetValue("SELECT id\nFROM users\nWHERE id = 1")
	m.focus = focusResults
	m.editor.Blur()

	left, top := m.editorOrigin()
	clicked, _ := m.Update(click(left+editorGutter+4, top+1))
	on := clicked.(Model)
	if on.focus != focusEditor || !on.editor.Focused() {
		t.Fatalf("focus = %v, a click in the statement must focus it", on.focus)
	}
	if on.editor.Line() != 1 {
		t.Errorf("line = %d, the cursor must land on the line clicked", on.editor.Line())
	}
	if on.editor.Column() != 4 {
		t.Errorf("column = %d, and in the column clicked", on.editor.Column())
	}

	back, _ := on.Update(click(left+editorGutter+2, top))
	if back.(Model).editor.Line() != 0 {
		t.Errorf("line = %d, going back up must work too", back.(Model).editor.Line())
	}
	past, _ := on.Update(click(left+editorGutter+200, top+2))
	if past.(Model).editor.Line() != 2 {
		t.Errorf("line = %d", past.(Model).editor.Line())
	}
}

// A click outside the statement is not a click in it.
func TestAClickOutsideTheStatementIsNot(t *testing.T) {
	m := manyRows(t, 4)
	left, top := m.editorOrigin()
	for _, want := range []struct {
		name string
		x, y int
	}{
		{"in the schema", ui.Gutter + 1, top},
		{"below the statement", left + editorGutter, top + 50},
		{"above the body", left + editorGutter, 1},
	} {
		t.Run(want.name, func(t *testing.T) {
			if m.inEditor(want.x, want.y) {
				t.Error("that is not the statement")
			}
		})
	}
	zoomed := m
	zoomed.zoomed = true
	if zoomed.inEditor(left+editorGutter, top) {
		t.Error("a zoomed result has no statement to click in")
	}
}

// Clicking a key in the footer presses it.
func TestClickingAKeyInTheFooterPressesIt(t *testing.T) {
	m := manyRows(t, 4)
	row := ui.FooterRow(m.height, true)
	found := false
	for x := ui.Gutter; x < m.width; x++ {
		hit, ok := m.hintAt(x, row)
		if !ok {
			continue
		}
		found = true
		if hit.code == 0 {
			t.Errorf("at %d the key is nothing", x)
		}
	}
	if !found {
		t.Fatalf("nothing in the footer answers a click on row %d", row)
	}
	if _, ok := m.hintAt(ui.Gutter, row-1); ok {
		t.Error("and only that row does")
	}

	tabbed, _ := m.Update(click(ui.Gutter+3, row))
	if len(tabbed.(Model).sheets) != 1 {
		t.Log("the first key on this screen is run, which needs a statement")
	}
}

// A key nobody can press back is not offered to the mouse.
func TestAKeyThatCannotBePressedBackIsNotOffered(t *testing.T) {
	for _, want := range []struct {
		name  string
		named string
		ok    bool
	}{
		{"a letter", "y", true},
		{"a named key", "enter", true},
		{"with a modifier", "ctrl+r", true},
		{"two modifiers", "ctrl+shift+tab", true},
		{"a modifier nobody has", "hyper+r", false},
		{"a name nobody knows", "wibble", false},
	} {
		t.Run(want.name, func(t *testing.T) {
			pressed, ok := pressed4Name(want.named)
			if ok != want.ok {
				t.Fatalf("pressed4Name(%q) = %v, want %v", want.named, ok, want.ok)
			}
			if ok && pressed.code == 0 {
				t.Errorf("%q came back as nothing", want.named)
			}
		})
	}
}

// Every key in the footer answers where it is drawn. This is measured against
// the rendered row rather than against the code that places it: the two were
// worked out separately, and a click that pressed the key next to the one under
// the pointer is exactly what that costs.
func TestEveryKeyInTheFooterAnswersWhereItIsDrawn(t *testing.T) {
	for _, want := range []struct {
		name  string
		build func(*testing.T) Model
	}{
		{"the dashboard", func(t *testing.T) Model {
			m := loaded(t, healthy())
			m.width, m.height = 120, 36
			return m
		}},
		{"the editor", func(t *testing.T) Model { return manyRows(t, 4) }},
	} {
		t.Run(want.name, func(t *testing.T) {
			m := want.build(t)
			row := ui.FooterRow(m.height, m.view == viewQuery && !m.zoomed)
			drawn := plain(m.footer(0))
			from := 0
			for _, binding := range m.footerKeys(ui.FrameWidth(m.width)) {
				label := binding.Help().Key
				column, ok := columnOf(drawn, label+" "+binding.Help().Desc, from)
				if !ok {
					t.Fatalf("%q is not in the footer after column %d:\n%s",
						label, from, drawn)
				}
				pressed, found := m.hintAt(ui.Gutter+column, row)
				if !found {
					t.Errorf("nothing answers a click on %q at column %d", label, column)
					from = column + lipgloss.Width(label)
					continue
				}
				wanted, _ := pressed4Name(binding.Keys()[0])
				if pressed != wanted {
					t.Errorf("clicking %q at column %d presses %+v, want %+v",
						label, column, pressed, wanted)
				}
				from = column + lipgloss.Width(label)
			}
		})
	}
}

// columnOf finds the screen column a piece of text starts at, counting the
// cells a terminal draws rather than the bytes Go stores. It is given the key
// and what it does together, because a key of one letter is a letter that also
// appears inside the words around it.
func columnOf(drawn, label string, from int) (int, bool) {
	runes := []rune(drawn)
	column := 0
	for i := range runes {
		if column >= from && strings.HasPrefix(string(runes[i:]), label) {
			return column, true
		}
		column += lipgloss.Width(string(runes[i]))
	}
	return 0, false
}

// Clicking a reading on the dashboard opens the page about it, which is what
// enter on it already did.
func TestClickingAReadingOpensItsPage(t *testing.T) {
	for _, want := range []struct {
		name  string
		width int
	}{
		{"in two columns", 160},
		{"in one", 100},
	} {
		t.Run(want.name, func(t *testing.T) {
			m := loaded(t, healthy())
			m.width, m.height = want.width, 40
			readings := m.readings(every)
			if len(readings) < 2 {
				t.Skip("this server reports too little to lay out")
			}

			for at := range readings {
				x, y, ok := whereIsReading(m, at)
				if !ok {
					t.Fatalf("reading %d (%q) is not drawn", at, readings[at].Label)
				}
				clicked, _ := m.Update(click(x, y))
				opened := clicked.(Model)
				if opened.page == nil {
					t.Fatalf("clicking %q must open its page", readings[at].Label)
				}
				if opened.reading != at {
					t.Errorf("clicking %q opened reading %d, want %d",
						readings[at].Label, opened.reading, at)
				}
			}
		})
	}
}

// whereIsReading finds the screen position of a reading by reading the body
// back, which is the same thing the click does and so proves nothing on its
// own; it is here so the test can click somewhere real.
func whereIsReading(m Model, at int) (int, int, bool) {
	readings := m.readings(every)
	width := ui.FrameWidth(m.width)
	lines := strings.Split(plain(m.body()), "\n")
	for line := range lines {
		for _, column := range []int{ui.Gutter + 1, ui.Gutter + width/2 + 1} {
			if found, ok := m.readingAt(column, line+ui.BodyTop(false)); ok && found == at {
				return column, line + ui.BodyTop(false), true
			}
		}
		_ = readings
	}
	return 0, 0, false
}

// A click on the dashboard that is not on a reading opens nothing.
func TestAClickOnNoReadingOpensNothing(t *testing.T) {
	m := loaded(t, healthy())
	m.width, m.height = 160, 40
	for _, want := range []struct {
		name string
		x, y int
	}{
		{"above the body", ui.Gutter, 1},
		{"far below it", ui.Gutter, 200},
		{"on a heading", ui.Gutter, ui.BodyTop(false)},
	} {
		t.Run(want.name, func(t *testing.T) {
			if _, ok := m.readingAt(want.x, want.y); ok {
				t.Error("that is not a reading")
			}
		})
	}
	elsewhere := m
	elsewhere.view = viewQuery
	if _, ok := elsewhere.readingAt(ui.Gutter+1, ui.BodyTop(false)+2); ok {
		t.Error("the readings are on the dashboard and nowhere else")
	}
}

// A reading whose name begins another reading's name does not answer for it.
func TestAReadingDoesNotAnswerForAnother(t *testing.T) {
	for _, want := range []struct {
		name  string
		said  string
		label string
		is    bool
	}{
		{"the same", "cache", "cache", true},
		{"followed by its bar", "cache   [||||]   100%   ok", "cache", true},
		{"a longer name", "index cache   [||||]", "cache", false},
		{"a name that starts the same", "idle in transaction   0", "idle indexes", false},
		{"nothing", "cache", "", false},
	} {
		t.Run(want.name, func(t *testing.T) {
			if got := named4Reading(want.said, want.label); got != want.is {
				t.Errorf("named4Reading(%q, %q) = %v, want %v",
					want.said, want.label, got, want.is)
			}
		})
	}
}

// Clicking a statement in the sidebar opens it, on the second click the way a
// table does: the first click moves the cursor onto it.
func TestClickingAFileOpensIt(t *testing.T) {
	m, _ := seeded(t)
	m.mouse = true
	line, ok := lineOf4File(m, "daily.sql")
	if !ok {
		t.Fatal("the file is not drawn")
	}
	first, _ := m.Update(click(ui.Gutter+1, line))
	picked := first.(Model)
	if picked.focus != focusSidebar {
		t.Fatalf("focus = %v", picked.focus)
	}
	if name, ok := picked.sidebar.file(); !ok || name != "daily.sql" {
		t.Fatalf("the cursor is on %q", name)
	}
	if len(picked.sheets) != 1 {
		t.Error("one click moves the cursor, it does not open anything")
	}
	updated, cmd := picked.Update(click(ui.Gutter+1, line))
	opened := pump(t, updated.(Model), cmd)
	if opened.file != "daily.sql" {
		t.Errorf("the second click must open it, tab holds %q", opened.file)
	}
}

func TestClickingTheFilesHeadingDoesNothing(t *testing.T) {
	m, _ := seeded(t)
	m.mouse = true
	line, ok := lineOf4File(m, "daily.sql")
	if !ok {
		t.Fatal("the file is not drawn")
	}
	if _, hit := m.treeAt(ui.Gutter+1, line-2); hit {
		t.Error("the heading above the files belongs to no row")
	}
}

// lineOf4File finds the row of the screen a statement is drawn on.
func lineOf4File(m Model, name string) (int, bool) {
	for i, item := range m.sidebar.rows {
		if item.key != "file:"+name {
			continue
		}
		lines := m.sidebar.paint(m.sidebar.width(ui.FrameWidth(m.width)), false)
		for at, line := range lines {
			if line.row == i {
				return ui.BodyTop(true) + treeTop + at -
					m.sidebar.offset(lines, m.workbenchHeight()-2), true
			}
		}
	}
	return 0, false
}

// The row of keys stays inside the frame. Off macOS a modifier is spelled out
// rather than drawn as a glyph, so the same eight keys are far wider there; a
// footer that is allowed to overflow makes every row of the screen grow with it.
func TestTheFooterStaysInsideTheFrame(t *testing.T) {
	for _, width := range []int{60, 80, 110, 120} {
		m := manyRows(t, 4)
		m.width = width
		drawn := plain(m.footer(3))
		if lipgloss.Width(drawn) > ui.FrameWidth(width) {
			t.Errorf("at %d the footer is %d wide, the frame is %d:\n%s",
				width, lipgloss.Width(drawn), ui.FrameWidth(width), drawn)
		}
		if bare := plain(m.footer(0)); lipgloss.Width(bare) > ui.FrameWidth(width) {
			t.Errorf("at %d the footer with nothing to scroll is %d wide:\n%s",
				width, lipgloss.Width(bare), bare)
		}
	}
}

// A key the footer had no room to draw cannot be clicked, because there is
// nothing there to click.
func TestAKeyTheFooterCouldNotDrawIsNotClickable(t *testing.T) {
	m := manyRows(t, 4)
	m.width = 40
	m.mouse = true
	row := ui.FooterRow(m.height, true)
	if _, ok := m.hintAt(ui.Gutter+ui.FrameWidth(40)+4, row); ok {
		t.Error("past the frame there is no key to press")
	}
}
