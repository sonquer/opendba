package app

import (
	"context"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/sonquer/opendba/src/cli/internal/ui"
)

// sheetKind says what a tab was opened for.
type sheetKind int

const (
	sheetQuery sheetKind = iota
	sheetTable
	sheetFile
)

// worksheet is one tab: a statement, what it returned, and how the pane was
// arranged while it was being written.
type worksheet struct {
	kind    sheetKind
	title   string
	editor  textarea.Model
	results results
	suggest completion
	split   int
	zoomed  bool

	// began, stopQuery and token are what a statement that is still running is made
	// visible, interruptible and identifiable by.
	began     time.Time
	stopQuery context.CancelFunc
	inflight  bool
	token     int

	// file is the name this tab was read from and ctrl+s writes back to, and saved
	// is the statement as the file holds it.
	file  string
	saved string

	// on is the connection this tab is worked through, which is not always the one
	// the tab beside it is worked through.
	on sessionID
}

// finished releases the context of a run that has come back, whichever tab it
// belonged to. Nothing ever called the cancel of a run that arrived, so every
// successful statement left one behind.
func (s worksheet) finished() worksheet {
	if s.stopQuery != nil {
		s.stopQuery()
	}
	s.stopQuery, s.inflight = nil, false
	return s
}

// dirty says whether the tab holds something its file does not.
func (s worksheet) dirty() bool {
	return s.file != "" && s.editor.Value() != s.saved
}

func newWorksheet(theme *ui.Theme, kind sheetKind, title string, on sessionID) worksheet {
	return worksheet{
		kind:    kind,
		title:   title,
		editor:  newEditor(theme),
		suggest: completion{theme: theme},
		on:      on,
	}
}

// stow writes the tab being worked in, and the connection it is being worked
// through, back into the lists they came from.
func (m Model) stow() Model {
	m = m.stowLink()
	if m.sheet < 0 || m.sheet >= len(m.sheets) {
		return m
	}
	sheets := make([]worksheet, len(m.sheets))
	copy(sheets, m.sheets)
	sheets[m.sheet] = m.worksheet
	m.sheets = sheets
	return m
}

// eachSheet is every tab, with the one being worked in as it is now rather than
// as it was last stowed.
func (m Model) eachSheet() []worksheet {
	sheets := make([]worksheet, len(m.sheets))
	copy(sheets, m.sheets)
	if m.sheet >= 0 && m.sheet < len(sheets) {
		sheets[m.sheet] = m.worksheet
	}
	return sheets
}

// sized is the model as it would be with another tab in front, which is what a
// result arriving for a tab that is not in front has to be measured against.
func (m Model) sized(sheet worksheet) Model {
	m.worksheet = sheet
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
	m = m.through(m.on)
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
	m = m.through(sheet.on)
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
	m.sheets[index] = m.sheets[index].finished()
	if len(m.sheets) == 1 {
		fresh := newWorksheet(m.theme, sheetQuery, "", m.link.key())
		m.sheets = []worksheet{fresh}
		m.sheet = 0
		m.worksheet = fresh
		m.editor.SetWidth(m.paneWidth())
		m.editor.SetHeight(m.editorRows())
		return m
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
	m = m.through(m.on)
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

// mentionedFirst is the first table of this database the statement names, in the
// order the statement names them rather than the order the catalogue holds them,
// so a tab is called after what the statement is mostly about.
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
// half the screen.
func (m Model) tabBar(width int) string {
	sheets := m.eachSheet()
	drawn := make([]string, 0, 2*len(sheets))
	mixed := m.mixed4Tabs(sheets)
	previous := sessionID(0)
	for i, sheet := range sheets {
		if mixed && sheet.on != previous {
			drawn = append(drawn, m.run4Tabs(sheet.on))
		}
		previous = sheet.on
		drawn = append(drawn, m.tab(sheet, i))
	}
	return clip4Tabs(lipgloss.JoinHorizontal(lipgloss.Top, drawn...), width)
}

// mixed4Tabs reports whether the tabs are on more than one connection, which is
// when the strip has to say which is which. One connection costs no room at all.
func (m Model) mixed4Tabs(sheets []worksheet) bool {
	for _, sheet := range sheets {
		if sheet.on != sheets[0].on {
			return true
		}
	}
	return false
}

// run4Tabs names the connection the run of tabs beside it is worked through,
// once rather than on every tab, since a name on each is the same name three
// times over.
func (m Model) run4Tabs(on sessionID) string {
	named := m.linked4Sheet(on)
	return m.theme.TabRun(ui.EnvColor(named.session.Connection.Color),
		ui.Truncate(m.label4Link(named), maxTabWidth))
}

// tab is one tab: the key that reaches it, its name, and a dot while its
// statement is still running.
func (m Model) tab(sheet worksheet, index int) string {
	name := ui.Truncate(m.label(sheet, index), maxTabWidth)
	if sheet.dirty() {
		name += " *"
	}
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
// it.
func jumpCap(index int) string {
	if index >= maxJumpTabs {
		return ""
	}
	return ui.Keystroke("ctrl+" + strconv.Itoa(index+1))
}

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
// above it has had its rows.
func (m Model) workbenchHeight() int {
	return max(ui.BodyHeight(m.height)-(ui.TabStripRows-1), 1)
}

// answered puts a result in the tab that asked for it, which is not always the
// tab in front: a statement left running in one tab while another is worked in
// still belongs to the tab it was typed in.
func (m Model) returned(msg queriedMsg) (tea.Model, tea.Cmd) {
	if msg.token == m.token {
		m.worksheet = m.worksheet.finished()
		m.results = newResults(m.theme, msg, m.paneWidth(), m.resultsHeight())
		m.focus = focusEditor
		return m, m.wroteDown(msg, m.on)
	}
	for i := range m.sheets {
		if i == m.sheet || m.sheets[i].token != msg.token {
			continue
		}
		sheets := make([]worksheet, len(m.sheets))
		copy(sheets, m.sheets)
		sheet := sheets[i].finished()
		measured := m.sized(sheet)
		sheet.results = newResults(m.theme, msg, measured.paneWidth(), measured.resultsHeight())
		sheets[i] = sheet
		m.sheets = sheets
		if text := m.about(msg, sheet, i); text != "" {
			return m, tea.Batch(m.wroteDown(msg, sheet.on), m.say(text, msg.err != nil))
		}
		return m, m.wroteDown(msg, sheet.on)
	}
	return m, nil
}

// about is what a result arriving for a tab that is not in front says, or
// nothing when the strip already says it. A failure always says so, because
// never hearing that a statement failed is the whole cost of leaving one
// running; a result that worked is only worth saying away from the editor
// screen, where the strip that marks the tab is not drawn.
func (m Model) about(msg queriedMsg, sheet worksheet, index int) string {
	switch {
	case msg.err != nil:
		return m.label(sheet, index) + ": " + msg.err.Error()
	case m.view == viewQuery:
		return ""
	default:
		return m.label(sheet, index) + " · " + ui.Plural(len(msg.rows), "row", "rows")
	}
}

// focused puts the cursor in the editor of whichever tab is now in front, and
// reads the server behind it the first time a tab on that connection is looked
// at. Coming back to one that has been read reads nothing.
func focused(m Model) (tea.Model, tea.Cmd) {
	m.focus = focusEditor
	if m.read || m.loading {
		return m, m.editor.Focus()
	}
	m.loading = true
	return m, tea.Batch(m.editor.Focus(), m.load(), m.spinner.Tick)
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

// confirmClose asks before a tab goes.
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
	case sheet.dirty():
		return "what is below has not been written to " + sheet.file + " since it was changed"
	case sheet.file != "":
		return sheet.file + " keeps what is below, so closing the tab loses nothing"
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
	sheets := m.eachSheet()
	tabs := make([]command, 0, len(sheets))
	for i, sheet := range sheets {
		tabs = append(tabs, command{
			title: "tab " + strconv.Itoa(i+1) + ", " + m.label(sheet, i),
			key:   jumpCap(i),
			msg:   gotoSheetMsg{index: i},
		})
	}
	return tabs
}

// show4Tabs puts the editor in front.
func (m Model) show4Tabs(cmd tea.Cmd) (tea.Model, tea.Cmd) {
	if m.view == viewQuery {
		return m, cmd
	}
	shown, opening := m.show(viewQuery)
	return shown, tea.Batch(cmd, opening)
}
