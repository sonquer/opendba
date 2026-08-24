package app

import (
	"context"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/sonquer/opendba/src/cli/internal/chats"
	"github.com/sonquer/opendba/src/cli/internal/ui"
)

type openChatsMsg struct{}

type newChatMsg struct{}

type listedChatsMsg struct {
	chats []chats.Chat
	err   error
}

type openedChatMsg struct {
	chat chats.Chat
	err  error
}

// chatList is the conversations that have been had, offered over the one on
// screen.
type chatList struct {
	theme   *ui.Theme
	chats   []chats.Chat
	cursor  int
	term    string
	trouble string
}

const chatsWidth = 62

func (m Model) openChats() (tea.Model, tea.Cmd) {
	if m.session.Chats == nil {
		return m, m.notify("conversations are not being kept")
	}
	list := &chatList{theme: m.theme}
	m.chats = list
	return m, m.readChats()
}

func (m Model) readChats() tea.Cmd {
	if m.session.Chats == nil {
		return nil
	}
	store := m.session.Chats
	term := ""
	if m.chats != nil {
		term = m.chats.term
	}
	limit := m.session.Settings.Chats.Limit
	connection := m.session.Connection.Name
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
		defer cancel()
		if strings.TrimSpace(term) != "" {
			found, err := store.Search(ctx, term, limit)
			return listedChatsMsg{chats: found, err: err}
		}
		found, err := store.Recent(ctx, connection, limit)
		return listedChatsMsg{chats: found, err: err}
	}
}

func (m Model) listedChats(msg listedChatsMsg) (tea.Model, tea.Cmd) {
	if m.chats == nil {
		return m, nil
	}
	list := *m.chats
	list.trouble = ""
	if msg.err != nil {
		list.trouble = msg.err.Error()
		m.chats = &list
		return m, nil
	}
	list.chats = msg.chats
	list.cursor = min(max(list.cursor, 0), max(len(msg.chats)-1, 0))
	m.chats = &list
	return m, nil
}

func (l chatList) selected() (chats.Chat, bool) {
	if l.cursor < 0 || l.cursor >= len(l.chats) {
		return chats.Chat{}, false
	}
	return l.chats[l.cursor], true
}

func (m Model) chatsKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	list := *m.chats
	switch {
	case key.Matches(msg, m.keys.Back):
		if list.term != "" {
			list.term = ""
			m.chats = &list
			return m, m.readChats()
		}
		m.chats = nil
		return m, nil
	case key.Matches(msg, m.keys.Above):
		list.cursor = clamp(list.cursor-1, 0, max(len(list.chats)-1, 0))
		m.chats = &list
		return m, nil
	case key.Matches(msg, m.keys.Below):
		list.cursor = clamp(list.cursor+1, 0, max(len(list.chats)-1, 0))
		m.chats = &list
		return m, nil
	case key.Matches(msg, m.keys.Choose):
		return m.openChat()
	case key.Matches(msg, m.keys.Forget):
		return m.confirmForget()
	case msg.String() == "backspace":
		if list.term == "" {
			return m, nil
		}
		list.term = list.term[:len(list.term)-1]
		m.chats = &list
		return m, m.readChats()
	}
	if typed := msg.String(); len(typed) == 1 {
		list.term += typed
		m.chats = &list
		return m, m.readChats()
	}
	return m, nil
}

// openChat reads a conversation back and carries on from it.
func (m Model) openChat() (tea.Model, tea.Cmd) {
	held, ok := m.chats.selected()
	if !ok {
		return m, nil
	}
	store := m.session.Chats
	id := held.ID
	m.chats = nil
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
		defer cancel()
		found, err := store.Load(ctx, id)
		return openedChatMsg{chat: found, err: err}
	}
}

func (m Model) openedChat(msg openedChatMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		return m, m.notify("that conversation could not be read: " + msg.err.Error())
	}
	m.talk.exchanges = transcript(msg.chat.Messages)
	m.talk.id = msg.chat.ID
	m.talk.thread++
	m.talk.started = msg.chat.StartedAt
	m.talk.trouble = ""
	if remembers, ok := recalls(m.assistant); ok {
		remembers.Resume(msg.chat.Messages)
	}
	shown, cmd := m.show(viewAsk)
	return shown.(Model).pinned(), cmd
}

// confirmForget asks before a conversation goes, because it is the only copy.
func (m Model) confirmForget() (tea.Model, tea.Cmd) {
	held, ok := m.chats.selected()
	if !ok {
		return m, nil
	}
	dialog := ask(m.theme, "forget this conversation?",
		"everything said in it goes with it, and this is the only copy",
		forgetChatMsg{id: held.ID})
	dialog.tag = m.theme.Muted.Render(ui.Plural(held.Questions(), "question", "questions"))
	dialog.body = held.Snippet(chatsWidth-6) + " — " + dialog.body
	m.modal = dialog
	return m, nil
}

type forgetChatMsg struct{ id int64 }

func (m Model) forgetChat(msg forgetChatMsg) (tea.Model, tea.Cmd) {
	if m.session.Chats == nil {
		return m, nil
	}
	store := m.session.Chats
	id := msg.id
	if m.talk.id == id {
		m.talk.id = 0
	}
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), rememberTimeout)
		defer cancel()
		if err := store.Remove(ctx, id); err != nil {
			return listedChatsMsg{err: err}
		}
		return openChatsMsg{}
	}
}

// startChat puts the conversation on screen away and begins another. What was
// said is already kept, so beginning again costs nothing.
func (m Model) startChat() (tea.Model, tea.Cmd) {
	if len(m.talk.exchanges) == 0 {
		return m, nil
	}
	keeping := m.keep()
	m.talk.exchanges = nil
	m.talk.id = 0
	m.talk.thread++
	m.talk.started = time.Time{}
	m.talk.trouble = ""
	m.offset, m.talk.bottom = 0, 0
	if remembers, ok := recalls(m.assistant); ok {
		remembers.Resume(nil)
	}
	shown, cmd := m.show(viewAsk)
	return shown, tea.Batch(cmd, keeping)
}

func (l chatList) view(width, height int) string {
	inner := min(ui.TextWidth(width)-6, chatsWidth)
	lines := []string{
		ui.SplitLine(l.theme.Title.Render("conversations"),
			l.theme.Muted.Render(l.tag()), inner),
		"",
		l.search(inner),
		l.theme.Rule(inner),
	}
	switch {
	case l.trouble != "":
		lines = append(lines, l.theme.Error.Render("✗ "+wrap(l.trouble, inner-2)))
	case len(l.chats) == 0:
		lines = append(lines, l.theme.Muted.Render("  nothing here yet"))
	default:
		lines = append(lines, l.list(inner, height))
	}
	lines = append(lines, "", l.theme.Hints(
		ui.Hint{Key: "enter", Does: "open"},
		ui.Hint{Key: "⌃X", Does: "forget"},
		ui.Hint{Key: "esc", Does: "close"}))
	return l.theme.Panel.Render(square(strings.Join(lines, "\n"), inner))
}

func (l chatList) tag() string {
	if l.term != "" {
		return ui.Plural(len(l.chats), "match", "matches")
	}
	return ui.Plural(len(l.chats), "conversation", "conversations")
}

func (l chatList) search(width int) string {
	typed := l.term
	if typed == "" {
		typed = l.theme.Subtle.Render("type to search what you have asked")
	}
	return ui.SplitLine(l.theme.Prompt.Render("› ")+typed, "", width)
}

func (l chatList) list(width, height int) string {
	room := max(ui.BodyHeight(height)-10, minChatRows)
	shown := l.chats
	cursor := l.cursor
	if len(shown) > room {
		start := clamp(cursor-room/2, 0, len(shown)-room)
		shown = shown[start : start+room]
		cursor -= start
	}
	rows := make([]row, 0, len(shown))
	for _, held := range shown {
		rows = append(rows, row{
			key:   held.Title,
			label: held.Snippet(max(width-26, 16)),
			note:  l.note(held),
		})
	}
	list := newPicker(l.theme, "").withRows(rows)
	list.cursor = cursor
	return list.view(width)
}

// minChatRows is how many conversations are offered on a window with no room.
const minChatRows = 3

func (l chatList) note(held chats.Chat) string {
	return ui.Dotted(held.UpdatedAt.Format("02 Jan 15:04"),
		ui.Plural(held.Questions(), "question", "questions"))
}
