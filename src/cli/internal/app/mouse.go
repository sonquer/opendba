package app

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/sonquer/tui4db/src/cli/internal/export"
	"github.com/sonquer/tui4db/src/cli/internal/ui"
)

// tabAt says which tab a click landed on. The strip is drawn from the model, so
// where every tab sits is worked out from the model too rather than read back
// off the screen: what is drawn and what answers a click cannot then disagree.
func (m Model) tabAt(x, y int) (int, bool) {
	if m.strip() == "" || y < ui.TabTop || y >= ui.TabTop+ui.TabRows {
		return 0, false
	}
	at := ui.Gutter
	for i := range m.sheets {
		sheet := m.sheets[i]
		if i == m.sheet {
			sheet = m.worksheet
		}
		width := lipgloss.Width(m.tab(sheet, i))
		if x >= at && x < at+width {
			return i, true
		}
		at += width
	}
	return 0, false
}

// clicked is the mouse doing what a command does. Nothing here is the only way
// to reach anything: every tab is in the command list and on a key as well, and
// a terminal that reports no mouse loses nothing.
func (m Model) clicked(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	if !m.mouse || msg.Button != tea.MouseLeft {
		return m, nil
	}
	if m.page != nil || m.modal != nil || m.chooser != nil || m.palette != nil {
		return m, nil
	}
	mouse := msg.Mouse()
	if index, ok := m.tabAt(mouse.X, mouse.Y); ok {
		return focused(m.onSheet(index))
	}
	if m.onSplit(mouse.X, mouse.Y) {
		m.dragging = true
		return m, nil
	}
	if at, ok := m.rowAt(mouse.X, mouse.Y); ok {
		m.focus = focusResults
		m.results = m.results.startPicking(at)
		return m, nil
	}
	if at, ok := m.treeAt(mouse.X, mouse.Y); ok {
		return m.pickedInTree(at)
	}
	return m, nil
}

// treeAt says which row of the schema a click landed on.
func (m Model) treeAt(x, y int) (int, bool) {
	if m.view != viewQuery || m.zoomed || m.sidebar.hidden || m.overResults(x) {
		return 0, false
	}
	width := ui.FrameWidth(m.width)
	side := m.sidebar.width(width)
	return m.sidebar.rowAt(side, m.workbenchHeight(),
		y-ui.BodyTop(true), m.focus == focusSidebar)
}

// pickedInTree puts the cursor on the row that was clicked, and opens it when
// it was already there. Two clicks rather than a double click: telling one
// double click from two single ones needs a clock in the update loop, and a
// clock there makes every frame of this program depend on when it was drawn.
func (m Model) pickedInTree(at int) (tea.Model, tea.Cmd) {
	if m.focus == focusSidebar && m.sidebar.cursor == at {
		return m.openTable()
	}
	m.focus = focusSidebar
	m.sidebar.cursor = at
	return m, nil
}

// rowAt says which row of the result a click landed on.
func (m Model) rowAt(x, y int) (int, bool) {
	if m.view != viewQuery || !m.overResults(x) || !m.results.present {
		return 0, false
	}
	return m.results.rowAt(y - m.resultsTop())
}

// resultsTop is the row the result pane starts on, which is everything the
// editor pane draws above it.
func (m Model) resultsTop() int {
	above := m.editorRows() + 1
	if m.verdict(m.paneWidth()) != "" {
		above += 2
	}
	if m.zoomed {
		return ui.BodyTop(false)
	}
	return ui.BodyTop(true) + above
}

// onSplit says whether the pointer is on the line between the statement and its
// result, which is the line that can be dragged to give one of them more room.
func (m Model) onSplit(x, y int) bool {
	if m.view != viewQuery || m.zoomed || !m.overResults(x) {
		return false
	}
	return y == ui.BodyTop(true)+m.editorRows()
}

// dragged carries a selection or the split with the pointer. Nothing is copied
// yet: what is wanted is not known until the button comes back up.
func (m Model) dragged(msg tea.MouseMotionMsg) (tea.Model, tea.Cmd) {
	if !m.mouse {
		return m, nil
	}
	if m.dragging {
		return m.resizedBy(msg.Mouse().Y)
	}
	if m.results.anchor < 0 {
		return m, nil
	}
	mouse := msg.Mouse()
	at, ok := m.rowAt(mouse.X, mouse.Y)
	if !ok {
		return m, nil
	}
	m.results = m.results.pickTo(at)
	return m, nil
}

// released ends a selection by putting it on the clipboard. Selecting something
// and then having to reach for another key to copy it is the part people forget
// to do, and a selection nobody copied was a selection for nothing.
func (m Model) dropped(tea.MouseReleaseMsg) (tea.Model, tea.Cmd) {
	if m.dragging {
		m.dragging = false
		return m, nil
	}
	if !m.mouse || m.results.anchor < 0 {
		return m, nil
	}
	rows, ok := m.results.picked()
	m.results = m.results.stopPicking()
	if !ok {
		return m, nil
	}
	if len(rows) == 1 {
		return m.copiedCell()
	}
	text, err := text4Clipboard(m.results.columns, rows, export.FormatCSV)
	if err != nil {
		return m, m.notify("that could not be written: " + err.Error())
	}
	return m, tea.Batch(tea.SetClipboard(text),
		m.notify("copied "+ui.Plural(len(rows), "row", "rows")))
}

// rolled4Query scrolls whatever the pointer is over. The editor screen is two
// lists side by side inside one body, so a wheel that moved the body would move
// neither of them: what is under the pointer is the only sensible answer to
// what a notch of the wheel meant.
func (m Model) rolled4Query(x, step int) Model {
	if m.zoomed || m.overResults(x) {
		m.results = m.results.roll(step)
		return m
	}
	m.sidebar = m.sidebar.roll(step)
	return m
}

// overResults says whether the pointer is on the editor side of the divider.
// A hidden schema tree leaves the whole width to the editor.
func (m Model) overResults(x int) bool {
	if m.sidebar.hidden {
		return true
	}
	width := ui.FrameWidth(m.width)
	return x >= ui.Gutter+m.sidebar.width(width)
}

type mouseMsg struct{}

// tookMouse turns the mouse over to the terminal, or takes it back. While this
// program is reading the mouse the terminal cannot select text with it, which
// is the price of clicking anything and is not always worth paying.
func (m Model) tookMouse() (tea.Model, tea.Cmd) {
	m.mouse = !m.mouse
	if m.mouse {
		return m, m.notify("the mouse is being read again")
	}
	return m, m.notify("the terminal has the mouse back, so it can select text")
}

// resizedBy puts the line between the statement and its result where the
// pointer is, within the room each of them needs.
func (m Model) resizedBy(y int) (tea.Model, tea.Cmd) {
	wanted := y - ui.BodyTop(true)
	room := max(m.workbenchHeight()-6, minEditorRows)
	m.split = clamp(wanted, minEditorRows, room)
	m.editor.SetHeight(m.editorRows())
	m.results = m.results.resize(m.paneWidth(), m.resultsHeight())
	return m, nil
}
