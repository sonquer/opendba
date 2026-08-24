package app

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sonquer/tui4db/src/cli/internal/ai"
	"github.com/sonquer/tui4db/src/cli/internal/ai/agent"
	"github.com/sonquer/tui4db/src/cli/internal/chats"
	"github.com/sonquer/tui4db/src/cli/internal/config"
)

// chatting is a model on the ask screen with somewhere to keep what is said.
func chatting(t *testing.T, talk *scripted) Model {
	t.Helper()
	store, err := chats.Open(filepath.Join(t.TempDir(), "chats.db"),
		config.ChatSettings{Enabled: true, Limit: 100})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	m := talking(t, talk)
	m.width, m.height = 110, 40
	m.session.Chats = store
	m.session.Settings.Chats = config.ChatSettings{Enabled: true, Limit: 100}
	return m
}

func answering(events ...string) *scripted {
	said := make([]agent.Event, 0, len(events)+1)
	for _, text := range events {
		said = append(said, agent.Event{Kind: agent.EventText, Text: text})
	}
	return &scripted{events: append(said, agent.Event{Kind: agent.EventDone})}
}

// asking drives one whole turn and hands back the model afterwards.
func asking(t *testing.T, m Model, question string) Model {
	t.Helper()
	m.talk.prompt.SetValue(question)
	sent, cmd := m.send()
	if cmd == nil {
		t.Fatal("asking something must send it")
	}
	return converse(t, sent.(Model), cmd)
}

// A conversation is written down as it happens, so quitting is not losing it.
func TestAConversationIsWrittenDownAsItHappens(t *testing.T) {
	m := chatting(t, answering("there are two."))
	m = asking(t, m, "how many users?")

	if m.talk.id == 0 {
		t.Fatal("a conversation that has been had must be kept")
	}
	kept, err := m.session.Chats.Recent(context.Background(), "production-eu", 10)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(kept) != 1 {
		t.Fatalf("conversations = %d, want 1", len(kept))
	}
	if kept[0].Title != "how many users?" {
		t.Errorf("title = %q", kept[0].Title)
	}
	if kept[0].Instance != "claude" {
		t.Errorf("instance = %q, it must remember who answered", kept[0].Instance)
	}

	again := asking(t, m, "and orders?")
	after, err := m.session.Chats.Recent(context.Background(), "production-eu", 10)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(after) != 1 {
		t.Errorf("conversations = %d, a second question is the same conversation", len(after))
	}
	if again.talk.id != m.talk.id {
		t.Errorf("id = %d, want the same %d", again.talk.id, m.talk.id)
	}
}

// A conversation is opened again and carried on from, so the next answer knows
// what the last one said.
func TestAConversationIsOpenedAgainAndCarriedOn(t *testing.T) {
	talk := answering("there are two.")
	m := chatting(t, talk)
	m = asking(t, m, "how many users?")
	id := m.talk.id

	fresh := chatting(t, talk)
	fresh.session.Chats = m.session.Chats
	if len(fresh.talk.exchanges) != 0 {
		t.Fatal("a new conversation starts empty")
	}

	opened, cmd := fresh.Update(openChatsMsg{})
	list := settle(t, opened.(Model), cmd)
	if list.chats == nil {
		t.Fatal("the command must open the list")
	}
	if len(list.chats.chats) != 1 {
		t.Fatalf("conversations = %d", len(list.chats.chats))
	}
	view := plain(list.content())
	for _, want := range []string{"conversations", "how many users?", "1 question"} {
		if !strings.Contains(view, want) {
			t.Errorf("the list must show %q:\n%s", want, view)
		}
	}

	chosen, cmd := press(t, list, "enter")
	back := settle(t, chosen, cmd)
	if back.chats != nil {
		t.Error("choosing one must close the list")
	}
	if back.view != viewAsk {
		t.Errorf("view = %v", back.view)
	}
	if back.talk.id != id {
		t.Errorf("id = %d, want %d", back.talk.id, id)
	}
	if len(back.talk.exchanges) != 1 || back.talk.exchanges[0].question != "how many users?" {
		t.Errorf("exchanges = %+v, the conversation must be drawn again", back.talk.exchanges)
	}
	if len(talk.Messages()) == 0 {
		t.Error("and the assistant must be told what was said")
	}
}

// A conversation can be put away and another begun, without losing the first.
func TestANewConversationLeavesTheOldOneKept(t *testing.T) {
	m := chatting(t, answering("there are two."))
	m = asking(t, m, "how many users?")

	fresh, cmd := m.Update(newChatMsg{})
	begun := settle(t, fresh.(Model), cmd)
	if len(begun.talk.exchanges) != 0 || begun.talk.id != 0 {
		t.Errorf("a new conversation is empty: %+v", begun.talk)
	}

	kept, err := m.session.Chats.Recent(context.Background(), "production-eu", 10)
	if err != nil || len(kept) != 1 {
		t.Errorf("conversations = %d (%v), the old one is still there", len(kept), err)
	}
	quiet, cmd := begun.Update(newChatMsg{})
	if cmd != nil || len(quiet.(Model).talk.exchanges) != 0 {
		t.Error("beginning again with nothing said does nothing")
	}
}

// Searching narrows the list, and esc clears the search before it closes.
func TestSearchingTheConversations(t *testing.T) {
	m := chatting(t, answering("yes."))
	m = asking(t, m, "about users")
	m, _ = press(t, m, "ctrl+n")
	m = asking(t, m, "about orders")

	opened, cmd := m.Update(openChatsMsg{})
	list := settle(t, opened.(Model), cmd)
	if len(list.chats.chats) != 2 {
		t.Fatalf("conversations = %d", len(list.chats.chats))
	}

	searched := list
	for _, key := range []string{"o", "r", "d"} {
		next, cmd := press(t, searched, key)
		searched = settle(t, next, cmd)
	}
	if len(searched.chats.chats) != 1 {
		t.Errorf("matches = %d, searching must narrow it", len(searched.chats.chats))
	}
	if !strings.Contains(plain(searched.content()), "1 match") {
		t.Errorf("and say how many:\n%s", plain(searched.content()))
	}

	back, cmd := press(t, searched, "esc")
	cleared := settle(t, back, cmd)
	if cleared.chats == nil || cleared.chats.term != "" {
		t.Error("esc clears the search first")
	}
	closed, _ := press(t, cleared, "esc")
	if closed.chats != nil {
		t.Error("and then closes the list")
	}

	moved, _ := press(t, cleared, "down")
	if moved.chats.cursor != 1 {
		t.Errorf("cursor = %d", moved.chats.cursor)
	}
	up, _ := press(t, moved, "up")
	if up.chats.cursor != 0 {
		t.Errorf("cursor = %d", up.chats.cursor)
	}
	typed, cmd := press(t, cleared, "x")
	typed = settle(t, typed, cmd)
	removed, cmd := press(t, typed, "backspace")
	if settle(t, removed, cmd).chats.term != "" {
		t.Error("backspace takes the search back")
	}
	if quiet, cmd := press(t, cleared, "backspace"); cmd != nil || quiet.chats.term != "" {
		t.Error("and does nothing when there is nothing to take back")
	}
}

// Forgetting one asks first, and says what goes.
func TestForgettingAConversationAsksFirst(t *testing.T) {
	m := chatting(t, answering("yes."))
	m = asking(t, m, "about users")

	opened, cmd := m.Update(openChatsMsg{})
	list := settle(t, opened.(Model), cmd)
	asked, _ := press(t, list, "ctrl+x")
	if asked.modal == nil {
		t.Fatal("it must ask")
	}
	said := plain(asked.content())
	for _, want := range []string{"forget this conversation?", "about users", "only copy"} {
		if !strings.Contains(said, want) {
			t.Errorf("the question must say %q:\n%s", want, said)
		}
	}
	answered, cmd := press(t, asked, "enter")
	gone := pump(t, answered, cmd)
	if count, _ := m.session.Chats.Count(context.Background()); count != 0 {
		t.Errorf("conversations = %d, saying yes forgets it", count)
	}
	if gone.talk.id != 0 {
		t.Error("and the one on screen is no longer kept")
	}
}

// With nowhere to keep them the list says so and nothing is written.
func TestWithNowhereToKeepThemNothingIsKept(t *testing.T) {
	m := talking(t, answering("yes."))
	m.width, m.height = 110, 40
	refused, cmd := m.Update(openChatsMsg{})
	if cmd == nil || refused.(Model).chats != nil {
		t.Fatal("there is nothing to open")
	}
	if !strings.Contains(refused.(Model).text(), "not being kept") {
		t.Errorf("text = %q", refused.(Model).text())
	}
	if _, ok := m.kept(); ok {
		t.Error("and nothing to keep")
	}
	if cmd := m.keep(); cmd != nil {
		t.Error("nor anything to do about it")
	}
	if cmd := m.readChats(); cmd != nil {
		t.Error("nor anything to read")
	}
}

// A conversation that cannot hand back what was said is not kept, and the
// screen carries on regardless.
func TestAConversationThatCannotRememberIsNotKept(t *testing.T) {
	m := chatting(t, answering("yes."))
	m.assistant = &forgetful{}
	if _, ok := m.kept(); ok {
		t.Error("there is nothing to ask it for")
	}
	if _, ok := recalls(m.assistant); ok {
		t.Error("and it does not claim otherwise")
	}
	if _, ok := recalls(&scripted{}); !ok {
		t.Error("while one that can, does")
	}
}

// A conversation that could not be written down says so and is not lost.
func TestAConversationThatCouldNotBeKeptSaysSo(t *testing.T) {
	m := chatting(t, answering("yes."))
	said, _ := m.Update(keptMsg{err: errAlreadyClosed})
	if !strings.Contains(said.(Model).text(), "not being kept") {
		t.Errorf("text = %q", said.(Model).text())
	}
	if said.(Model).talk.id != 0 {
		t.Error("and nothing is claimed to have been written")
	}
	held, _ := m.Update(keptMsg{id: 7})
	if held.(Model).talk.id != 7 {
		t.Error("while one that was, is")
	}
}

// A conversation that could not be read back says so.
func TestAConversationThatCouldNotBeReadSaysSo(t *testing.T) {
	m := chatting(t, answering("yes."))
	failed, cmd := m.Update(openedChatMsg{err: errAlreadyClosed})
	if cmd == nil {
		t.Fatal("it has to say why")
	}
	if !strings.Contains(failed.(Model).text(), "could not be read") {
		t.Errorf("text = %q", failed.(Model).text())
	}
	broken, _ := m.Update(listedChatsMsg{err: errAlreadyClosed})
	if broken.(Model).chats != nil {
		t.Skip("there is no list open to put the trouble on")
	}
}

// An empty list says so rather than looking broken.
func TestAnEmptyListOfConversationsSaysSo(t *testing.T) {
	m := chatting(t, answering("yes."))
	opened, cmd := m.Update(openChatsMsg{})
	list := settle(t, opened.(Model), cmd)
	if !strings.Contains(plain(list.content()), "nothing here yet") {
		t.Errorf("an empty list says so:\n%s", plain(list.content()))
	}
	if _, ok := list.chats.selected(); ok {
		t.Error("and has nothing to open")
	}
	quiet, cmd := press(t, list, "enter")
	if cmd != nil || quiet.chats == nil {
		t.Error("enter on nothing does nothing")
	}
	if held, _ := press(t, list, "ctrl+x"); held.modal != nil {
		t.Error("nor does d")
	}
	if trouble, _ := list.Update(listedChatsMsg{err: errAlreadyClosed}); trouble.(Model).chats.trouble == "" {
		t.Error("and a list that could not be read says why")
	}
}

var errAlreadyClosed = context.Canceled

// A conversation with more in it than the panel can hold scrolls to the cursor.
func TestALongListOfConversationsScrolls(t *testing.T) {
	m := chatting(t, answering("yes."))
	held := make([]chats.Chat, 0, 30)
	for i := range 30 {
		held = append(held, chats.Chat{
			ID:       int64(i + 1),
			Title:    strings.Repeat("x", 4) + string(rune('a'+i%26)),
			Messages: []ai.Message{{Role: ai.RoleUser, Content: "q"}},
		})
	}
	list := &chatList{theme: m.theme, chats: held, cursor: 25}
	m.chats = list
	drawn := plain(list.view(110, 40))
	if strings.Count(drawn, "\n") > 40 {
		t.Errorf("the panel must fit the window:\n%s", drawn)
	}
	if !strings.Contains(drawn, "30 conversations") {
		t.Errorf("and say how many there are:\n%s", drawn)
	}
}
