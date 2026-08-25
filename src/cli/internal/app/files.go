package app

import (
	tea "charm.land/bubbletea/v2"

	"github.com/sonquer/opendba/src/cli/internal/sqlfiles"
)

// filesMsg is what the workspace directory holds, or what went wrong reading it.
type filesMsg struct {
	files []sqlfiles.File
	err   error
}

// openedFileMsg is one statement read off disk, on its way into a tab.
type openedFileMsg struct {
	name string
	body string
	err  error
}

// saveFileMsg is the command list asking for what ctrl+s asks for.
type saveFileMsg struct{}

// nameFileMsg is the name typed into the dialog that asks for one.
type nameFileMsg struct{ name string }

// wroteFileMsg says a tab reached its file, and which tab it was: the answer
// arrives after the write, by which time another tab may be the one in front.
type wroteFileMsg struct {
	sheet int
	name  string
	body  string
	err   error
}

// deleteFileMsg is the answer to the dialog that asks before removing one.
type deleteFileMsg struct{ name string }

type deletedFileMsg struct {
	name string
	err  error
}

// root is the directory this connection's statements are kept in.
func (m Model) root() string {
	return sqlfiles.Root(
		m.workspace.Setup().Store.Paths,
		m.settings.Workspace.Root,
		m.session.Connection,
	)
}

func (m Model) readFiles() tea.Cmd {
	root := m.root()
	return func() tea.Msg {
		files, err := sqlfiles.List(root)
		return filesMsg{files: files, err: err}
	}
}

func (m Model) listedFiles(msg filesMsg) (Model, tea.Cmd) {
	if msg.err != nil {
		m.sidebar = m.sidebar.withFiles(nil, "cannot be read")
		return m, m.alarm(msg.err.Error())
	}
	trouble := ""
	if m.root() == "" {
		trouble = "nowhere to keep them"
	}
	m.sidebar = m.sidebar.withFiles(msg.files, trouble)
	return m, nil
}

func (m Model) readFile(name string) tea.Cmd {
	root := m.root()
	return func() tea.Msg {
		body, err := sqlfiles.Read(root, name)
		return openedFileMsg{name: name, body: body, err: err}
	}
}

// openedFile puts a statement in a tab, or moves to the tab it is already in.
func (m Model) openedFile(msg openedFileMsg) (Model, tea.Cmd) {
	if msg.err != nil {
		return m, tea.Batch(m.alarm(msg.err.Error()), m.readFiles())
	}
	stowed := m.stow()
	if at, ok := stowed.sheetOf(msg.name); ok {
		moved := stowed.onSheet(at)
		moved.focus = focusEditor
		return moved, moved.editor.Focus()
	}
	sheet := newWorksheet(m.theme, sheetFile, msg.name, m.link.key())
	sheet.editor.SetValue(msg.body)
	sheet.file = msg.name
	sheet.saved = msg.body
	opened := stowed.openSheet(sheet)
	return opened, opened.editor.Focus()
}

// sheetOf finds the tab a file is already open in.
func (m Model) sheetOf(name string) (int, bool) {
	for i, sheet := range m.sheets {
		if sheet.file == name {
			return i, true
		}
	}
	return 0, false
}

// saveSheet writes the tab to its file, or asks what to call it.
func (m Model) saveSheet() (tea.Model, tea.Cmd) {
	if m.root() == "" {
		return m, m.alarm("there is nowhere to keep sql files")
	}
	if m.file != "" {
		return m, m.writeFile(m.sheet, m.file, m.editor.Value())
	}
	dialog, cmd := askName(m.theme, "save this tab", "the name of the file to write",
		func(typed string) tea.Msg { return nameFileMsg{name: typed} })
	m.modal = dialog
	return m, cmd
}

// saveNamed writes a tab that has never been a file, refusing to write over one
// that is already there.
func (m Model) saveNamed(msg nameFileMsg) (Model, tea.Cmd) {
	name, err := sqlfiles.Named(msg.name)
	if err != nil {
		return m, m.alarm(err.Error())
	}
	root, at, body := m.root(), m.sheet, m.editor.Value()
	return m, func() tea.Msg {
		if _, err := sqlfiles.Create(root, name, body); err != nil {
			return wroteFileMsg{sheet: at, name: name, err: err}
		}
		return wroteFileMsg{sheet: at, name: name, body: body}
	}
}

func (m Model) writeFile(at int, name, body string) tea.Cmd {
	root := m.root()
	return func() tea.Msg {
		if _, err := sqlfiles.Write(root, name, body); err != nil {
			return wroteFileMsg{sheet: at, name: name, err: err}
		}
		return wroteFileMsg{sheet: at, name: name, body: body}
	}
}

// wroteFile marks the tab as saved, whichever tab is in front by now.
func (m Model) wroteFile(msg wroteFileMsg) (Model, tea.Cmd) {
	if msg.err != nil {
		return m, m.alarm(msg.err.Error())
	}
	stowed := m.stow()
	if msg.sheet < 0 || msg.sheet >= len(stowed.sheets) {
		return stowed, tea.Batch(stowed.notify(msg.name+" is written"), stowed.readFiles())
	}
	sheets := make([]worksheet, len(stowed.sheets))
	copy(sheets, stowed.sheets)
	sheets[msg.sheet].kind = sheetFile
	sheets[msg.sheet].title = msg.name
	sheets[msg.sheet].file = msg.name
	sheets[msg.sheet].saved = msg.body
	stowed.sheets = sheets
	if msg.sheet == stowed.sheet {
		stowed.worksheet = sheets[msg.sheet]
	}
	return stowed, tea.Batch(stowed.notify(msg.name+" is written"), stowed.readFiles())
}

// confirmDeleteFile asks before taking a statement away, because nothing else
// in this program removes something somebody wrote.
func (m Model) confirmDeleteFile() (tea.Model, tea.Cmd) {
	name, ok := m.sidebar.file()
	if !ok {
		return m, nil
	}
	m.modal = ask(m.theme, "remove "+name+"?", "the file is deleted from disk",
		deleteFileMsg{name: name})
	return m, nil
}

func (m Model) removeFile(msg deleteFileMsg) tea.Cmd {
	root := m.root()
	return func() tea.Msg {
		return deletedFileMsg{name: msg.name, err: sqlfiles.Remove(root, msg.name)}
	}
}

func (m Model) deletedFile(msg deletedFileMsg) (Model, tea.Cmd) {
	if msg.err != nil {
		return m, tea.Batch(m.alarm(msg.err.Error()), m.readFiles())
	}
	return m, tea.Batch(m.notify(msg.name+" is gone"), m.readFiles())
}
