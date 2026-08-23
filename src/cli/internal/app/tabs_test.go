package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/sonquer/tui4db/src/cli/internal/ui"
)

// A tab holds its own statement, and coming back to it finds it as it was left.
func TestEachTabKeepsItsOwnStatement(t *testing.T) {
	m := typeInto(t, workbench(t), "SELECT 1")
	second, _ := press(t, m, "ctrl+n")
	if len(second.sheets) != 2 || second.statement() != "" {
		t.Fatalf("a new tab starts empty: %d tabs, statement %q",
			len(second.sheets), second.statement())
	}
	second = typeInto(t, second, "SELECT 2")

	back, _ := press(t, second, "ctrl+pgup")
	if back.statement() != "SELECT 1" {
		t.Errorf("statement = %q, going back must find the tab as it was left", back.statement())
	}
	forward, _ := press(t, back, "ctrl+pgdown")
	if forward.statement() != "SELECT 2" {
		t.Errorf("statement = %q, and so must going forward again", forward.statement())
	}
}

// Walking off either end comes back round, which is what tabs do everywhere.
func TestWalkingPastTheLastTabComesBackToTheFirst(t *testing.T) {
	m := workbench(t)
	if walked, _ := press(t, m, "ctrl+pgdown"); walked.sheet != 0 {
		t.Errorf("sheet = %d, one tab has nowhere to walk to", walked.sheet)
	}
	two, _ := press(t, m, "ctrl+n")
	wrapped, _ := press(t, two, "ctrl+pgdown")
	if wrapped.sheet != 0 {
		t.Errorf("sheet = %d, past the last tab is the first", wrapped.sheet)
	}
	behind, _ := press(t, wrapped, "ctrl+pgup")
	if behind.sheet != 1 {
		t.Errorf("sheet = %d, and before the first is the last", behind.sheet)
	}
}

// Closing the only tab empties it rather than leaving a screen with nothing on
// it to do.
func TestClosingTheLastTabLeavesAnEmptyOne(t *testing.T) {
	m := typeInto(t, workbench(t), "SELECT 1")
	closed := answerClose(t, m)
	if len(closed.sheets) != 1 {
		t.Fatalf("tabs = %d", len(closed.sheets))
	}
	if closed.statement() != "" {
		t.Errorf("statement = %q, closing the last tab clears it", closed.statement())
	}
}

// Closing a tab leaves the ones beside it, and lands on one of them.
func TestClosingATabLandsOnTheOneBeside(t *testing.T) {
	m := typeInto(t, workbench(t), "SELECT 1")
	second, _ := press(t, m, "ctrl+n")
	second = typeInto(t, second, "SELECT 2")
	third, _ := press(t, second, "ctrl+n")
	third = typeInto(t, third, "SELECT 3")

	closed := answerClose(t, third)
	if len(closed.sheets) != 2 {
		t.Fatalf("tabs = %d", len(closed.sheets))
	}
	if closed.statement() != "SELECT 2" {
		t.Errorf("statement = %q, closing the last of three lands on the second",
			closed.statement())
	}
	if strings.Contains(plain(closed.content()), "SELECT 3") {
		t.Error("and what was closed is gone")
	}
}

// The tab strip costs the editor two rows and no more, and the panes keep their
// floors at every size the program is drawn at.
func TestTheTabStripCostsTwoRows(t *testing.T) {
	for _, size := range []struct {
		name          string
		width, height int
	}{
		{"small", 80, 24},
		{"wide", 120, 36},
		{"tiny", 60, 11},
	} {
		t.Run(size.name, func(t *testing.T) {
			m := workbench(t)
			m.width, m.height = size.width, size.height
			want := max(ui.BodyHeight(size.height)-(ui.TabStripRows-1), 1)
			if m.workbenchHeight() != want {
				t.Errorf("workbench = %d, want %d", m.workbenchHeight(), want)
			}
			if m.editorRows() < minEditorRows {
				t.Errorf("editor = %d, below its floor", m.editorRows())
			}
			if m.resultsHeight() < minResultsRows {
				t.Errorf("results = %d, below its floor", m.resultsHeight())
			}
		})
	}
}

// A tab is named after the first table of this database its statement mentions,
// and after its place in the list when it mentions none.
func TestATabIsNamedAfterWhatItReads(t *testing.T) {
	for _, want := range []struct {
		name      string
		statement string
		label     string
	}{
		{"nothing typed", "", "query 1"},
		{"a table", "SELECT * FROM users", "users"},
		{"no table of ours", "SELECT 1", "query 1"},
	} {
		t.Run(want.name, func(t *testing.T) {
			typed := typeInto(t, workbench(t), want.statement)
			if got := typed.label(typed.worksheet, 0); got != want.label {
				t.Errorf("label = %q, want %q", got, want.label)
			}
		})
	}
}

// A tab that is still running says so, in the strip and in the command list.
func TestARunningTabSaysSoWhereverItIsNamed(t *testing.T) {
	conn := healthy()
	conn.holdQuery = make(chan struct{})
	defer close(conn.holdQuery)

	m := loadedWith(t, conn, workspaceWith(t))
	editing, _ := press(t, m, "e")
	editing = typeInto(t, editing, "SELECT 1")
	running, _ := press(t, editing, "ctrl+r")

	if !strings.Contains(plain(running.tabBar(80)), "•") {
		t.Errorf("the strip must mark a tab that is waiting:\n%s", plain(running.tabBar(80)))
	}
	found := false
	for _, entry := range running.commands() {
		if entry.hint == "still running" {
			found = true
		}
	}
	if !found {
		t.Error("and so must the command that reaches it")
	}
}

// Every tab is a command, so a tab is reached by name and not only by walking.
func TestEveryTabIsACommand(t *testing.T) {
	m := workbench(t)
	second, _ := press(t, m, "ctrl+n")
	second = typeInto(t, second, "SELECT * FROM users")

	entries := second.commands()
	if len(entries) != 2 {
		t.Fatalf("commands = %d, one per tab", len(entries))
	}
	if !strings.Contains(entries[1].title, "users") {
		t.Errorf("title = %q, a tab is named in the list the way it is on screen",
			entries[1].title)
	}
	back, _ := second.Update(entries[0].msg)
	if back.(Model).sheet != 0 {
		t.Error("and running the command moves to it")
	}
}

// The commands reach the editor from anywhere, because a tab is a thing on the
// editor screen and a command about it says so.
func TestATabCommandOpensTheEditor(t *testing.T) {
	m := loaded(t, healthy())
	if m.view != viewDashboard {
		t.Fatalf("view = %v", m.view)
	}
	opened, _ := m.Update(newSheetMsg{})
	if opened.(Model).view != viewQuery || len(opened.(Model).sheets) != 2 {
		t.Errorf("view = %v with %d tabs", opened.(Model).view, len(opened.(Model).sheets))
	}
	closed, _ := opened.(Model).Update(closeSheetMsg{})
	if len(closed.(Model).sheets) != 1 {
		t.Errorf("tabs = %d", len(closed.(Model).sheets))
	}
	walked, _ := closed.(Model).Update(walkSheetMsg{step: 1})
	if walked.(Model).view != viewQuery {
		t.Errorf("view = %v", walked.(Model).view)
	}
}

// Clicking a tab moves to it, and clicking anywhere else does not.
func TestClickingATabMovesToIt(t *testing.T) {
	m := workbench(t)
	m.width, m.height = 100, 28
	second, _ := press(t, m, "ctrl+n")
	if second.sheet != 1 {
		t.Fatalf("sheet = %d", second.sheet)
	}
	clicked, _ := second.Update(click(ui.Gutter+1, ui.TabTop))
	if clicked.(Model).sheet != 0 {
		t.Errorf("sheet = %d, a click on the first tab moves to it", clicked.(Model).sheet)
	}
	for _, miss := range []struct {
		name string
		x, y int
	}{
		{"below the strip", ui.Gutter + 1, ui.BodyTop(true)},
		{"past the last tab", 90, ui.TabTop},
		{"left of the first", 0, ui.TabTop},
	} {
		t.Run(miss.name, func(t *testing.T) {
			missed, _ := second.Update(click(miss.x, miss.y))
			if missed.(Model).sheet != 1 {
				t.Errorf("sheet = %d, that click was not on a tab", missed.(Model).sheet)
			}
		})
	}
}

// A click while a dialog is open belongs to the dialog, not to what is behind
// it.
func TestAClickBehindADialogIsIgnored(t *testing.T) {
	m := workbench(t)
	m.width, m.height = 100, 28
	second, _ := press(t, m, "ctrl+n")
	asked, _ := press(t, second, "ctrl+c")
	if asked.modal == nil {
		t.Fatal("ctrl+c must ask")
	}
	clicked, _ := asked.Update(click(ui.Gutter+1, ui.TabTop))
	if clicked.(Model).sheet != 1 {
		t.Error("a click behind a dialog must not reach the tabs")
	}
}

// Only the left button does anything.
func TestOnlyTheLeftButtonClicks(t *testing.T) {
	m := workbench(t)
	m.width, m.height = 100, 28
	second, _ := press(t, m, "ctrl+n")
	right := tea.MouseClickMsg{X: ui.Gutter + 1, Y: ui.TabTop, Button: tea.MouseRight}
	clicked, _ := second.Update(right)
	if clicked.(Model).sheet != 1 {
		t.Error("the right button is not how a tab is chosen")
	}
}

func click(x, y int) tea.MouseClickMsg {
	return tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft}
}

// A digit with a modifier on it reaches a tab by its place, whichever of the
// two modifiers the terminal can send.
func TestADigitReachesATabByItsPlace(t *testing.T) {
	for _, want := range []struct {
		name string
		key  string
		on   int
	}{
		{"ctrl and a digit", "ctrl+1", 0},
		{"alt and a digit", "alt+1", 0},
		{"the third tab", "ctrl+3", 2},
		{"a tab that is not there", "ctrl+9", 2},
		{"a digit on its own", "1", 2},
	} {
		t.Run(want.name, func(t *testing.T) {
			m := workbench(t)
			two, _ := press(t, m, "ctrl+n")
			three, _ := press(t, two, "ctrl+n")
			if three.sheet != 2 {
				t.Fatalf("sheet = %d", three.sheet)
			}
			jumped, _ := press(t, three, want.key)
			if jumped.sheet != want.on {
				t.Errorf("sheet = %d, want %d", jumped.sheet, want.on)
			}
		})
	}
}

// The strip prints the key that reaches each tab, and stops printing one once
// there are no digits left.
func TestTheStripPrintsTheKeyThatReachesEachTab(t *testing.T) {
	if first := jumpLabel(0); !strings.Contains(first, "1") {
		t.Errorf("label = %q, the first tab is reached by the first digit", first)
	}
	if past := jumpLabel(maxJumpTabs); past != "10" {
		t.Errorf("label = %q, past the ninth there is no key left to print", past)
	}
}

// The wheel moves whatever the pointer is over, because the editor screen is
// two lists side by side and moving the body would move neither.
func TestTheWheelMovesWhateverIsUnderIt(t *testing.T) {
	m := workbench(t)
	m.width, m.height = 100, 28
	m.sidebar = m.sidebar.withTables(m.tables, m.fields)
	if len(m.sidebar.rows) < 2 {
		t.Fatalf("rows = %d, the tree needs something to scroll", len(m.sidebar.rows))
	}

	down, _ := m.wheeled(wheel(ui.Gutter+1, tea.MouseWheelDown))
	if down.(Model).sidebar.cursor == m.sidebar.cursor {
		t.Error("the wheel over the tree must move the tree")
	}
	back, _ := down.(Model).wheeled(wheel(ui.Gutter+1, tea.MouseWheelUp))
	if back.(Model).sidebar.cursor != 0 {
		t.Errorf("cursor = %d, back up is back to the top", back.(Model).sidebar.cursor)
	}
	held, _ := back.(Model).wheeled(wheel(ui.Gutter+1, tea.MouseWheelUp))
	if held.(Model).sidebar.cursor != 0 {
		t.Error("and the top is where it stops, rather than wrapping to the end")
	}
}

// Over the editor side the wheel moves the result rather than the tree.
func TestTheWheelOverTheResultMovesTheResult(t *testing.T) {
	m := typeInto(t, workbench(t), "SELECT 1")
	m.width, m.height = 100, 28
	ran, cmd := press(t, m, "ctrl+r")
	shown := settle(t, ran, cmd)
	shown.sidebar = shown.sidebar.withTables(shown.tables, shown.fields)
	before := shown.sidebar.cursor

	rolled, _ := shown.wheeled(wheel(80, tea.MouseWheelDown))
	if rolled.(Model).sidebar.cursor != before {
		t.Error("the wheel over the editor must leave the tree alone")
	}
}

func wheel(x int, button tea.MouseButton) tea.MouseWheelMsg {
	return tea.MouseWheelMsg{X: x, Y: ui.BodyTop(true), Button: button}
}

// A statement left running in one tab lands in that tab, not in whichever tab
// happens to be in front when it answers.
func TestAResultLandsInTheTabThatAskedForIt(t *testing.T) {
	conn := healthy()
	conn.holdQuery = make(chan struct{})
	defer close(conn.holdQuery)

	m := loadedWith(t, conn, workspaceWith(t))
	editing, _ := press(t, m, "e")
	first := typeInto(t, editing, "SELECT 1")
	running, _ := press(t, first, "ctrl+r")
	waiting := running.token

	elsewhere, _ := press(t, running, "ctrl+n")
	if elsewhere.sheet != 1 || elsewhere.results.present {
		t.Fatalf("the new tab is empty: sheet %d", elsewhere.sheet)
	}

	landed, _ := elsewhere.Update(queriedMsg{
		statement: "SELECT 1", columns: []string{"n"},
		rows: [][]any{{int64(1)}}, token: waiting,
	})
	arrived := landed.(Model)
	if arrived.results.present {
		t.Error("the tab in front asked for nothing and must be given nothing")
	}
	if !arrived.sheets[0].results.present || arrived.sheets[0].inflight {
		t.Error("and the tab that asked must have it")
	}

	back, _ := press(t, arrived, "ctrl+1")
	if !back.results.present {
		t.Error("going back to it finds the result waiting")
	}
}

// A result belonging to no tab at all is dropped rather than drawn somewhere.
func TestAResultForNoTabIsDropped(t *testing.T) {
	m := workbench(t)
	dropped, _ := m.Update(queriedMsg{
		statement: "SELECT 1", columns: []string{"n"},
		rows: [][]any{{int64(1)}}, token: 999,
	})
	if dropped.(Model).results.present {
		t.Error("a result nobody is waiting for must not be drawn")
	}
}

// The command list says what each tab is holding.
func TestACommandSaysWhatItsTabIsHolding(t *testing.T) {
	for _, want := range []struct {
		name  string
		sheet worksheet
		hint  string
	}{
		{"nothing yet", worksheet{}, "nothing has run yet"},
		{"a table", worksheet{kind: sheetTable}, "table"},
		{"rows", worksheet{results: results{present: true, rows: [][]any{{1}, {2}}}}, "2 rows"},
		{"a failure", worksheet{results: results{present: true, failure: "boom"}}, "failed"},
		{"still going", worksheet{inflight: true}, "still running"},
	} {
		t.Run(want.name, func(t *testing.T) {
			if got := workbench(t).hint(want.sheet); got != want.hint {
				t.Errorf("hint = %q, want %q", got, want.hint)
			}
		})
	}
}

// Closing a tab in front of the one being worked in leaves the right one in
// front.
func TestClosingATabBeforeThisOneKeepsTheRightOne(t *testing.T) {
	m := typeInto(t, workbench(t), "SELECT 1")
	second, _ := press(t, m, "ctrl+n")
	second = typeInto(t, second, "SELECT 2")
	third, _ := press(t, second, "ctrl+n")
	third = typeInto(t, third, "SELECT 3")

	closed := third.closeSheet(0)
	if len(closed.sheets) != 2 {
		t.Fatalf("tabs = %d", len(closed.sheets))
	}
	if closed.statement() != "SELECT 2" {
		t.Errorf("statement = %q, closing the first leaves the second in front",
			closed.statement())
	}
}

// A tab that is not there is not opened, closed or moved to.
func TestATabThatIsNotThereIsLeftAlone(t *testing.T) {
	m := workbench(t)
	if m.onSheet(9).sheet != 0 || m.onSheet(-1).sheet != 0 {
		t.Error("there is no ninth tab to move to")
	}
	if len(m.closeSheet(9).sheets) != 1 {
		t.Error("nor a ninth to close")
	}
	stray := m
	stray.sheet = 9
	if len(stray.stow().sheets) != 1 {
		t.Error("and nothing is written back for one")
	}
}

// answerClose presses the key that closes a tab and says yes to the question it
// raises.
func answerClose(t *testing.T, m Model) Model {
	t.Helper()
	asked, _ := press(t, m, "ctrl+w")
	if asked.modal == nil {
		t.Fatal("closing a tab must ask first")
	}
	answered, cmd := press(t, asked, "enter")
	if cmd == nil {
		t.Fatal("enter must answer it")
	}
	return settle(t, answered, cmd)
}

// Closing a tab asks first, and says what closing it costs.
func TestClosingATabAsksFirst(t *testing.T) {
	for _, want := range []struct {
		name      string
		statement string
		ran       bool
		said      string
	}{
		{"an empty tab", "", false, "there is nothing in this tab"},
		{"a statement", "SELECT 1", false, "the statement below is not kept"},
		{"a result", "SELECT 1", true, "rows it returned are not kept"},
	} {
		t.Run(want.name, func(t *testing.T) {
			m := typeInto(t, workbench(t), want.statement)
			m.width, m.height = 110, 34
			if want.ran {
				ran, cmd := press(t, m, "ctrl+r")
				m = settle(t, ran, cmd)
			}
			asked, _ := press(t, m, "ctrl+w")
			if asked.modal == nil {
				t.Fatal("closing a tab must ask")
			}
			said := plain(asked.content())
			if !strings.Contains(said, want.said) {
				t.Errorf("the question must say %q:\n%s", want.said, said)
			}
			left, _ := press(t, asked, "esc")
			if left.modal != nil || left.statement() != want.statement {
				t.Error("saying no must leave the tab alone")
			}
		})
	}
}

// A tab with a statement still running says what closing it gives up on, and
// makes that something to tick rather than something to press through.
func TestClosingARunningTabSaysWhatItGivesUp(t *testing.T) {
	conn := healthy()
	conn.holdQuery = make(chan struct{})
	defer close(conn.holdQuery)

	m := loadedWith(t, conn, workspaceWith(t))
	m.width, m.height = 110, 34
	editing, _ := press(t, m, "e")
	typed := typeInto(t, editing, "SELECT 1")
	running, _ := press(t, typed, "ctrl+r")

	asked, _ := press(t, running, "ctrl+w")
	if asked.modal == nil {
		t.Fatal("it must ask")
	}
	said := plain(asked.content())
	if !strings.Contains(said, "still running") {
		t.Errorf("the question must say the statement is out:\n%s", said)
	}
	if !strings.Contains(said, "give up on it") {
		t.Errorf("and make it something to tick:\n%s", said)
	}
	held, _ := press(t, asked, "enter")
	if held.modal == nil {
		t.Error("enter with the box unticked must not close it")
	}
}
