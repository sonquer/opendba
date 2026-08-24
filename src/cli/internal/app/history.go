package app

import (
	"context"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/sonquer/opendba/src/cli/internal/history"
	"github.com/sonquer/opendba/src/cli/internal/ui"
)

type recalledMsg struct {
	entries []history.Entry
	err     error
}

// recall is the list of statements that have been run.
type recall struct {
	theme   *ui.Theme
	entries []history.Entry
	cursor  int
	term    string
	trouble string
	loading bool
}

func newRecall(theme *ui.Theme) recall { return recall{theme: theme} }

// keeping reports whether there is anywhere to keep what has been run.
func (m Model) keeping() bool { return m.session.History != nil }

// remember writes down what a statement did.
func (m Model) wroteDown(msg queriedMsg) tea.Cmd {
	if !m.keeping() || strings.TrimSpace(msg.statement) == "" {
		return nil
	}
	store := m.session.History
	entry := history.Entry{
		ConnectionID:   m.session.Connection.Name,
		ConnectionName: m.session.Connection.Name,
		Statement:      msg.statement,
		Kind:           string(m.Verdict().Kind),
		Rows:           len(msg.rows),
		Duration:       msg.duration,
	}
	if msg.err != nil {
		entry.Failure = msg.err.Error()
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), rememberTimeout)
		defer cancel()
		if err := store.Record(ctx, entry); err != nil {
			return toastMsg{}
		}
		return nil
	}
}

// rememberTimeout is how long writing one line to a local file is given before
// it is treated as something that is not going to happen.
const rememberTimeout = 5 * time.Second

// readHistory reads what has been run, filtered by whatever has been typed.
func (m Model) readHistory() tea.Cmd {
	if !m.keeping() {
		return nil
	}
	store := m.session.History
	term := m.recall.term
	limit := m.session.Settings.History.Limit
	connection := m.session.Connection.Name
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
		defer cancel()
		if strings.TrimSpace(term) != "" {
			entries, err := store.Search(ctx, term, limit)
			return recalledMsg{entries: entries, err: err}
		}
		entries, err := store.Recent(ctx, connection, limit)
		return recalledMsg{entries: entries, err: err}
	}
}

func (m Model) recalled(msg recalledMsg) (tea.Model, tea.Cmd) {
	m.recall.loading = false
	m.recall.trouble = ""
	if msg.err != nil {
		m.recall.trouble = msg.err.Error()
		return m, nil
	}
	m.recall.entries = msg.entries
	m.recall.cursor = min(max(m.recall.cursor, 0), max(len(msg.entries)-1, 0))
	return m, nil
}

func (r recall) move(step int) recall {
	if len(r.entries) == 0 {
		return r
	}
	r.cursor = clamp(r.cursor+step, 0, len(r.entries)-1)
	return r
}

func (r recall) selected() (history.Entry, bool) {
	if r.cursor < 0 || r.cursor >= len(r.entries) {
		return history.Entry{}, false
	}
	return r.entries[r.cursor], true
}

func (m Model) historyKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Back):
		if m.recall.term != "" {
			m.recall.term = ""
			return m, m.readHistory()
		}
		return m.show(viewDashboard)
	case key.Matches(msg, m.keys.Above):
		m.recall = m.recall.move(-1)
		return m, nil
	case key.Matches(msg, m.keys.Below):
		m.recall = m.recall.move(1)
		return m, nil
	case key.Matches(msg, m.keys.Choose):
		return m.reopen()
	case key.Matches(msg, m.keys.Pick):
		return m.favourite()
	case msg.String() == "backspace":
		if m.recall.term == "" {
			return m, nil
		}
		m.recall.term = m.recall.term[:len(m.recall.term)-1]
		return m, m.readHistory()
	}
	if typed := msg.String(); len(typed) == 1 {
		m.recall.term += typed
		return m, m.readHistory()
	}
	return m, nil
}

// reopen puts a statement back in a tab of its own rather than over whatever is
// being written, because coming back for something you ran before is not a
// reason to lose what you are writing now.
func (m Model) reopen() (tea.Model, tea.Cmd) {
	entry, ok := m.recall.selected()
	if !ok || entry.Statement == history.Redacted() {
		return m, m.notify("this statement was not kept, only that it ran")
	}
	sheet := newWorksheet(m.theme, sheetQuery, "")
	sheet.editor.SetValue(entry.Statement)
	opened := m.openSheet(sheet)
	shown, cmd := opened.show(viewQuery)
	return shown, tea.Batch(cmd, opened.editor.Focus())
}

// favourite keeps a statement past the point where the rest are trimmed away.
func (m Model) favourite() (tea.Model, tea.Cmd) {
	entry, ok := m.recall.selected()
	if !ok || !m.keeping() {
		return m, nil
	}
	store := m.session.History
	wanted := !entry.Favorite
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), rememberTimeout)
		defer cancel()
		if err := store.SetFavorite(ctx, entry.ID, wanted); err != nil {
			return recalledMsg{err: err}
		}
		return nil
	}
}

func (m Model) historyBody() string {
	width := ui.FrameWidth(m.width)
	if !m.keeping() {
		return m.theme.Screen("history", m.theme.Muted.Render("switched off"), width) +
			"\n\n" + m.theme.Muted.Render(
			"nothing is being kept. `history.enabled` in settings.toml turns it on.")
	}
	lines := []string{
		m.theme.Screen("history", m.theme.Muted.Render(m.recall.tag()), width),
		"",
		m.recall.search(width),
		"",
	}
	if m.recall.trouble != "" {
		return strings.Join(append(lines, m.theme.Error.Render("✗ "+m.recall.trouble)), "\n")
	}
	if len(m.recall.entries) == 0 {
		return strings.Join(append(lines, m.theme.Muted.Render("nothing here")), "\n")
	}
	return strings.Join(append(lines, m.recall.list(width)), "\n")
}

func (r recall) tag() string {
	if r.term != "" {
		return ui.Plural(len(r.entries), "match", "matches")
	}
	return ui.Plural(len(r.entries), "statement", "statements")
}

func (r recall) search(width int) string {
	typed := r.term
	if typed == "" {
		typed = r.theme.Subtle.Render("type to search what you have run")
	}
	return ui.SplitLine(r.theme.Prompt.Render("› ")+typed,
		r.theme.Subtle.Render("enter opens it in a tab · space keeps it"), width)
}

func (r recall) list(width int) string {
	rows := make([]row, 0, len(r.entries))
	for _, entry := range r.entries {
		rows = append(rows, row{
			key:   entry.Statement,
			label: entry.Snippet(max(width-34, 20)),
			note:  r.note(entry),
		})
	}
	list := newPicker(r.theme, "").withRows(rows)
	list.cursor = r.cursor
	return list.view(width)
}

// note is what a statement did, said the way somebody looking for it would
// remember it: when, and how it went.
func (r recall) note(entry history.Entry) string {
	when := entry.RanAt.Format("15:04")
	if !entry.Succeeded() {
		return ui.Dotted(when, "failed")
	}
	kept := ""
	if entry.Favorite {
		kept = "kept"
	}
	return ui.Dotted(when, ui.Plural(entry.Rows, "row", "rows"), kept)
}
