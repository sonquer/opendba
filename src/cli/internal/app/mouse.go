package app

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sonquer/opendba/src/cli/internal/export"
	"github.com/sonquer/opendba/src/cli/internal/ui"
)

// tabAt says which tab a click landed on.
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

// clicked is the mouse doing what a command does.
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
	if m.inEditor(mouse.X, mouse.Y) {
		return m.caretAt(mouse.X, mouse.Y)
	}
	if at, ok := m.readingAt(mouse.X, mouse.Y); ok {
		m.reading = at
		return m.readingPage()
	}
	if hit, ok := m.hintAt(mouse.X, mouse.Y); ok {
		return m.key(tea.KeyPressMsg{Code: hit.code, Mod: hit.mod, Text: hit.text})
	}
	return m, nil
}

// inEditor says whether a click landed in the statement being written.
func (m Model) inEditor(x, y int) bool {
	if m.view != viewQuery || m.zoomed || !m.overResults(x) {
		return false
	}
	top := ui.BodyTop(true)
	return y >= top && y < top+m.editorRows()
}

// caretAt puts the cursor where the pointer is.
func (m Model) caretAt(x, y int) (tea.Model, tea.Cmd) {
	m.focus = focusEditor
	cmd := m.editor.Focus()
	left, top := m.editorOrigin()
	wanted := y - top + m.editor.ScrollYOffset()
	for range abs(wanted - m.editor.Line()) {
		if wanted > m.editor.Line() {
			m.editor.CursorDown()
			continue
		}
		m.editor.CursorUp()
	}
	m.editor.SetCursorColumn(max(x-left-editorGutter, 0))
	return m, cmd
}

// readingAt says which reading of the dashboard a click landed on.
func (m Model) readingAt(x, y int) (int, bool) {
	if m.view != viewDashboard || m.onSessions || m.wizard != nil {
		return 0, false
	}
	readings := m.readings(every)
	if len(readings) == 0 {
		return 0, false
	}
	lines := strings.Split(ansi.Strip(m.body()), "\n")
	at := y - ui.BodyTop(false) + m.offset
	if at < 0 || at >= len(lines) {
		return 0, false
	}
	said := side4Dashboard(lines[at], x-ui.Gutter, ui.FrameWidth(m.width))
	for i, reading := range readings {
		if named4Reading(said, reading.Label) {
			return i, true
		}
	}
	return 0, false
}

// side4Dashboard is the half of a drawn line the pointer is in, which is which
// column of readings it is over.
func side4Dashboard(line string, column, width int) string {
	if width < twoColumns {
		return bare4Reading(line)
	}
	half := width / 2
	runes := []rune(line)
	from, to := 0, min(half, len(runes))
	if column >= half {
		from, to = min(half, len(runes)), len(runes)
	}
	return bare4Reading(string(runes[from:to]))
}

// bare4Reading takes the cursor marker off the front of a line, so the reading
// under the cursor is named the same way as every other one.
func bare4Reading(line string) string {
	return strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(line), "▌"))
}

// named4Reading reports whether a drawn line is the one this reading is on.
func named4Reading(said, label string) bool {
	if label == "" || !strings.HasPrefix(said, label) {
		return false
	}
	rest := said[len(label):]
	return rest == "" || strings.HasPrefix(rest, " ")
}

// hit is a key the footer offered and somebody pressed with the mouse.
type hit struct {
	code rune
	mod  tea.KeyMod
	text string
}

// hintAt says which of the keys in the footer a click landed on.
func (m Model) hintAt(x, y int) (hit, bool) {
	if y != ui.FooterRow(m.height, m.view == viewQuery && !m.zoomed) {
		return hit{}, false
	}
	at := ui.Gutter
	separator := lipgloss.Width(
		m.help.Styles.ShortSeparator.Inline(true).Render(m.help.ShortSeparator))
	for _, binding := range m.footerKeys(ui.FrameWidth(m.width)) {
		width := lipgloss.Width(m.help.ShortHelpView([]key.Binding{binding}))
		if x >= at && x < at+width {
			return pressing(binding)
		}
		at += width + separator
	}
	return hit{}, false
}

// pressing turns a binding back into the key that would have been pressed. Only
// the first key a binding answers to is used: it is the one the footer drew.
func pressing(binding key.Binding) (hit, bool) {
	keys := binding.Keys()
	if len(keys) == 0 {
		return hit{}, false
	}
	return pressed4Name(keys[0])
}

// pressed4Name turns the name of a key back into the key itself, which is what a
// binding holds and what the program answers to.
func pressed4Name(name string) (hit, bool) {
	codes := map[string]rune{
		"enter": tea.KeyEnter, "esc": tea.KeyEscape, "tab": tea.KeyTab,
		"backspace": tea.KeyBackspace, "up": tea.KeyUp, "down": tea.KeyDown,
		"left": tea.KeyLeft, "right": tea.KeyRight, "home": tea.KeyHome,
		"end": tea.KeyEnd, "pgup": tea.KeyPgUp, "pgdown": tea.KeyPgDown,
		"space": tea.KeySpace, "f5": tea.KeyF5, "f6": tea.KeyF6,
	}
	modifiers := map[string]tea.KeyMod{
		"ctrl": tea.ModCtrl, "alt": tea.ModAlt,
		"shift": tea.ModShift, "super": tea.ModSuper,
	}
	parts := strings.Split(name, "+")
	pressed := hit{}
	for _, part := range parts[:len(parts)-1] {
		mod, ok := modifiers[part]
		if !ok {
			return hit{}, false
		}
		pressed.mod |= mod
	}
	last := parts[len(parts)-1]
	if code, ok := codes[last]; ok {
		pressed.code = code
		return pressed, true
	}
	if len([]rune(last)) != 1 {
		return hit{}, false
	}
	pressed.code = []rune(last)[0]
	if pressed.mod == 0 {
		pressed.text = last
	}
	return pressed, true
}

func abs(value int) int {
	if value < 0 {
		return -value
	}
	return value
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

// pickedInTree puts the cursor on the row that was clicked, and opens it when it
// was already there.
func (m Model) pickedInTree(at int) (tea.Model, tea.Cmd) {
	if m.focus == focusSidebar && m.sidebar.cursor == at {
		return m.openedInSidebar()
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

// released ends a selection by putting it on the clipboard.
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
		m.notify(ui.Plural(len(rows), "row", "rows")+" are on the clipboard"))
}

// rolled4Query scrolls whatever the pointer is over.
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

// tookMouse turns the mouse over to the terminal, or takes it back.
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
