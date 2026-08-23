package app

import (
	"context"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/sonquer/tui4db/src/cli/internal/ui"
)

// sheetKind says what a tab was opened for. Both kinds hold a statement and its
// result, because a tab that reads a table without showing the statement it
// sent would be the one place in this program where something runs unseen.
type sheetKind int

const (
	sheetQuery sheetKind = iota
	sheetTable
)

// worksheet is one tab: a statement, what it returned, and how the pane was
// arranged while it was being written. Everything here belongs to the tab it is
// in, which is why it is a type rather than more fields on the model.
type worksheet struct {
	kind    sheetKind
	title   string
	editor  textarea.Model
	results results
	suggest completion
	split   int
	zoomed  bool

	// began, stopQuery and token are what a statement that is still running is
	// made visible, interruptible and identifiable by. The token is stamped
	// from a counter on the model, so a result can find the tab that asked for
	// it however many tabs have been opened since.
	began     time.Time
	stopQuery context.CancelFunc
	inflight  bool
	token     int
}

func newWorksheet(theme *ui.Theme, kind sheetKind, title string) worksheet {
	return worksheet{
		kind:    kind,
		title:   title,
		editor:  newEditor(theme),
		suggest: completion{theme: theme},
	}
}

// stow writes the tab being worked in back into the list of tabs. The model
// embeds the active worksheet, so the entry in sheets for the active tab is
// stale by design; this is the one place that makes it true again, and every
// path that changes which tab is active goes through it.
func (m Model) stow() Model {
	if m.sheet < 0 || m.sheet >= len(m.sheets) {
		return m
	}
	sheets := make([]worksheet, len(m.sheets))
	copy(sheets, m.sheets)
	sheets[m.sheet] = m.worksheet
	m.sheets = sheets
	return m
}

// onSheet makes another tab the one being worked in.
func (m Model) onSheet(index int) Model {
	if index < 0 || index >= len(m.sheets) || index == m.sheet {
		return m
	}
	m = m.stow()
	m.sheet = index
	m.worksheet = m.sheets[index]
	m.editor.SetWidth(m.paneWidth())
	m.editor.SetHeight(m.editorRows())
	m.results = m.results.resize(m.paneWidth(), m.resultsHeight())
	m.focus = focusEditor
	return m
}

// openSheet adds a tab and moves to it.
func (m Model) openSheet(sheet worksheet) Model {
	m = m.stow()
	m.sheets = append(append([]worksheet{}, m.sheets...), sheet)
	m.sheet = len(m.sheets) - 1
	m.worksheet = sheet
	m.editor.SetWidth(m.paneWidth())
	m.editor.SetHeight(m.editorRows())
	m.focus = focusEditor
	return m
}

// closeSheet drops a tab. The last one is emptied rather than removed, because
// an editor screen with no editor on it is a screen with nothing to do.
func (m Model) closeSheet(index int) Model {
	if index < 0 || index >= len(m.sheets) {
		return m
	}
	m = m.stow()
	if len(m.sheets) == 1 {
		fresh := newWorksheet(m.theme, sheetQuery, "")
		m.sheets = []worksheet{fresh}
		m.sheet = 0
		m.worksheet = fresh
		m.editor.SetWidth(m.paneWidth())
		m.editor.SetHeight(m.editorRows())
		return m
	}
	if m.sheets[index].stopQuery != nil {
		m.sheets[index].stopQuery()
	}
	kept := make([]worksheet, 0, len(m.sheets)-1)
	kept = append(kept, m.sheets[:index]...)
	kept = append(kept, m.sheets[index+1:]...)
	m.sheets = kept
	m.sheet = min(max(m.sheet, 0), len(kept)-1)
	if index < m.sheet || m.sheet == index {
		m.sheet = min(index, len(kept)-1)
	}
	m.worksheet = m.sheets[m.sheet]
	m.editor.SetWidth(m.paneWidth())
	m.editor.SetHeight(m.editorRows())
	m.results = m.results.resize(m.paneWidth(), m.resultsHeight())
	return m
}

// walkSheets moves to the tab beside this one, wrapping, which is what every
// program with tabs does at the ends.
func (m Model) walkSheets(step int) Model {
	if len(m.sheets) < 2 {
		return m
	}
	return m.onSheet((m.sheet + step + len(m.sheets)) % len(m.sheets))
}

// label is what the tab says. A table tab is named after its table; a query is
// named after the first table in the catalogue it mentions, and after its place
// in the list when it mentions none, because a tab called SELECT is every tab.
func (m Model) label(sheet worksheet, index int) string {
	if sheet.title != "" {
		return sheet.title
	}
	if named := m.mentionedFirst(sheet.editor.Value()); named != "" {
		return named
	}
	return "query " + strconv.Itoa(index+1)
}

// mentionedFirst is the first table of this database the statement names, in
// the order the statement names them rather than the order the catalogue holds
// them, so a tab is called after what the statement is mostly about.
func (m Model) mentionedFirst(statement string) string {
	lowered := strings.ToLower(statement)
	first, at := "", -1
	for _, table := range m.tables {
		found := strings.Index(lowered, strings.ToLower(table.Name))
		if found < 0 || (at >= 0 && found >= at) {
			continue
		}
		first, at = table.Name, found
	}
	return first
}

// maxTabWidth is how much of a name a tab shows. Past it a tab is mostly a
// truncation mark, and the number in front is what the command names anyway.
const maxTabWidth = 18

// tabBar draws the tabs across the whole width, above the schema tree and the
// editor together, because a tab holds a statement and its result rather than
// half the screen. The one being worked in is a block of colour and the rest
// are not, which is what a tab looks like when it cannot be drawn with corners.
func (m Model) tabBar(width int) string {
	drawn := make([]string, 0, len(m.sheets))
	for i := range m.sheets {
		sheet := m.sheets[i]
		if i == m.sheet {
			sheet = m.worksheet
		}
		drawn = append(drawn, m.tab(sheet, i))
	}
	return clip4Tabs(lipgloss.JoinHorizontal(lipgloss.Top, drawn...), width)
}

// tab is one tab: the key that reaches it, its name, and a dot while its
// statement is still running. The key is on the tab rather than in a list
// somewhere because a tab nobody can get to without counting along is a tab
// nobody uses.
func (m Model) tab(sheet worksheet, index int) string {
	name := ui.Truncate(m.label(sheet, index), maxTabWidth)
	if sheet.inflight {
		name += " •"
	}
	body := m.theme.TabIdle
	if index == m.sheet {
		body = m.theme.Tab
	}
	return m.theme.TabKey.Render(" "+jumpLabel(index)+" ") +
		body.Render("  "+name+"   ")
}

// jumpLabel is the key that reaches a tab, written the way the keyboard prints
// it. Past the ninth there is no key left to print, so the number stands on its
// own and the command list is the way there.
func jumpLabel(index int) string {
	if index >= maxJumpTabs {
		return strconv.Itoa(index + 1)
	}
	return ui.Keystroke("ctrl+" + strconv.Itoa(index+1))
}

// maxJumpTabs is how many tabs get a key of their own, which is as many digits
// as there are.
const maxJumpTabs = 9

// clip4Tabs cuts every row of the strip to the width there is, since a strip
// is more than one row high and the plain clip is about one.
func clip4Tabs(strip string, width int) string {
	rows := strings.Split(strip, "\n")
	for i, row := range rows {
		rows[i] = ui.Clip(row, width)
	}
	return strings.Join(rows, "\n")
}

// workbenchHeight is what the editor screen has to draw in, once the tab strip
// above it has had its rows. One of them was the blank row every screen leaves
// under its header, so the strip costs the editor the rest.
func (m Model) workbenchHeight() int {
	return max(ui.BodyHeight(m.height)-(ui.TabStripRows-1), 1)
}

// answered puts a result in the tab that asked for it, which is not always the
// tab in front: a statement left running in one tab while another is worked in
// still belongs to the tab it was typed in. A result whose token matches no tab
// is the answer to a statement that was given up on, and is dropped.
func (m Model) returned(msg queriedMsg) (tea.Model, tea.Cmd) {
	if msg.token == m.token {
		m.stopQuery, m.inflight = nil, false
		m.results = newResults(m.theme, msg, m.paneWidth(), m.resultsHeight())
		m.focus = focusEditor
		return m, m.wroteDown(msg)
	}
	for i := range m.sheets {
		if i == m.sheet || m.sheets[i].token != msg.token {
			continue
		}
		sheets := make([]worksheet, len(m.sheets))
		copy(sheets, m.sheets)
		sheets[i].stopQuery, sheets[i].inflight = nil, false
		sheets[i].results = newResults(m.theme, msg, m.paneWidth(), m.resultsHeight())
		m.sheets = sheets
		return m, m.wroteDown(msg)
	}
	return m, nil
}

// focused puts the cursor in the editor of whichever tab is now in front. The
// textarea has to be told, because focus lives inside it rather than in the
// model that holds it.
func focused(m Model) (tea.Model, tea.Cmd) {
	m.focus = focusEditor
	return m, m.editor.Focus()
}

// strip is the tab bar, drawn only on the screen that has tabs. Everywhere else
// the row it would take stays the blank row it has always been.
func (m Model) strip() string {
	if m.view != viewQuery || m.zoomed {
		return ""
	}
	return m.tabBar(ui.FrameWidth(m.width))
}

type newSheetMsg struct{}

type closeSheetMsg struct{}

// askCloseMsg raises the question rather than answering it, which is what the
// command in the list does: a command that closed a tab outright would be a
// command somebody runs by mistake looking for something else.
type askCloseMsg struct{}

// confirmClose asks before a tab goes. What it says depends on what is in the
// tab, because "are you sure?" about an empty tab and about a statement
// somebody spent ten minutes on are not the same question, and a dialog that
// asks them the same way is one people learn to dismiss without reading.
func (m Model) confirmClose() (tea.Model, tea.Cmd) {
	sheet := m.worksheet
	name := m.label(sheet, m.sheet)
	dialog := ask(m.theme, "close "+name+"?", m.loss(sheet), closeSheetMsg{})
	dialog.tag = m.theme.Muted.Render(ui.Plural(len(m.sheets), "tab", "tabs") + " open")
	if statement := strings.TrimSpace(sheet.editor.Value()); statement != "" {
		dialog.code = statement
	}
	if sheet.inflight {
		dialog.warning("the statement in this tab is still running, and closing the tab gives up on it",
			"give up on it")
	}
	m.modal = dialog
	return m, nil
}

// loss is what closing this tab costs, said plainly. A tab with nothing in it
// costs nothing and says so rather than inventing a reason to worry.
func (m Model) loss(sheet worksheet) string {
	switch {
	case sheet.inflight:
		return "the result will not arrive, and the statement below is not kept anywhere"
	case strings.TrimSpace(sheet.editor.Value()) == "":
		return "there is nothing in this tab"
	case sheet.results.present:
		return "the statement below and the " +
			ui.Plural(len(sheet.results.rows), "row", "rows") +
			" it returned are not kept anywhere, and closing the tab is the end of them"
	default:
		return "the statement below is not kept anywhere, and closing the tab is the end of it"
	}
}

type walkSheetMsg struct{ step int }

type gotoSheetMsg struct{ index int }

// commands is one command per open tab, so a tab is reached by its name rather
// than by pressing the key that walks the list until the right one is in front.
func (m Model) commands() []command {
	tabs := make([]command, 0, len(m.sheets))
	for i := range m.sheets {
		sheet := m.sheets[i]
		if i == m.sheet {
			sheet = m.worksheet
		}
		tabs = append(tabs, command{
			title: "tab " + strconv.Itoa(i+1) + ", " + m.label(sheet, i),
			hint:  m.hint(sheet),
			msg:   gotoSheetMsg{index: i},
		})
	}
	return tabs
}

// hint is what a tab says about itself in the command list: what it is showing,
// or that it is still waiting for it.
func (m Model) hint(sheet worksheet) string {
	switch {
	case sheet.inflight:
		return "still running"
	case sheet.results.failure != "":
		return "failed"
	case sheet.results.present:
		return ui.Plural(len(sheet.results.rows), "row", "rows")
	case sheet.kind == sheetTable:
		return "table"
	default:
		return "nothing has run yet"
	}
}

// show4Tabs puts the editor in front. A command that opens or moves between
// tabs is a command about the editor, so it says so from wherever it was run
// rather than changing something nobody can see.
func (m Model) show4Tabs(cmd tea.Cmd) (tea.Model, tea.Cmd) {
	if m.view == viewQuery {
		return m, cmd
	}
	shown, opening := m.show(viewQuery)
	return shown, tea.Batch(cmd, opening)
}
